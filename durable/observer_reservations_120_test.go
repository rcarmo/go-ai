package durable

import (
	"strings"
	"testing"
)

func TestObservation120LargeBaselineSharesTotalReservation(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxRetainedBytes = 64 << 10
	limits.MaxPage = 4
	store, err := OpenMemory(MemoryOptions{Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	def := checkpointDef(t, DefinitionOptions{Kind: "reserve.large", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"text": strings.Repeat("x", 24000)}, nil }})
	checkpointCommit(t, s, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err })
	_, first, err := s.SubscribeCommits(bg)
	if err != nil {
		t.Fatal("baseline above fixed16KiB share rejected", err)
	}
	_, second, err := s.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, third, err := s.SubscribeCommits(bg); err == nil {
		third.Stop()
		t.Fatal("aggregate reservations exceeded64KiB")
	}
	first.Stop()
	_, replacement, err := s.SubscribeCommits(bg)
	if err != nil {
		t.Fatal("reservation not released", err)
	}
	replacement.Stop()
	second.Stop()
}
