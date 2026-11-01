package durable

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
)

// SearchDocument supplies text to the shared BM25 ranker. ID is returned unchanged.
type SearchDocument struct {
	ID   string
	Text string
}
type RankedDocument struct {
	ID    string
	Score float64
}

var searchCamelBoundary = regexp.MustCompile(`([a-z0-9])([A-Z])`)
var searchAcronymBoundary = regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`)
var searchPluralES = regexp.MustCompile(`(ches|shes|sses|xes|zes)$`)
var searchWordBoundary = regexp.MustCompile(`[^a-z0-9]+`)
var searchStopWords = func() map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields("a an and are as at be by for from in is it of on or that the this to with") {
		out[word] = true
	}
	return out
}()

func searchTokens(text string) []string {
	text = searchCamelBoundary.ReplaceAllString(text, "${1} ${2}")
	text = strings.ToLower(searchAcronymBoundary.ReplaceAllString(text, "${1} ${2}"))
	words := searchWordBoundary.Split(text, -1)
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word == "" || searchStopWords[word] {
			continue
		}
		if len(word) > 4 && strings.HasSuffix(word, "ies") {
			word = word[:len(word)-3] + "y"
		} else if len(word) > 4 && searchPluralES.MatchString(word) {
			word = word[:len(word)-2]
		} else if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") {
			word = word[:len(word)-1]
		}
		out = append(out, word)
	}
	return out
}

func searchTermMatches(tokens, terms []string) []bool {
	matches := make([]bool, len(terms))
	for _, token := range tokens {
		for j, term := range terms {
			if token == term {
				matches[j] = true
			}
		}
	}
	return matches
}

func searchNormalizeName(name string) string {
	// ECMAScript trim includes BOM but excludes NEL (unlike strings.TrimSpace).
	name = strings.TrimFunc(name, func(r rune) bool {
		switch r {
		case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
			return true
		}
		return r >= 0x2000 && r <= 0x200a
	})
	var out strings.Builder
	for _, unit := range utf16.Encode([]rune(strings.ToLower(name))) {
		if unit >= 'a' && unit <= 'z' || unit >= '0' && unit <= '9' || unit == '_' || unit == '$' {
			out.WriteByte(byte(unit))
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

// RankDocuments ranks ID as the tool name separately from Text (BM25 k1=1.2,
// b=0.75). Exact names precede term coverage, then weighted relevance; ties keep
// input order. Text must not repeat the name. The two-field document layout is
// retained for compatibility. Tokenisation currently covers the pinned ASCII
// subset, not Intl.Segmenter's Unicode dictionary segmentation.
// This pure ranker performs no storage writes, tool loading or provider calls.
func RankDocuments(query string, documents []SearchDocument, limit int) []RankedDocument {
	return rankDocuments(query, documents, nil, limit)
}

// namespaceText, when supplied, is parallel to documents.
func rankDocuments(query string, documents []SearchDocument, namespaceText []string, limit int) []RankedDocument {
	out := []RankedDocument{}
	if limit <= 0 || len(documents) == 0 {
		return out
	}
	terms := []string{}
	seen := map[string]bool{}
	for _, term := range searchTokens(query) {
		if !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	if len(terms) == 0 {
		return out
	}
	// Only query-term frequencies affect BM25. Count full lengths without
	// retaining token lists or maps for unrelated words.
	frequencies := make([][]int, len(documents))
	lengths := make([]int, len(documents))
	df := make([]int, len(terms))
	nameMatches := make([][]bool, len(documents))
	namespaceMatches := make([][]bool, len(documents))
	localMatches := make([]bool, len(documents))
	total := 0
	for i, doc := range documents {
		tokens := searchTokens(doc.Text)
		lengths[i] = len(tokens)
		total += len(tokens)
		counts := make([]int, len(terms))
		for _, token := range tokens {
			for j, term := range terms {
				if token == term {
					counts[j]++
				}
			}
		}
		nameMatches[i] = searchTermMatches(searchTokens(doc.ID), terms)
		var ns string
		if namespaceText != nil {
			ns = namespaceText[i]
		}
		namespaceMatches[i] = searchTermMatches(searchTokens(ns), terms)
		localName := doc.ID
		if n := strings.LastIndex(localName, "__"); n >= 0 {
			localName = localName[n+2:]
		}
		localTerms := searchTokens(localName)
		localMatches[i] = len(localTerms) > 0
		for _, term := range localTerms {
			if !seen[term] {
				localMatches[i] = false
				break
			}
		}
		for j, n := range counts {
			if n > 0 || nameMatches[i][j] || namespaceMatches[i][j] {
				df[j]++
			}
		}
		frequencies[i] = counts
	}
	average := float64(total) / float64(len(documents))
	if average == 0 {
		average = 1
	}
	idf := make([]float64, len(terms))
	for i, n := range df {
		idf[i] = math.Log(1 + (float64(len(documents)-n)+0.5)/(float64(n)+0.5))
	}
	fullNameBonus := 0.0
	for _, v := range idf {
		fullNameBonus += v
	}
	exactQuery := searchNormalizeName(query)
	type match struct {
		index   int
		score   float64
		covered int
		exact   bool
	}
	matches := []match{}
	for i, counts := range frequencies {
		score := 0.0
		covered := 0
		norm := 1.2 * (0.25 + 0.75*float64(lengths[i])/average)
		for j, count := range counts {
			weight := 0.0
			if nameMatches[i][j] {
				weight += 3
			}
			if namespaceMatches[i][j] {
				weight += 0.5
			}
			if count > 0 {
				tf := float64(count)
				weight += tf * 2.2 / (tf + norm)
			}
			if weight > 0 {
				covered++
			}
			score += idf[j] * weight
		}
		if score > 0 {
			if localMatches[i] {
				score += fullNameBonus
			}
			name := searchNormalizeName(documents[i].ID)
			matches = append(matches, match{i, score, covered, name == exactQuery || strings.HasSuffix(name, "__"+exactQuery)})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool {
		a, b := matches[i], matches[j]
		if a.exact != b.exact {
			return a.exact
		}
		if a.covered != b.covered {
			return a.covered > b.covered
		}
		return a.score > b.score
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	for _, m := range matches {
		out = append(out, RankedDocument{documents[m.index].ID, m.score})
	}
	return out
}
