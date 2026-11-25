package durable

import (
	"strings"
	"testing"
)

func groupedWatermarksV160() (CodemodeStart, codemodeJournalState) {
	start := CodemodeStart{JournalVersion: codemodeGroupedJournalVersion, ParallelCalls: true, DrainAcknowledgements: true}
	j := codemodeJournalState{Version: start.JournalVersion, FirstIssued: watermark120(3), BatchEnds: []int{1, 3}}
	for i := 1; i <= 3; i++ {
		call := CodemodeCall{ID: i, Name: "leaf", Arguments: JSON{}}
		j.Issued = append(j.Issued, call)
		j.Deliveries = append(j.Deliveries, CodemodeDelivery{Seq: i, Call: call})
	}
	j.Deliveries[0].IssuedAfter = watermark120(3)
	return start, j
}

func TestV160CodemodeGroupedJournalAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, reason string
		edit         func(*CodemodeStart, *codemodeJournalState) *codemodeTerminal
	}{
		{"running-with-unacknowledged-suffix", "", nil},
		{"sealed-before-unused-suffix", "", func(_ *CodemodeStart, _ *codemodeJournalState) *codemodeTerminal {
			return &codemodeTerminal{Delivered: 2, Issued: 3, Committed: 3}
		}},
		{"unsupported-version", "invalid codemode group journal version", func(s *CodemodeStart, _ *codemodeJournalState) *codemodeTerminal {
			s.JournalVersion++
			return nil
		}},
		{"new-group-without-predecessor-watermark", "codemode batch lacks preceding durable count", func(_ *CodemodeStart, j *codemodeJournalState) *codemodeTerminal {
			j.Deliveries[0].IssuedAfter = nil
			return nil
		}},
		{"new-call-justified-only-within-its-batch", "codemode batch delivery lacks opening admission", func(_ *CodemodeStart, j *codemodeJournalState) *codemodeTerminal {
			j.BatchEnds = []int{3}
			j.FirstIssued = watermark120(1)
			return nil // a's later count does not authorize b or c in this group.
		}},
		{"early-stop-contradicts-second-idle", "codemode ended before durable continuation", func(_ *CodemodeStart, j *codemodeJournalState) *codemodeTerminal {
			j.Deliveries[1].IssuedAfter = watermark120(3)
			return &codemodeTerminal{Delivered: 2, Issued: 3, Committed: 3}
		}},
		{"seal-cannot-trim-committed-outcome", "invalid codemode terminal extent", func(_ *CodemodeStart, _ *codemodeJournalState) *codemodeTerminal {
			return &codemodeTerminal{Delivered: 2, Issued: 3, Committed: 2}
		}},
		{"nonterminal-issued-calls-need-coverage", "codemode batch contains unadmitted issued calls", func(_ *CodemodeStart, j *codemodeJournalState) *codemodeTerminal {
			j.Issued = append(j.Issued, CodemodeCall{ID: 4, Name: "leaf", Arguments: JSON{}})
			return nil
		}},
		{"batch-ends-cover-each-outcome", "codemode delivery missing batch extent", func(_ *CodemodeStart, j *codemodeJournalState) *codemodeTerminal {
			j.BatchEnds = []int{1}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			start, journal := groupedWatermarksV160()
			var terminal *codemodeTerminal
			if tc.edit != nil {
				terminal = tc.edit(&start, &journal)
			}
			err := validateGroupedCodemodeJournal(start, journal, terminal, terminal != nil)
			if tc.reason == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("journal result = %v; want %q", err, tc.reason)
			}
		})
	}
}
