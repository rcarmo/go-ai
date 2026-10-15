package durable

import "testing"

func watermark120(n int) *int { return &n }
func TestCodemode120WatermarkAdmissionIntegrity(t *testing.T) {
	start := CodemodeStart{DrainAcknowledgements: true}
	call := CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}
	journal := codemodeJournalState{Issued: []CodemodeCall{call}, FirstIssued: watermark120(1), Deliveries: []CodemodeDelivery{{Seq: 1, Call: call, HasValue: true, Value: "ok", IssuedAfter: watermark120(1)}}}
	if err := validateCodemodeWatermarks(start, journal, true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"initial-missing", "initial-negative", "initial-beyond", "unadmitted", "decreasing", "beyond", "missing-settled", "missing-middle", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			j := journal
			j.Deliveries = append([]CodemodeDelivery(nil), journal.Deliveries...)
			s := start
			switch mode {
			case "initial-missing":
				j.FirstIssued = nil
			case "initial-negative":
				j.FirstIssued = watermark120(-1)
			case "initial-beyond":
				j.FirstIssued = watermark120(2)
			case "unadmitted":
				j.FirstIssued = watermark120(0)
			case "decreasing":
				j.Deliveries[0].IssuedAfter = watermark120(0)
			case "beyond":
				j.Deliveries[0].IssuedAfter = watermark120(2)
			case "missing-settled":
				j.Deliveries[0].IssuedAfter = nil
			case "missing-middle":
				j.Deliveries[0].IssuedAfter = nil
				j.Deliveries = append(j.Deliveries, journal.Deliveries[0])
			case "disabled":
				s.DrainAcknowledgements = false
			}
			if err := validateCodemodeWatermarks(s, j, true); err == nil {
				t.Fatal("invalid watermark accepted")
			}
		})
	}
	empty := codemodeJournalState{FirstIssued: watermark120(0)}
	if err := validateCodemodeWatermarks(start, empty, true); err != nil {
		t.Fatal("zero watermark rejected", err)
	}
	pending := journal
	pending.Deliveries = append([]CodemodeDelivery(nil), journal.Deliveries...)
	pending.Deliveries[0].IssuedAfter = nil
	if err := validateCodemodeWatermarks(start, pending, false); err != nil {
		t.Fatal("pending acknowledgement rejected", err)
	}
}

func TestCodemode120WatermarkReplacementOnce(t *testing.T) {
	l := DefaultLimits()
	call := CodemodeCall{ID: 1, Name: "leaf", Arguments: JSON{}}
	pending := codemodeJournalState{Issued: []CodemodeCall{call}, Deliveries: []CodemodeDelivery{{Seq: 1, Call: call, HasValue: true, Value: "ok"}}}
	document := func(j codemodeJournalState) Document {
		value, err := dtoObject(j, l)
		if err != nil {
			t.Fatal(err)
		}
		return Document{Kind: codemodeJournalKind, Value: value}
	}
	acknowledged := pending
	acknowledged.FirstIssued = watermark120(1)
	acknowledged.Deliveries = append([]CodemodeDelivery(nil), pending.Deliveries...)
	acknowledged.Deliveries[0].IssuedAfter = watermark120(1)
	if err := validateCodemodeReplacement(document(pending), document(acknowledged), l); err != nil {
		t.Fatal("first acknowledgement rejected", err)
	}
	for _, mode := range []string{"initial-change", "initial-clear", "delivery-change", "delivery-clear", "payload"} {
		t.Run(mode, func(t *testing.T) {
			j := acknowledged
			j.Deliveries = append([]CodemodeDelivery(nil), acknowledged.Deliveries...)
			switch mode {
			case "initial-change":
				j.FirstIssued = watermark120(0)
			case "initial-clear":
				j.FirstIssued = nil
			case "delivery-change":
				j.Deliveries[0].IssuedAfter = watermark120(0)
			case "delivery-clear":
				j.Deliveries[0].IssuedAfter = nil
			case "payload":
				j.Deliveries[0].Value = "forged"
			}
			if err := validateCodemodeReplacement(document(acknowledged), document(j), l); err == nil {
				t.Fatal("watermark mutation accepted")
			}
		})
	}
}
