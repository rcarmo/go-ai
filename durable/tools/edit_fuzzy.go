package tools

import (
	"golang.org/x/text/unicode/norm"
	"strings"
	"unicode"
)

// These transforms follow pinned normalizeForFuzzyMatch; untouched source lines
// are restored after matching in normalised space.
func normalizeFuzzy(text string) string {
	lines := strings.Split(norm.NFKC.String(text), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, func(r rune) bool { return r == 0xfeff || unicode.IsSpace(r) })
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case 0x2018, 0x2019, 0x201a, 0x201b:
			return '\''
		case 0x201c, 0x201d, 0x201e, 0x201f:
			return '"'
		case 0x2010, 0x2011, 0x2012, 0x2013, 0x2014, 0x2015, 0x2212:
			return '-'
		case 0xa0, 0x202f, 0x205f, 0x3000:
			return ' '
		}
		if r >= 0x2000 && r <= 0x200a {
			return ' '
		}
		return r
	}, strings.Join(lines, "\n"))
}
func editMatch(content, old string) (start, length int, fuzzy bool) {
	if start = strings.Index(content, old); start >= 0 {
		return start, len(old), false
	}
	normalised, needle := normalizeFuzzy(content), normalizeFuzzy(old)
	return strings.Index(normalised, needle), len(needle), true
}

type editLine struct{ start, end int }

func editLineRanges(text string) []editLine {
	var lines []editLine
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, editLine{start, i + 1})
			start = i + 1
		}
	}
	if start < len(text) || len(lines) == 0 {
		lines = append(lines, editLine{start, len(text)})
	}
	return lines
}
func replaceMatches(content string, matches []matchedEdit, offset int) string {
	var out strings.Builder
	cursor := 0
	for _, match := range matches {
		out.WriteString(content[cursor : match.start-offset])
		out.WriteString(match.NewText)
		cursor = match.end - offset
	}
	out.WriteString(content[cursor:])
	return out.String()
}
func preserveUnchangedEditLines(original, base string, matches []matchedEdit) string {
	if original == base {
		return replaceMatches(base, matches, 0)
	}
	lines, source := editLineRanges(base), editLineRanges(original)
	type group struct {
		first, last int
		matches     []matchedEdit
	}
	var groups []group
	for _, match := range matches {
		first := 0
		for first < len(lines)-1 && match.start >= lines[first].end {
			first++
		}
		last := first
		end := match.end - 1
		if end < match.start {
			end = match.start
		}
		for last < len(lines)-1 && end >= lines[last].end {
			last++
		}
		last++
		if len(groups) > 0 && first < groups[len(groups)-1].last {
			g := &groups[len(groups)-1]
			if last > g.last {
				g.last = last
			}
			g.matches = append(g.matches, match)
		} else {
			groups = append(groups, group{first, last, []matchedEdit{match}})
		}
	}
	var result strings.Builder
	sourceIndex := 0
	for _, g := range groups {
		for _, line := range source[sourceIndex:g.first] {
			result.WriteString(original[line.start:line.end])
		}
		from, to := lines[g.first].start, lines[g.last-1].end
		result.WriteString(replaceMatches(base[from:to], g.matches, from))
		sourceIndex = g.last
	}
	for _, line := range source[sourceIndex:] {
		result.WriteString(original[line.start:line.end])
	}
	return result.String()
}
