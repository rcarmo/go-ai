package anthropic

import (
	"container/list"
	"encoding/json"
	"sync"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/stricttools"
)

const maxStrictToolLimitSessions = 10_000

var strictToolLimitSessions = struct {
	sync.Mutex
	entries map[string]*list.Element
	oldest  list.List
}{entries: map[string]*list.Element{}}

func init() {
	goai.RegisterSessionResourceCleanup(func(sessionID string) error {
		s := &strictToolLimitSessions
		s.Lock()
		defer s.Unlock()
		if sessionID == "" {
			clear(s.entries)
			s.oldest.Init()
		} else if entry := s.entries[sessionID]; entry != nil {
			s.oldest.Remove(entry)
			delete(s.entries, sessionID)
		}
		return nil
	})
}

func hasStrictToolLimitRejection(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	s := &strictToolLimitSessions
	s.Lock()
	defer s.Unlock()
	return s.entries[sessionID] != nil
}

func rememberStrictToolLimitRejection(sessionID string) {
	if sessionID == "" {
		return
	}
	s := &strictToolLimitSessions
	s.Lock()
	defer s.Unlock()
	if s.entries[sessionID] != nil {
		return
	}
	s.entries[sessionID] = s.oldest.PushBack(sessionID)
	if len(s.entries) > maxStrictToolLimitSessions {
		evicted := s.oldest.Remove(s.oldest.Front()).(string)
		delete(s.entries, evicted)
	}
}

func preferredStrictTool(tool goai.Tool, supported bool) bool {
	if tool.ConstrainedSampling == nil || tool.ConstrainedSampling.Type != "json_schema" || tool.ConstrainedSampling.Strict != "prefer" {
		return false
	}
	_, strict := anthropicToolInputSchema(tool, supported)
	return strict
}
func withoutPreferredStrictTools(input *goai.Context, supported bool) (*goai.Context, bool) {
	owned := *input
	found := false
	detach := func(tools []goai.Tool) []goai.Tool {
		out := append([]goai.Tool(nil), tools...)
		for i, tool := range out {
			if preferredStrictTool(tool, supported) {
				found = true
				tool.ConstrainedSampling = nil
				out[i] = tool
			}
		}
		return out
	}
	owned.Tools = detach(input.Tools)
	owned.Messages = append([]goai.Message(nil), input.Messages...)
	for i := range owned.Messages {
		owned.Messages[i].ToolsAdded = detach(input.Messages[i].ToolsAdded)
	}
	return &owned, found
}
func strictLimitError(body []byte) bool {
	var envelope struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Error.Type != "invalid_request_error" {
		return false
	}
	return stricttools.LimitMessage(envelope.Error.Message)
}
