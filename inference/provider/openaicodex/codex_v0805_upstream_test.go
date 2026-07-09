package openaicodex

import (
	"bytes"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func TestCodexZstdCompressesSSERequestBodies(t *testing.T) {
	body := []byte(`{"model":"gpt-5.2-codex","stream":true,"input":[{"role":"user","content":"hello"}]}`)
	compressed, ok := compressCodexRequestBodyZstd(body)
	if !ok {
		t.Fatal("expected zstd compression to be available")
	}
	if bytes.Equal(compressed, body) || len(compressed) == 0 {
		t.Fatalf("compressed body was not compressed: len=%d", len(compressed))
	}
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := dec.DecodeAll(compressed, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decompressed, body) {
		t.Fatalf("decompressed body=%q, want %q", decompressed, body)
	}
}

func TestCodexCachedWebSocketSessionExpiresBeforeBackendAgeLimit(t *testing.T) {
	entry := &codexWebSocketSessionEntry{createdAt: time.Unix(100, 0)}
	if codexWebSocketSessionExpired(entry, entry.createdAt.Add(codexWebSocketSessionMaxAge-time.Second)) {
		t.Fatal("session expired before max age")
	}
	if !codexWebSocketSessionExpired(entry, entry.createdAt.Add(codexWebSocketSessionMaxAge)) {
		t.Fatal("session should expire at max age")
	}
	if codexWebSocketSessionMaxAge != 55*time.Minute {
		t.Fatalf("max age=%v, want 55m", codexWebSocketSessionMaxAge)
	}
}
