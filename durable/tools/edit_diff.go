package tools

import (
	"fmt"
	"strings"
)

type editDiffLine struct {
	kind byte
	text string
}

func splitEditLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// Myers line edit paths use the reference's remove-before-add tie break. Reject
// excessive edit-path work instead of allocating an unbounded quadratic table.
func diffEditLines(old, new []string) ([]editDiffLine, error) {
	n, m := len(old), len(new)
	v := map[int]int{1: 0}
	var trace []map[int]int
	work := 0
	for depth := 0; depth <= n+m; depth++ {
		snapshot := make(map[int]int, len(v))
		for k, x := range v {
			snapshot[k] = x
		}
		trace = append(trace, snapshot)
		for k := -depth; k <= depth; k += 2 {
			work++
			if work > 1_000_000 {
				return nil, fmt.Errorf("edit diff work limit")
			}
			x := 0
			if k == -depth || k != depth && v[k-1] < v[k+1] {
				x = v[k+1]
			} else {
				x = v[k-1] + 1
			}
			y := x - k
			for x < n && y < m && old[x] == new[y] {
				x++
				y++
			}
			v[k] = x
			if x >= n && y >= m {
				var reverse []editDiffLine
				x, y = n, m
				for d := depth; d > 0; d-- {
					previous := trace[d]
					k := x - y
					pk := 0
					if k == -d || k != d && previous[k-1] < previous[k+1] {
						pk = k + 1
					} else {
						pk = k - 1
					}
					px := previous[pk]
					py := px - pk
					for x > px && y > py {
						reverse = append(reverse, editDiffLine{' ', old[x-1]})
						x--
						y--
					}
					if x == px {
						reverse = append(reverse, editDiffLine{'+', new[y-1]})
						y--
					} else {
						reverse = append(reverse, editDiffLine{'-', old[x-1]})
						x--
					}
				}
				for x > 0 && y > 0 {
					reverse = append(reverse, editDiffLine{' ', old[x-1]})
					x--
					y--
				}
				result := make([]editDiffLine, len(reverse))
				for i, line := range reverse {
					result[len(reverse)-1-i] = line
				}
				// jsdiff emits removals before additions in each change group.
				for i := 0; i < len(result); {
					if result[i].kind == ' ' {
						i++
						continue
					}
					end := i
					for end < len(result) && result[end].kind != ' ' {
						end++
					}
					group := append([]editDiffLine{}, result[i:end]...)
					at := i
					for _, kind := range []byte{'-', '+'} {
						for _, line := range group {
							if line.kind == kind {
								result[at] = line
								at++
							}
						}
					}
					i = end
				}
				return result, nil
			}
		}
	}
	return nil, fmt.Errorf("edit diff unavailable")
}
func editDiffDetails(path, old, new string) (string, string, int, error) {
	lines, err := diffEditLines(splitEditLines(old), splitEditLines(new))
	if err != nil {
		return "", "", 0, err
	}
	max := len(strings.Split(old, "\n"))
	if n := len(strings.Split(new, "\n")); n > max {
		max = n
	}
	width := len(fmt.Sprint(max))
	oldLine, newLine, first := 1, 1, 0
	output := []string{}
	emit := func(line editDiffLine) {
		number := oldLine
		if line.kind == '+' {
			number = newLine
		}
		output = append(output, fmt.Sprintf("%c%*d %s", line.kind, width, number, strings.TrimSuffix(line.text, "\n")))
		if line.kind != '+' {
			oldLine++
		}
		if line.kind != '-' {
			newLine++
		}
	}
	skip := func(count int) {
		if count == 0 {
			return
		}
		output = append(output, fmt.Sprintf(" %*s ...", width, ""))
		oldLine += count
		newLine += count
	}
	for i := 0; i < len(lines); {
		if lines[i].kind != ' ' {
			if first == 0 {
				first = newLine
			}
			emit(lines[i])
			i++
			continue
		}
		end := i
		for end < len(lines) && lines[end].kind == ' ' {
			end++
		}
		count := end - i
		leading := i > 0
		trailing := end < len(lines)
		switch {
		case leading && trailing:
			if count <= 8 {
				for _, line := range lines[i:end] {
					emit(line)
				}
			} else {
				for _, line := range lines[i : i+4] {
					emit(line)
				}
				skip(count - 8)
				for _, line := range lines[end-4 : end] {
					emit(line)
				}
			}
		case leading:
			n := count
			if n > 4 {
				n = 4
			}
			for _, line := range lines[i : i+n] {
				emit(line)
			}
			skip(count - n)
		case trailing:
			n := count
			if n > 4 {
				n = 4
			}
			skip(count - n)
			for _, line := range lines[end-n : end] {
				emit(line)
			}
		default:
			for _, line := range lines[i:end] {
				emit(line)
			}
		}
		i = end
	}
	patch := fmt.Sprintf("--- %s\n+++ %s\n", path, path)
	// Include four context lines around each change; merge overlapping contexts.
	type hunk struct{ start, end int }
	hunks := []hunk{}
	for i, line := range lines {
		if line.kind == ' ' {
			continue
		}
		start, end := i, i+1
		before := 0
		for start > 0 && lines[start-1].kind == ' ' && before < 4 {
			start--
			before++
		}
		after := 0
		for end < len(lines) && lines[end].kind == ' ' && after < 4 {
			end++
			after++
		}
		if len(hunks) > 0 && start <= hunks[len(hunks)-1].end {
			if end > hunks[len(hunks)-1].end {
				hunks[len(hunks)-1].end = end
			}
		} else {
			hunks = append(hunks, hunk{start, end})
		}
	}
	for _, h := range hunks {
		oldStart, newStart := 1, 1
		for _, line := range lines[:h.start] {
			if line.kind != '+' {
				oldStart++
			}
			if line.kind != '-' {
				newStart++
			}
		}
		oldCount, newCount := 0, 0
		for _, line := range lines[h.start:h.end] {
			if line.kind != '+' {
				oldCount++
			}
			if line.kind != '-' {
				newCount++
			}
		}
		if oldCount == 0 {
			oldStart--
		}
		if newCount == 0 {
			newStart--
		}
		patch += fmt.Sprintf("@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, line := range lines[h.start:h.end] {
			patch += string(line.kind) + strings.TrimSuffix(line.text, "\n") + "\n"
			if !strings.HasSuffix(line.text, "\n") {
				patch += "\\ No newline at end of file\n"
			}
		}
	}
	return strings.Join(output, "\n"), patch, first, nil
}
