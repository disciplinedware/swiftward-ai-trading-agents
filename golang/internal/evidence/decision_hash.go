package evidence

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"go.uber.org/zap"
	"golang.org/x/crypto/sha3"
)

// ZeroHash is the genesis hash for the first event in an entity's chain.
const ZeroHash = "0x0000000000000000000000000000000000000000000000000000000000000000"

// ComputeDecisionHash computes keccak256(canonical_json(trace) + previous_hash_bytes).
// Returns a 0x-prefixed hex string.
func ComputeDecisionHash(log *zap.Logger, trace map[string]any, previousHash string) (string, error) {
	canonicalJSON, err := CanonicalizeJSON(trace)
	if err != nil {
		return "", fmt.Errorf("canonicalize trace: %w", err)
	}

	// Decode previous hash from hex (strip 0x prefix)
	prevHex := previousHash
	if len(prevHex) >= 2 && prevHex[:2] == "0x" {
		prevHex = prevHex[2:]
	}
	prevBytes, err := hex.DecodeString(prevHex)
	if err != nil {
		log.Warn("Invalid previousHash, falling back to zero hash",
			zap.String("previous_hash", previousHash),
			zap.Error(err),
		)
		prevBytes = make([]byte, 32)
	}

	// Concatenate canonical JSON bytes + previous hash bytes
	data := append(canonicalJSON, prevBytes...)

	// Compute keccak256
	hasher := sha3.NewLegacyKeccak256()
	hasher.Write(data)
	hash := hasher.Sum(nil)

	return "0x" + hex.EncodeToString(hash), nil
}

// CanonicalizeJSON produces RFC 8785-compatible canonical JSON.
// Keys are sorted lexicographically, nested objects recursively canonicalized.
func CanonicalizeJSON(v any) ([]byte, error) {
	canonical := canonicalizeValue(v)
	return json.Marshal(canonical)
}

// canonicalizeValue recursively processes values to ensure deterministic ordering.
func canonicalizeValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return canonicalizeMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = canonicalizeValue(item)
		}
		return result
	default:
		return v
	}
}

// canonicalizeMap converts a map to an ordered structure for deterministic JSON.
func canonicalizeMap(m map[string]any) json.Marshaler {
	return &orderedMap{m: m}
}

// orderedMap produces JSON with lexicographically sorted keys.
type orderedMap struct {
	m map[string]any
}

func (o *orderedMap) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(o.m))
	for k := range o.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := []byte{'{'}
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, keyJSON...)
		buf = append(buf, ':')

		valJSON, err := json.Marshal(canonicalizeValue(o.m[k]))
		if err != nil {
			return nil, err
		}
		buf = append(buf, valJSON...)
	}
	buf = append(buf, '}')
	return buf, nil
}
