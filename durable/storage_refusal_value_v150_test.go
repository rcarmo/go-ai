package durable

import (
	"errors"
	"reflect"
	"testing"
)

// TestV150PublicCommitRefusalCarriesOriginalValue ports the original boundary
// semantics: CommitResult.refused is `unknown`, so the value a batch was refused
// with survives the public boundary verbatim, including present null/undefined
// and falsy values, while a Go error keeps its identity.
// Source: packages/durable/src/types.ts:1314 (CommitResult.refused?: unknown).
func TestV150PublicCommitRefusalCarriesOriginalValue(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	ctx := bg

	// A real refusal: an unrepresentable command type.
	result, err := store.Commit(ctx, [][]StorageWrite{{}, {{Type: "unknown"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stored != 1 || result.Refused == nil {
		t.Fatalf("refusal presence lost: %+v", result)
	}
	if _, ok := result.Refused.Value().(error); !ok {
		t.Fatalf("native cause lost: %#v", result.Refused.Value())
	}
	if !errors.Is(result.Refused.AsError(), result.Refused.Value().(error)) {
		t.Fatal("AsError lost the original cause identity")
	}

	// Present non-error values must survive verbatim rather than collapsing to a
	// generic error string.
	for _, value := range []any{commitUndefinedValue{}, nil, false, 0, ""} {
		refused := newCommitRefusal(value)
		if refused.Value() == nil && value == nil && refused.Value() != nil {
			t.Fatal("nil value lost")
		}
		if !reflect.DeepEqual(refused.Value(), value) {
			t.Fatalf("value %v became %#v", value, refused.Value())
		}
		if refused.AsError() == nil {
			t.Fatalf("present refusal %v projected as absent", value)
		}
	}

	// Absent refusal stays absent.
	if result, err := store.Commit(ctx, [][]StorageWrite{{}}, nil); err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatalf("empty batch: %+v %v", result, err)
	}
}
