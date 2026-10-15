package durable

import (
	"strings"
	"unicode/utf8"
)

// projectCodemodeOutput keeps both ends when the budget can fit a marker and
// useful context. Small budgets retain the existing prefix contract.
const MaxCodemodeOutputLines = 100000

// Reserve lines for truncation/path/status notices in the model transcript.
func codemodeLineProjection(text string) string {
	if strings.Count(text, "\n")+1 <= MaxCodemodeOutputLines-100 {
		return text
	}
	keep := (MaxCodemodeOutputLines - 100) / 2
	headEnd := 0
	for i := 0; i < keep; i++ {
		next := strings.IndexByte(text[headEnd:], '\n')
		if next < 0 {
			return text
		}
		headEnd += next + 1
	}
	tailStart := len(text)
	for i := 0; i < keep; i++ {
		next := strings.LastIndexByte(text[:tailStart], '\n')
		if next < 0 {
			break
		}
		tailStart = next
	}
	return text[:headEnd] + "\n… truncated …\n" + text[tailStart:]
}

func projectCodemodeOutput(text string, tokens int) string {
	text = codemodeLineProjection(text)
	prefix := boundCodemodeOutput(text, tokens)
	if prefix == text || tokens <= 0 {
		return prefix
	}
	const marker = "\n… truncated …\n"
	budget := codemodeTokenUnits(tokens)
	markerUnits := codemodeTextUnits(marker)
	if budget < markerUnits+8 {
		return prefix
	}
	remaining := budget - markerUnits
	head := codemodePrefixUnits(text, (remaining+1)/2)
	tail := codemodeSuffixUnits(text, remaining/2)
	return head + marker + tail
}
func codemodeTextUnits(text string) int {
	units := 0
	for _, r := range text {
		units++
		if r > 0xffff {
			units++
		}
	}
	return units
}
func codemodePrefixUnits(text string, budget int) string {
	units := 0
	for index, r := range text {
		size := 1
		if r > 0xffff {
			size = 2
		}
		if units+size > budget {
			return text[:index]
		}
		units += size
	}
	return text
}
func codemodeSuffixUnits(text string, budget int) string {
	pos := len(text)
	units := 0
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:pos])
		cost := 1
		if r > 0xffff {
			cost = 2
		}
		if units+cost > budget {
			break
		}
		units += cost
		pos -= size
	}
	return text[pos:]
}

// Keep a bounded saved-output hint only when it fits without sacrificing the
// entire useful body; full paths remain available in FullOutputPath.
func codemodeOutputWithPath(text, path string, tokens int) string {
	if path == "" {
		return projectCodemodeOutput(text, tokens)
	}
	hint := "\n[Full output: " + path + "]"
	if tokens <= 0 || codemodeTextUnits(hint)+8 > codemodeTokenUnits(tokens) {
		return projectCodemodeOutput(text, tokens)
	}
	bodyUnits := codemodeTokenUnits(tokens) - codemodeTextUnits(hint)
	body := projectCodemodeOutput(text, bodyUnits/4)
	return strings.TrimRight(body, "\n") + hint
}
