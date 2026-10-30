package durable

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"testing"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
)

func utf16ReceiptMessage(t *testing.T) goai.Message {
	t.Helper()
	var message goai.Message
	raw := []byte(`{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{"\ud800":"\udfff","n":9007199254740993,"__proto__":{"safe":true}},"argumentsError":"bad \ud800"}],"timestamp":1}`)
	if err := decodeStrict(raw, DefaultLimits(), 4096, &message); err != nil {
		t.Fatal(err)
	}
	return message
}
func assertUTF16Receipt(t *testing.T, object JSON, want goai.Message) {
	t.Helper()
	var receipt MessageReceipt
	if err := fromObject(object, &receipt, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err := restoreReceiptArguments(&receipt, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	message := receiptMessage(receipt)
	if !reflect.DeepEqual(message.Content, want.Content) {
		t.Fatal("receipt changed arguments/diagnostic", message.Content, want.Content)
	}
	if value, ok := message.Content[0].Arguments["n"].(json.Number); !ok || value.String() != "9007199254740993" {
		t.Fatal("stored integer rounded")
	}
}

func TestReceiptUTF16MainContributionAndStores(t *testing.T) {
	message := utf16ReceiptMessage(t)
	receipt, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	object, err := dtoObject(receipt, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = copyObject(object, DefaultLimits()); err != nil {
		t.Fatal("wire data escaped generic JSON fences", err)
	}
	if len(receipt.UTF16Arguments) != 1 {
		t.Fatal("missing storage adaptation")
	}
	raw, err := encodeBounded(object, DefaultLimits(), DefaultLimits().MaxDocumentBytes)
	if err != nil || !utf8.Valid(raw) {
		t.Fatal("wire is not bounded UTF-8", err)
	}
	assertUTF16Receipt(t, object, message)
	// Canonical internal carriers keep unchanged receipt deduplication stable.
	again, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !equalJSONValue(receipt, again) {
		t.Fatal("unchanged receipt failed semantic deduplication")
	}
	backends(t, func(t *testing.T, b backend) {
		id := mint(t, b.store)
		apply(t, b.store, entry(id, object))
		reopened := b.reopen()
		assertUTF16Receipt(t, snap(t, reopened).Entries[id].Value, message)
	})
	path := filepath.Join(t.TempDir(), "receipts.db")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	id := mint(t, store)
	apply(t, store, entry(id, object))
	if err = store.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	assertUTF16Receipt(t, snap(t, store).Entries[id].Value, message)
	// The owned copy and persisted image never borrow caller argument maps.
	message.Content[0].Arguments["n"] = json.Number("7")
	if receipt.Content[0].Arguments["n"] != json.Number("9007199254740993") {
		t.Fatal("receipt borrowed caller map")
	}
}

func TestReceiptUTF16MainWitnessGuards(t *testing.T) {
	message := utf16ReceiptMessage(t)
	receipt, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	object, err := dtoObject(receipt, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(JSON){
		func(o JSON) { o["role"] = "user" },
		func(o JSON) { w := o["utf16Arguments"].([]any)[0].(map[string]any); w["index"] = json.Number("99") },
		func(o JSON) { w := o["utf16Arguments"].([]any); o["utf16Arguments"] = append(w, w[0]) },
		func(o JSON) { w := o["utf16Arguments"].([]any)[0].(map[string]any); w["chunks"] = []any{"not base64!"} },
		func(o JSON) {
			c := o["content"].([]any)[0].(map[string]any)
			c["arguments"] = map[string]any{"spoof": true}
		},
		func(o JSON) { w := o["utf16Arguments"].([]any)[0].(map[string]any); w["unexpected"] = true },
	} {
		copy, err := copyObject(object, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		mutate(copy)
		var decoded MessageReceipt
		if err = fromObject(copy, &decoded, DefaultLimits()); err == nil {
			t.Fatal("invalid witness accepted")
		}
	}
	message.Content[0].Arguments["n"] = math.Inf(1)
	if _, err = contributionReceipt(message, DefaultLimits()); err == nil {
		t.Fatal("witness bypassed finite-number gate")
	}
	if _, err = copyObject(JSON{"x": message.Content[0].Arguments}, DefaultLimits()); err == nil {
		t.Fatal("ordinary JSON acquired UTF-16 permission")
	}
}

func TestReceiptUTF16MainGenerationTerminal(t *testing.T) {
	message := utf16ReceiptMessage(t)
	message.StopReason = goai.StopReasonToolUse
	h := &Harness{session: &Session{limits: DefaultLimits()}, options: Options{Registry: NewRegistry()}}
	// Production events use pointer variants; feed the same producer contract.
	pointerEvents := make(chan goai.Event, 1)
	pointerEvents <- &goai.DoneEvent{Message: &message}
	close(pointerEvents)
	pinned := &goai.Model{ID: "pinned", Provider: "openai", Api: "openai-completions"}
	receipt, ok, terminal := h.drainInvocation(nil, pointerEvents, pinned)
	if !ok || !terminal || receipt.ErrorCode != "" {
		t.Fatal("generation rejected UTF-16 tool arguments", receipt.ErrorCode)
	}
	if !reflect.DeepEqual(receipt.Content, message.Content) {
		t.Fatal("generation changed arguments")
	}
	object, err := dtoObject(receipt, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	assertUTF16Receipt(t, object, message)
}

func TestReceiptUTF16MainBudgetsAndAuthority(t *testing.T) {
	message := utf16ReceiptMessage(t)
	message.Content[0].Arguments["caller"] = callerStreamingMain{}
	if _, err := contributionReceipt(message, DefaultLimits()); err == nil {
		t.Fatal("caller marshaler admitted")
	}
	message = utf16ReceiptMessage(t)
	receipt, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = encodeBounded(receipt, DefaultLimits(), 32); err == nil {
		t.Fatal("small physical cap bypassed")
	}
	// A single projected receipt cannot accumulate per-slot payload budgets.
	receipt.Content = make([]goai.ContentBlock, 30)
	for i := range receipt.Content {
		receipt.Content[i] = message.Content[0]
	}
	if _, err = encodeBounded(receipt, DefaultLimits(), 1024); err == nil {
		t.Fatal("aggregate witness byte cap bypassed")
	}
	limits := DefaultLimits()
	limits.MaxStringBytes = 16
	receipt, err = contributionReceipt(message, limits)
	if err != nil {
		t.Fatal("bounded chunking failed", err)
	}
	object, err := dtoObject(receipt, limits)
	if err != nil {
		t.Fatal(err)
	}
	var decoded MessageReceipt
	if err = fromObject(object, &decoded, limits); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receipt.Content, decoded.Content) {
		t.Fatal("chunked receipt changed arguments")
	}
}

func TestReceiptUTF16MainEmptyArgumentsDiagnostic(t *testing.T) {
	message := utf16ReceiptMessage(t)
	message.Content[0].Arguments = map[string]any{}
	receipt, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.EmptyArguments) != 1 || receipt.Content[0].Arguments == nil || len(receipt.Content[0].Arguments) != 0 || receipt.Content[0].ArgumentsError != message.Content[0].ArgumentsError {
		t.Fatal("empty arguments/diagnostic changed")
	}
	object, err := dtoObject(receipt, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var next MessageReceipt
	if err = fromObject(object, &next, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err = restoreReceiptArguments(&next, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(receipt.Content, next.Content) {
		t.Fatal("empty arguments changed on replay")
	}
}

func FuzzReceiptUTF16MainProjection(f *testing.F) {
	for _, raw := range []string{`{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{"\ud800":"\udfff","n":9007199254740993}}]}`, `{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{},"argumentsError":"bad \ud800"}]}`, `{"role":"assistant","content":[{"type":"toolCall","id":"c","name":"f","arguments":{"n":1}}]}`} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		if len(raw) > 4096 {
			return
		}
		l := DefaultLimits()
		l.MaxDocumentBytes = 8192
		l.MaxRecordBytes = 8192
		l.MaxNodes = 1024
		l.MaxDepth = 32
		l.MaxMembers = 128
		l.MaxStringBytes = 1024
		var source goai.Message
		if decodeStrict([]byte(raw), l, 4096, &source) != nil {
			return
		}
		receipt, err := contributionReceipt(source, l)
		if err != nil {
			return
		}
		object, err := dtoObject(receipt, l)
		if err != nil {
			t.Fatal("owned receipt no longer projects", err)
		}
		if _, err = copyObject(object, l); err != nil {
			t.Fatal("wire projection fails generic JSON fence", err)
		}
		var next MessageReceipt
		if err = fromObject(object, &next, l); err != nil {
			t.Fatal("projection cannot replay", err)
		}
		if err = restoreReceiptArguments(&next, l); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(receipt.Content, next.Content) {
			t.Fatal("receipt content changed on replay")
		}
	})
}

func TestReceiptUTF16MainCarrierCannotChangeIdentity(t *testing.T) {
	message := utf16ReceiptMessage(t)
	receipt, err := contributionReceipt(message, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*goai.ToolCall){func(c *goai.ToolCall) { c.ID = "other" }, func(c *goai.ToolCall) { c.Name = "other" }, func(c *goai.ToolCall) { c.Namespace = "other" }, func(c *goai.ToolCall) { c.ThoughtSignature = "other" }, func(c *goai.ToolCall) { c.SetStreamingArguments(map[string]any{"ordinary": true}, "") }} {
		object, err := dtoObject(receipt, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		carrier := goai.ToolCall{Type: "toolCall"}
		carrier.SetStreamingArguments(message.Content[0].Arguments, message.Content[0].ArgumentsError)
		change(&carrier)
		payload, err := encodeBoundedOrder(carrier, DefaultLimits(), 4096, true)
		if err != nil {
			t.Fatal(err)
		}
		w := object["utf16Arguments"].([]any)[0].(map[string]any)
		w["chunks"] = []any{base64.RawStdEncoding.EncodeToString(payload)}
		var next MessageReceipt
		if err = fromObject(object, &next, DefaultLimits()); err == nil {
			t.Fatal("carrier changed identity or admitted ordinary data")
		}
	}
}
