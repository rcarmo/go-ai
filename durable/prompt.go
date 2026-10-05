package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"regexp"
	"sort"
	"strings"
)

type RenderedSection struct {
	Key  string
	Text string
}
type PromptInput struct {
	Conversation ID
	Agent        AgentChange
	Tools        []goai.Tool
	Messages     []MessageReceipt
}
type PromptSection struct {
	Key      string
	Untagged bool
	Render   func(context.Context, PromptInput) (*string, error)
}

var sectionKey = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ReplayPromptSections preserves replacement position, deletion and re-add order.
// Older unordered receipts use lexicographic keys; native prompt entries persist
// their explicit SectionOrder rather than relying on Go map iteration.
func ReplayPromptSections(messages []MessageReceipt) []RenderedSection {
	shown := []RenderedSection{}
	for _, message := range messages {
		if message.Role != goai.RoleSystem {
			continue
		}
		keys := append([]string(nil), message.SectionOrder...)
		seen := map[string]bool{}
		for _, key := range keys {
			seen[key] = true
		}
		extra := []string{}
		for key := range message.Sections {
			if !seen[key] {
				extra = append(extra, key)
			}
		}
		sort.Strings(extra)
		keys = append(keys, extra...)
		for _, key := range keys {
			value, exists := message.Sections[key]
			if !exists {
				continue
			}
			index := -1
			for i, section := range shown {
				if section.Key == key {
					index = i
					break
				}
			}
			if value == nil {
				if index >= 0 {
					shown = append(shown[:index], shown[index+1:]...)
				}
				continue
			}
			section := RenderedSection{key, *value}
			if index < 0 {
				shown = append(shown, section)
			} else {
				shown[index] = section
			}
		}
	}
	return shown
}
func renderPromptCallback(ctx context.Context, section PromptSection, input PromptInput) (text *string, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("prompt section %s panicked", section.Key)
		}
	}()
	return section.Render(ctx, input)
}
func RenderPromptSections(ctx context.Context, sections []PromptSection, input PromptInput, shown []RenderedSection, report func(error)) ([]RenderedSection, error) {
	if ctx == nil {
		return nil, reject("nil prompt context")
	}
	seen := map[string]bool{}
	for _, section := range sections {
		if !sectionKey.MatchString(section.Key) || section.Render == nil || seen[section.Key] {
			return nil, reject("invalid or duplicate prompt section")
		}
		seen[section.Key] = true
	}
	desired := []RenderedSection{}
	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, err := renderPromptCallback(ctx, section, input)
		if err != nil {
			if cause := ctx.Err(); cause != nil {
				return nil, cause
			}
			reportTaskError(report, err)
			for _, kept := range shown {
				if kept.Key == section.Key {
					desired = append(desired, kept)
					break
				}
			}
			continue
		}
		if text == nil {
			continue
		}
		value := *text
		if !section.Untagged {
			value = "<" + section.Key + ">\n" + value + "\n</" + section.Key + ">"
		}
		desired = append(desired, RenderedSection{section.Key, value})
	}
	return desired, nil
}
func promptText(sections []RenderedSection) string {
	values := make([]string, len(sections))
	for i, section := range sections {
		values[i] = section.Text
	}
	return strings.Join(values, "\n\n")
}

// PlanPromptEntries returns immutable system contribution drafts. Caller mints
// IDs and places the drafts in the same request-intent commit.
func PlanPromptEntries(view ContextView, desired []RenderedSection, timestamp int64) ([]Entry, error) {
	wanted := map[string]string{}
	for _, section := range desired {
		if !sectionKey.MatchString(section.Key) {
			return nil, reject("invalid prompt section key")
		}
		if _, ok := wanted[section.Key]; ok {
			return nil, reject("duplicate prompt section")
		}
		wanted[section.Key] = section.Text
	}
	shown := ReplayPromptSections(view.Messages)
	entry := func(keys []string, patch map[string]*string) Entry {
		return Entry{Kind: "pi.system", Value: JSON{}, Model: []MessageReceipt{{Role: goai.RoleSystem, Content: []goai.ContentBlock{}, Sections: patch, SectionOrder: keys, Timestamp: timestamp}}}
	}
	baseline := false
	if view.Head != nil {
		baseline = true
		for _, existing := range view.Entries {
			if existing.Kind == "pi.system" && existing.ID > view.Head.ID {
				baseline = false
			}
		}
	}
	if baseline {
		keys := []string{}
		patch := map[string]*string{}
		for _, section := range desired {
			value := section.Text
			patch[section.Key] = &value
			keys = append(keys, section.Key)
		}
		draft := entry(keys, patch)
		for _, existing := range view.Entries {
			if existing.Kind == "pi.system" {
				draft.Edits = append(draft.Edits, ContextEdit{Target: existing.ID, Action: "omit"})
			}
		}
		return []Entry{draft}, nil
	}
	replayed := []string{}
	have := map[string]string{}
	for _, section := range shown {
		have[section.Key] = section.Text
		if _, ok := wanted[section.Key]; ok {
			replayed = append(replayed, section.Key)
		}
	}
	for _, section := range desired {
		if _, ok := have[section.Key]; !ok {
			replayed = append(replayed, section.Key)
		}
	}
	reorder := false
	for i, key := range replayed {
		if desired[i].Key != key {
			reorder = true
		}
	}
	if reorder {
		removed, added := map[string]*string{}, map[string]*string{}
		oldKeys, newKeys := []string{}, []string{}
		for _, section := range shown {
			removed[section.Key] = nil
			oldKeys = append(oldKeys, section.Key)
		}
		for _, section := range desired {
			value := section.Text
			added[section.Key] = &value
			newKeys = append(newKeys, section.Key)
		}
		return []Entry{entry(oldKeys, removed), entry(newKeys, added)}, nil
	}
	patch := map[string]*string{}
	keys := []string{}
	for _, section := range shown {
		value, ok := wanted[section.Key]
		if !ok {
			patch[section.Key] = nil
			keys = append(keys, section.Key)
		} else if value != section.Text {
			copy := value
			patch[section.Key] = &copy
			keys = append(keys, section.Key)
		}
	}
	for _, section := range desired {
		if _, ok := have[section.Key]; !ok {
			value := section.Text
			patch[section.Key] = &value
			keys = append(keys, section.Key)
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}
	return []Entry{entry(keys, patch)}, nil
}
