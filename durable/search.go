package durable

import (
	"math"
	"regexp"
	"sort"
	"strings"
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

var searchCamelBoundary = regexp.MustCompile(`([a-z])([A-Z])`)
var searchWordBoundary = regexp.MustCompile(`[^a-z0-9]+`)
var searchStopWords = func() map[string]bool {
	out := map[string]bool{}
	for _, word := range strings.Fields("a an the and or but in on at to for of with by from is are was were be been being have has had do does did will would should could may might can this that these those it its as into not no so if than then") {
		out[word] = true
	}
	return out
}()

func searchTokens(text string) []string {
	text = strings.ToLower(searchCamelBoundary.ReplaceAllString(text, "${1} ${2}"))
	words := searchWordBoundary.Split(text, -1)
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word == "" || searchStopWords[word] {
			continue
		}
		if len(word) > 4 && strings.HasSuffix(word, "ies") {
			word = word[:len(word)-3] + "y"
		} else if len(word) > 3 && strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && !strings.HasSuffix(word, "us") {
			word = word[:len(word)-1]
		}
		out = append(out, word)
	}
	return out
}

// RankDocuments uses the upstream1.2.0 shared BM25 contract (k1=1.2,b=0.75).
// Positive scores only; ties retain input order. Tokenisation splits ASCII
// camelCase, removes stop words and applies upstream's simple plural stemming.
// This pure ranker performs no storage writes, tool loading or provider calls.
func RankDocuments(query string, documents []SearchDocument, limit int) []RankedDocument {
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
		for j, n := range counts {
			if n > 0 {
				df[j]++
			}
		}
		frequencies[i] = counts
	}
	if total == 0 {
		return out
	}
	average := float64(total) / float64(len(documents))
	idf := make([]float64, len(terms))
	for i, n := range df {
		if n > 0 {
			idf[i] = math.Log(1 + (float64(len(documents)-n)+0.5)/(float64(n)+0.5))
		}
	}
	type match struct {
		index int
		score float64
	}
	matches := []match{}
	for i, counts := range frequencies {
		score := 0.0
		norm := 1.2 * (0.25 + 0.75*float64(lengths[i])/average)
		for j, count := range counts {
			if count > 0 {
				tf := float64(count)
				score += idf[j] * tf * 2.2 / (tf + norm)
			}
		}
		if score > 0 {
			matches = append(matches, match{i, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	if len(matches) > limit {
		matches = matches[:limit]
	}
	for _, m := range matches {
		out = append(out, RankedDocument{documents[m.index].ID, m.score})
	}
	return out
}
