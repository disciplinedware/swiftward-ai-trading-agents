package agentintel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// DataDir is the root for all agent-intel data.
const DataDir = "data/agent-intel"

// Paths returns resolved paths for all data subdirectories.
type Paths struct {
	Root     string
	Raw      string
	Computed string
	Site     string
}

// NewPaths creates a Paths rooted at the given base directory.
func NewPaths(base string) Paths {
	root := filepath.Join(base, DataDir)
	return Paths{
		Root:     root,
		Raw:      filepath.Join(root, "raw"),
		Computed: filepath.Join(root, "computed"),
		Site:     filepath.Join(base, "landing", "audit"),
	}
}

// EnsureDirs creates all required top-level subdirectories.
// Per-agent subdirectories are created on demand by SaveJSON/AppendJSONL.
func (p Paths) EnsureDirs() error {
	dirs := []string{
		filepath.Join(p.Raw, "agents"),
		filepath.Join(p.Raw, "blocks"),
		filepath.Join(p.Raw, "marketdata"),
		filepath.Join(p.Computed, "agents"),
		filepath.Join(p.Site, "agents"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// --- Per-agent path helpers ---
//
// Each agent's raw data lives in raw/agents/{id}/ as a self-contained directory.
// Shared data (market candles, block timestamps) lives at top level.

// AgentDir returns the per-agent raw data directory: raw/agents/{id}
func (p Paths) AgentDir(id int64) string {
	return filepath.Join(p.Raw, "agents", fmt.Sprintf("%d", id))
}

// AgentInfo returns the path to the agent's registration metadata:
// raw/agents/{id}/info.json (name, wallet, description, caps, registered_at)
func (p Paths) AgentInfo(id int64) string {
	return filepath.Join(p.AgentDir(id), "info.json")
}

// AgentState returns the path to the agent's current snapshot state:
// raw/agents/{id}/state.json (validation_score, reputation_score, attestation_count, vault_claimed)
func (p Paths) AgentState(id int64) string {
	return filepath.Join(p.AgentDir(id), "state.json")
}

// AgentIntents returns the per-agent trade intent events file (append-only JSONL).
func (p Paths) AgentIntents(id int64) string {
	return filepath.Join(p.AgentDir(id), "intents.jsonl")
}

// AgentApprovals returns the per-agent trade approval events file (append-only JSONL).
func (p Paths) AgentApprovals(id int64) string {
	return filepath.Join(p.AgentDir(id), "approvals.jsonl")
}

// AgentRejections returns the per-agent trade rejection events file (append-only JSONL).
func (p Paths) AgentRejections(id int64) string {
	return filepath.Join(p.AgentDir(id), "rejections.jsonl")
}

// AgentAttestations returns the per-agent attestation events file (append-only JSONL).
func (p Paths) AgentAttestations(id int64) string {
	return filepath.Join(p.AgentDir(id), "attestations.jsonl")
}

// AgentReputation returns the per-agent reputation feedback events file (append-only JSONL).
func (p Paths) AgentReputation(id int64) string {
	return filepath.Join(p.AgentDir(id), "reputation.jsonl")
}

// --- JSON file helpers ---

// LoadJSON reads a JSON file into dst. Returns false if the file does not exist.
func LoadJSON(path string, dst any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return true, nil
}

// SaveJSON writes src to a JSON file atomically (write to .tmp, then rename).
func SaveJSON(path string, src any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	data, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal for %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// --- JSONL file helpers ---

// AppendJSONL appends one or more JSON objects as lines to a JSONL file.
func AppendJSONL(path string, records ...any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", path, err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, r := range records {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("encode line in %s: %w", path, err)
		}
	}
	return nil
}

// ReadJSONL reads all lines from a JSONL file, decoding each into a new T.
func ReadJSONL[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	// Read all lines first so we can detect torn writes on the last line.
	// A torn write happens when the process crashes mid-AppendJSONL: the prefix
	// of a record is flushed but the terminating newline is not. An incomplete
	// final line must not fail the whole read, otherwise pnl.go loses everything
	// for that agent.
	var rawLines [][]byte
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB line buffer
	for scanner.Scan() {
		// Copy because scanner.Bytes() is only valid until next Scan().
		line := append([]byte(nil), scanner.Bytes()...)
		rawLines = append(rawLines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}

	results := make([]T, 0, len(rawLines))
	for i, line := range rawLines {
		if len(line) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(line, &item); err != nil {
			// The LAST line may be incomplete due to a crash mid-append.
			// Tolerate it with a log. Any earlier line failing means real corruption.
			if i == len(rawLines)-1 {
				// Silently tolerate — JSONL is append-only and the writer may have
				// crashed mid-record. The record will be re-fetched on the next sync.
				break
			}
			return nil, fmt.Errorf("unmarshal line %d in %s: %w", i+1, path, err)
		}
		results = append(results, item)
	}
	return results, nil
}

// TrimJSONLAfterBlock rewrites a JSONL file keeping only records whose top-level
// "block" field is <= maxBlock. Atomic via tmp file + rename. Used at sync start
// to undo any appends written by a prior crashed sync before the meta checkpoint
// advanced past them, so the resumed run cannot produce duplicate records.
//
// A torn/unparseable line is dropped (safer than keeping a garbage row). Returns
// the number of records removed. If nothing needs to change the file is left
// untouched (no rewrite, no rename).
func TrimJSONLAfterBlock(path string, maxBlock int64) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	type blockHeader struct {
		Block int64 `json:"block"`
	}

	var kept [][]byte
	dropped := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		line := append([]byte(nil), raw...)
		var h blockHeader
		if err := json.Unmarshal(line, &h); err != nil {
			// Torn or corrupt line: drop it so the rewritten file is clean.
			dropped++
			continue
		}
		if h.Block > maxBlock {
			dropped++
			continue
		}
		kept = append(kept, line)
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("scan %s: %w", path, err)
	}

	if dropped == 0 {
		return 0, nil
	}

	tmp := path + ".tmp"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", tmp, err)
	}
	cleanup := func() {
		_ = out.Close()
		_ = os.Remove(tmp)
	}
	w := bufio.NewWriter(out)
	for _, line := range kept {
		if _, err := w.Write(line); err != nil {
			cleanup()
			return 0, fmt.Errorf("write %s: %w", tmp, err)
		}
		if err := w.WriteByte('\n'); err != nil {
			cleanup()
			return 0, fmt.Errorf("write %s: %w", tmp, err)
		}
	}
	if err := w.Flush(); err != nil {
		cleanup()
		return 0, fmt.Errorf("flush %s: %w", tmp, err)
	}
	if err := out.Sync(); err != nil {
		cleanup()
		return 0, fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		return 0, fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return 0, fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return dropped, nil
}

// CountJSONL returns the number of lines in a JSONL file. Returns 0 if not exists.
func CountJSONL(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer func() { _ = f.Close() }()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count, scanner.Err()
}

// --- Block timestamp cache ---

// BlockTimestamps maps block number (as string key) to unix timestamp.
type BlockTimestamps map[string]int64

// LoadBlockTimestamps loads the block timestamp cache.
func LoadBlockTimestamps(p Paths) (BlockTimestamps, error) {
	path := filepath.Join(p.Raw, "blocks", "timestamps.json")
	var ts BlockTimestamps
	found, err := LoadJSON(path, &ts)
	if err != nil {
		return nil, err
	}
	if !found {
		return make(BlockTimestamps), nil
	}
	return ts, nil
}

// SaveBlockTimestamps saves the block timestamp cache.
func SaveBlockTimestamps(p Paths, ts BlockTimestamps) error {
	return SaveJSON(filepath.Join(p.Raw, "blocks", "timestamps.json"), ts)
}

// --- Meta helpers ---

// LoadMeta loads the sync metadata. Returns a zero Meta if not found.
func LoadMeta(p Paths) (Meta, error) {
	path := filepath.Join(p.Raw, "meta.json")
	var m Meta
	_, err := LoadJSON(path, &m)
	if err != nil {
		return m, err
	}
	if m.MarketCursors == nil {
		m.MarketCursors = make(map[string]MarketCursor)
	}
	return m, nil
}

// SaveMeta saves the sync metadata.
func SaveMeta(p Paths, m Meta) error {
	return SaveJSON(filepath.Join(p.Raw, "meta.json"), m)
}

// --- Sync lock ---

// AcquireSyncLock creates an exclusive lock file to prevent concurrent sync
// processes from corrupting each other's data. Returns a release function
// that must be called to remove the lock.
//
// This is an advisory lock — processes must cooperate. A stale lock (from a
// crashed process) can be forcibly overridden by deleting the lock file.
func AcquireSyncLock(p Paths) (release func(), err error) {
	if err := os.MkdirAll(p.Raw, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir for lock: %w", err)
	}
	lockPath := filepath.Join(p.Raw, ".sync.lock")

	// O_CREATE|O_EXCL fails if the file already exists.
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another sync is already running (or stale lock at %s — delete it if certain no other sync is running)", lockPath)
		}
		return nil, fmt.Errorf("create lock: %w", err)
	}
	// Write PID for debugging.
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Close()

	release = func() {
		if err := os.Remove(lockPath); err != nil {
			// Best-effort: log through stderr since we don't have a logger here.
			fmt.Fprintf(os.Stderr, "WARN: failed to remove sync lock %s: %v\n", lockPath, err)
		}
	}
	return release, nil
}
