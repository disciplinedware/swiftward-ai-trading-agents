package evidence

import (
	"strings"
	"testing"

	"go.uber.org/zap"
)

// nopLogger is a no-op logger for tests.
var nopLogger = zap.NewNop()

func TestCanonicalizeJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    any
		wantJSON string
	}{
		{
			name:     "empty_map",
			input:    map[string]any{},
			wantJSON: `{}`,
		},
		{
			name:     "keys_sorted_lexicographically",
			input:    map[string]any{"z": 1, "a": 2, "m": 3},
			wantJSON: `{"a":2,"m":3,"z":1}`,
		},
		{
			name:     "nested_map_keys_sorted",
			input:    map[string]any{"b": map[string]any{"z": 1, "a": 2}, "a": "x"},
			wantJSON: `{"a":"x","b":{"a":2,"z":1}}`,
		},
		{
			name:     "array_order_preserved",
			input:    map[string]any{"items": []any{3, 1, 2}},
			wantJSON: `{"items":[3,1,2]}`,
		},
		{
			name:     "array_of_maps_keys_sorted",
			input:    map[string]any{"rows": []any{map[string]any{"z": 1, "a": 2}}},
			wantJSON: `{"rows":[{"a":2,"z":1}]}`,
		},
		{
			name:     "null_value",
			input:    map[string]any{"key": nil},
			wantJSON: `{"key":null}`,
		},
		{
			name:     "bool_values",
			input:    map[string]any{"f": false, "t": true},
			wantJSON: `{"f":false,"t":true}`,
		},
		{
			name:     "string_escaping",
			input:    map[string]any{"msg": `say "hello"`},
			wantJSON: `{"msg":"say \"hello\""}`,
		},
		{
			name:     "numeric_types",
			input:    map[string]any{"n": 42, "f": 3.14},
			wantJSON: `{"f":3.14,"n":42}`,
		},
		{
			name:     "scalar_string",
			input:    "just a string",
			wantJSON: `"just a string"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CanonicalizeJSON(tt.input)
			if err != nil {
				t.Fatalf("CanonicalizeJSON() error = %v", err)
			}
			if string(got) != tt.wantJSON {
				t.Errorf("got  %s\nwant %s", got, tt.wantJSON)
			}
		})
	}
}

func TestCanonicalizeJSONDeterminism(t *testing.T) {
	// Same input map produced repeatedly must give identical output.
	input := map[string]any{
		"agent_id": "agent-001",
		"pair":     "ETH-USDC",
		"side":     "buy",
		"value":    1000.0,
		"nested":   map[string]any{"z": 3, "a": 1, "m": 2},
	}

	first, err := CanonicalizeJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		got, err := CanonicalizeJSON(input)
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		if string(got) != string(first) {
			t.Fatalf("iteration %d: non-deterministic output\nfirst: %s\ngot:   %s", i, first, got)
		}
	}
}

func TestComputeDecisionHash(t *testing.T) {
	trace := map[string]any{
		"agent_id": "agent-001",
		"pair":     "ETH-USDC",
		"side":     "buy",
		"value":    100.0,
		"status":   "fill",
	}

	t.Run("returns_0x_prefixed_hex", func(t *testing.T) {
		hash, err := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(hash, "0x") {
			t.Errorf("hash %q does not start with 0x", hash)
		}
		// keccak256 → 32 bytes → 64 hex chars + "0x"
		if len(hash) != 66 {
			t.Errorf("hash length = %d, want 66", len(hash))
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		h1, err := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		if err != nil {
			t.Fatal(err)
		}
		h2, err := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		if err != nil {
			t.Fatal(err)
		}
		if h1 != h2 {
			t.Errorf("non-deterministic: %s != %s", h1, h2)
		}
	})

	t.Run("different_trace_different_hash", func(t *testing.T) {
		other := map[string]any{
			"agent_id": "agent-001",
			"pair":     "BTC-USDC", // different
			"side":     "sell",
			"value":    50.0,
			"status":   "fill",
		}
		h1, _ := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		h2, _ := ComputeDecisionHash(nopLogger, other, ZeroHash)
		if h1 == h2 {
			t.Error("different traces produced the same hash")
		}
	})

	t.Run("prev_hash_changes_output", func(t *testing.T) {
		h1, err := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		if err != nil {
			t.Fatal(err)
		}
		// Use h1 as prev_hash for h2 → must differ from h1
		h2, err := ComputeDecisionHash(nopLogger, trace, h1)
		if err != nil {
			t.Fatal(err)
		}
		if h1 == h2 {
			t.Error("same hash despite different prev_hash")
		}
	})

	t.Run("invalid_prev_hash_falls_back_to_zero", func(t *testing.T) {
		// Should not return an error; falls back to zero bytes.
		h1, err := ComputeDecisionHash(nopLogger, trace, "not-a-valid-hex")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should equal what we get when prev_hash is all-zero bytes (same fallback).
		h2, err := ComputeDecisionHash(nopLogger, trace, ZeroHash)
		if err != nil {
			t.Fatal(err)
		}
		if h1 != h2 {
			t.Errorf("invalid prev_hash fallback produced different hash than ZeroHash: %s vs %s", h1, h2)
		}
	})
}

func TestHashChain(t *testing.T) {
	// Simulate a sequence of 5 decisions; each must depend on the previous.
	hashes := make([]string, 5)
	prevHash := ZeroHash

	for i := range hashes {
		trace := map[string]any{"seq": i, "agent_id": "agent-001"}
		h, err := ComputeDecisionHash(nopLogger, trace, prevHash)
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		hashes[i] = h
		prevHash = h
	}

	// All hashes must be distinct (chain, not all same).
	seen := map[string]bool{}
	for i, h := range hashes {
		if seen[h] {
			t.Errorf("duplicate hash at step %d: %s", i, h)
		}
		seen[h] = true
	}

	// Re-computing from ZeroHash must reproduce the same chain.
	prevHash = ZeroHash
	for i := range hashes {
		trace := map[string]any{"seq": i, "agent_id": "agent-001"}
		h, _ := ComputeDecisionHash(nopLogger, trace, prevHash)
		if h != hashes[i] {
			t.Errorf("step %d: chain not reproducible: got %s, want %s", i, h, hashes[i])
		}
		prevHash = h
	}
}
