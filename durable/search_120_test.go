package durable

import (
	"math"
	"reflect"
	"testing"
)

func TestSearch120Tokenisation(t *testing.T) {
	for _, tc := range []struct {
		text string
		want []string
	}{{"getFiles and the directories", []string{"get", "file", "directory"}}, {"ADDRESS status class gas", []string{"address", "status", "class", "gas"}}, {"file_system/listPaths", []string{"file", "system", "list", "path"}}, {"the AND", []string{}}} {
		if got := searchTokens(tc.text); !reflect.DeepEqual(got, tc.want) {
			t.Fatal(tc.text, got, tc.want)
		}
	}
}
func TestSearch120BM25IndependentScoresAndTieOrder(t *testing.T) {
	docs := []SearchDocument{{"first", "cat cat dog"}, {"second", "cat dog dog"}, {"third", "bird"}}
	ranked := RankDocuments("cats cat", docs, 10)
	if len(ranked) != 2 || ranked[0].ID != "first" || ranked[1].ID != "second" {
		t.Fatal(ranked)
	}
	// Independent formula for df=2,N=3,avgdl=7/3,dl=3.
	idf := math.Log(1 + (3-2+0.5)/(2+0.5))
	norm := 1.2 * (1 - 0.75 + 0.75*3/(7.0/3))
	for i, tf := range []float64{2, 1} {
		want := idf * (tf * (1.2 + 1)) / (tf + norm)
		if math.Abs(ranked[i].Score-want) > 1e-14 {
			t.Fatal("BM25 error", ranked[i].Score, want)
		}
	}
	tied := RankDocuments("file", []SearchDocument{{"z", "files"}, {"a", "file"}}, 1)
	if len(tied) != 1 || tied[0].ID != "z" {
		t.Fatal(tied)
	}
	for _, q := range []string{"", "the and", "missing"} {
		if len(RankDocuments(q, docs, 10)) != 0 {
			t.Fatal(q)
		}
	}
	if len(RankDocuments("cat", docs, 0)) != 0 || len(RankDocuments("cat", nil, 10)) != 0 {
		t.Fatal("empty guard")
	}
}
