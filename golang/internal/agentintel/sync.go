package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"go.uber.org/zap"
)

// ABIs for contract calls.
const abiRegistry = `[
  {"inputs":[],"name":"totalAgents","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAgent","outputs":[{"type":"tuple","components":[{"type":"address","name":"operatorWallet"},{"type":"address","name":"agentWallet"},{"type":"string","name":"name"},{"type":"string","name":"description"},{"type":"string[]","name":"capabilities"},{"type":"uint256","name":"registeredAt"},{"type":"bool","name":"active"}]}],"stateMutability":"view","type":"function"}
]`

// Validation registry view functions used by the state phase.
// Full attestation arrays are no longer fetched - we use AttestationPosted events instead.
const abiValidation = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAverageValidationScore","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"attestationCount","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const abiReputation = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAverageScore","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const abiVault = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"hasClaimed","outputs":[{"type":"bool"}],"stateMutability":"view","type":"function"}
]`

// Event signatures.
var (
	sigTradeIntent       = crypto.Keccak256Hash([]byte("TradeIntentSubmitted(uint256,bytes32,string,string,uint256)"))
	sigTradeApproved     = crypto.Keccak256Hash([]byte("TradeApproved(uint256,bytes32,uint256)"))
	sigTradeRejected     = crypto.Keccak256Hash([]byte("TradeRejected(uint256,bytes32,string)"))
	sigFeedbackSubmitted = crypto.Keccak256Hash([]byte("FeedbackSubmitted(uint256,address,uint8,bytes32,uint8)"))
	sigAttestationPosted = crypto.Keccak256Hash([]byte("AttestationPosted(uint256,address,bytes32,uint8,uint8)"))
)

// Event ABIs for decoding non-indexed fields.
const abiIntentEvent = `[
  {"anonymous":false,"name":"TradeIntentSubmitted","type":"event","inputs":[
    {"type":"uint256","name":"agentId","indexed":true},
    {"type":"bytes32","name":"intentHash","indexed":true},
    {"type":"string","name":"pair","indexed":false},
    {"type":"string","name":"action","indexed":false},
    {"type":"uint256","name":"amountUsdScaled","indexed":false}
  ]}
]`

const abiRejectedEvent = `[
  {"anonymous":false,"name":"TradeRejected","type":"event","inputs":[
    {"type":"uint256","name":"agentId","indexed":true},
    {"type":"bytes32","name":"intentHash","indexed":true},
    {"type":"string","name":"reason","indexed":false}
  ]}
]`

// Hackathon ReputationRegistry function ABI for decoding tx calldata.
const abiSubmitFeedback = `[
  {"inputs":[
    {"type":"uint256","name":"agentId"},
    {"type":"uint8","name":"score"},
    {"type":"bytes32","name":"outcomeRef"},
    {"type":"string","name":"comment"},
    {"type":"uint8","name":"feedbackType"}
  ],"name":"submitFeedback","outputs":[],"type":"function"}
]`

// Hackathon ValidationRegistry function ABI for decoding tx calldata (postEIP712Attestation).
// Used to extract the `notes` string from attestation events, same pattern as reputation comments.
const abiPostAttestation = `[
  {"inputs":[
    {"type":"uint256","name":"agentId"},
    {"type":"bytes32","name":"checkpointHash"},
    {"type":"uint8","name":"score"},
    {"type":"string","name":"notes"}
  ],"name":"postEIP712Attestation","outputs":[],"type":"function"}
]`

// AttestationPosted event signature (confirmed by on-chain inspection).
// Event: AttestationPosted(uint256 indexed agentId, address indexed validator, bytes32 indexed checkpointHash, uint8 score, uint8 proofType)
// Topics: [sig, agentId, validator, checkpointHash]
// Data:   [score(32b padded), proofType(32b padded)]
// Notes:  NOT in event, fetched from tx calldata of postEIP712Attestation.

// Phase is a bitmask selecting which sync phases to run.
type Phase int

const (
	PhaseEvents Phase = 1 << iota // scan event logs from block cursor (trade, reputation, attestation)
	PhaseState                    // batched snapshot of current on-chain state (scores, counts, vault)
	PhaseMarket                   // Kraken market data polling

	PhaseAll = PhaseEvents | PhaseState | PhaseMarket
)

// DefaultStateMaxAge is how long the snapshot state is considered fresh.
// Can be overridden per-syncer via NewSyncer(... WithStateMaxAge).
const DefaultStateMaxAge = 5 * time.Minute

// Syncer downloads blockchain data incrementally.
type Syncer struct {
	client      *ethclient.Client
	rpcClient   *rpc.Client // for batch calls
	paths       Paths
	log         *zap.Logger
	stateMaxAge time.Duration // snapshot throttle; 0 means always refresh
	forceState  bool          // bypass throttle this run
}

// NewSyncer creates a blockchain data syncer with default settings.
func NewSyncer(client *ethclient.Client, paths Paths, log *zap.Logger) *Syncer {
	return &Syncer{
		client:      client,
		rpcClient:   client.Client(),
		paths:       paths,
		log:         log,
		stateMaxAge: DefaultStateMaxAge,
	}
}

// SetStateMaxAge configures the snapshot-state throttle window.
// Zero means always refresh (no throttle).
func (s *Syncer) SetStateMaxAge(d time.Duration) { s.stateMaxAge = d }

// SetForceState makes the next sync bypass the state throttle once.
func (s *Syncer) SetForceState(force bool) { s.forceState = force }

// Sync runs all sync phases. Equivalent to SyncPhases(ctx, PhaseAll).
// Preserves backward compatibility with the old single-method API.
func (s *Syncer) Sync(ctx context.Context) error {
	return s.SyncPhases(ctx, PhaseAll)
}

// SyncPhases runs the requested sync phases:
//   - PhaseEvents: scan event logs from the block cursor (trade, reputation, attestation)
//     and resolve new block timestamps. Writes to per-agent JSONL files.
//   - PhaseState: batched snapshot of current on-chain state (scores, counts, vault).
//     Skipped if LastStateRefresh is within stateMaxAge, unless forceState is set.
//   - PhaseMarket: Kraken market data polling per pair (handled in market.go).
//
// Market sync is done by the caller (cmd/agent-intel), not here, because it uses
// its own MarketSyncer. PhaseMarket is accepted for completeness but this function
// just logs its intended execution.
//
// Caller must hold the sync lock via AcquireSyncLock before calling this
// function. This lets main.go coordinate a single lock across Syncer and
// MarketSyncer phases in one run.
func (s *Syncer) SyncPhases(ctx context.Context, phases Phase) error {
	meta, err := LoadMeta(s.paths)
	if err != nil {
		return fmt.Errorf("load meta: %w", err)
	}

	// Crash recovery: drop any JSONL records with block > LastSyncedBlock.
	// A crash mid-chunk can leave events appended on disk that were never
	// checkpointed. Without this rewind, the resumed run refetches the same
	// range and produces duplicates. Idempotent: a no-op on a clean resume.
	if err := s.rewindUncommittedEvents(meta); err != nil {
		return fmt.Errorf("rewind uncommitted events: %w", err)
	}

	// Always call totalAgents() to discover new registrations. Cheap single call.
	regABI := mustParseABI(abiRegistry)
	regAddr := common.HexToAddress(AgentRegistryAddr)
	total, err := s.callUint(ctx, regABI, regAddr, "totalAgents")
	if err != nil {
		return fmt.Errorf("totalAgents: %w", err)
	}
	s.log.Info("Chain agent count", zap.Int64("total", total))

	// Fetch registration metadata for any agents we haven't seen before.
	// This is per-agent and one-shot: registration data never changes.
	if err := s.discoverAgents(ctx, &meta, total); err != nil {
		return fmt.Errorf("discover agents: %w", err)
	}

	// Phase 1: events
	// Chunked outer loop. Each chunk: scan trade + reputation + attestation events,
	// resolve new block timestamps, then CHECKPOINT by advancing meta.LastSyncedBlock
	// and calling SaveMeta. A crash mid-sync leaves meta pointing to the last fully-
	// committed chunk, so the next run resumes from there with no duplicate appends.
	if phases&PhaseEvents != 0 {
		latest, err := s.client.BlockNumber(ctx)
		if err != nil {
			return fmt.Errorf("get block number: %w", err)
		}
		s.log.Info(fmt.Sprintf("Chain at block %d", latest), zap.Uint64("block", latest))

		const hackathonStartBlock = 10580000 // ~Mar 29 2026, before first agent registration
		fromBlock := meta.LastSyncedBlock + 1
		if meta.LastSyncedBlock == 0 {
			fromBlock = hackathonStartBlock
			meta.SyncStartedFrom = fromBlock
		}

		if fromBlock > int64(latest) {
			s.log.Info(fmt.Sprintf("Events already synced to block %d", meta.LastSyncedBlock), zap.Int64("last_synced", meta.LastSyncedBlock))
		} else {
			if err := s.runEventsPhase(ctx, &meta, fromBlock, int64(latest)); err != nil {
				return fmt.Errorf("events phase: %w", err)
			}
		}
	}

	// Phase 2: state (snapshot view calls)
	if phases&PhaseState != 0 {
		if !s.forceState && s.stateMaxAge > 0 && !meta.LastStateRefresh.IsZero() && time.Since(meta.LastStateRefresh) < s.stateMaxAge {
			s.log.Info("State phase skipped (recent)",
				zap.Time("last_refresh", meta.LastStateRefresh),
				zap.Duration("age", time.Since(meta.LastStateRefresh)),
				zap.Duration("max_age", s.stateMaxAge),
			)
		} else {
			// Only mark LastStateRefresh if ALL agents refreshed cleanly.
			// On partial failure, syncState returns an error; we log and keep
			// the old LastStateRefresh so the next sync retries.
			if err := s.syncState(ctx, meta.KnownAgentIDs); err != nil {
				s.log.Warn("State phase had errors - throttle not advanced", zap.Error(err))
				// Non-fatal: save progress so far (per-agent state.json files are
				// already on disk for agents that succeeded). Next run will retry.
			} else {
				meta.LastStateRefresh = time.Now().UTC()
				s.log.Info("State phase complete", zap.Int("agents", len(meta.KnownAgentIDs)))
			}
		}
	}

	// Save meta at the end (includes updated LastSyncedBlock, LastStateRefresh, KnownPairs).
	if err := SaveMeta(s.paths, meta); err != nil {
		return fmt.Errorf("save meta: %w", err)
	}

	return nil
}

// discoverAgents fetches registration metadata for any agents not yet cached.
// Registration data never changes — once fetched, it's cached forever per-agent
// in raw/agents/{id}/info.json. Agents already on disk are not touched.
func (s *Syncer) discoverAgents(ctx context.Context, meta *Meta, total int64) error {
	regABI := mustParseABI(abiRegistry)
	regAddr := common.HexToAddress(AgentRegistryAddr)

	for i := int64(0); i < total; i++ {
		infoPath := s.paths.AgentInfo(i)
		var info Agent
		found, loadErr := LoadJSON(infoPath, &info)
		if loadErr != nil {
			// File exists but is corrupt (truncated JSON, bad permissions, etc.).
			// Refuse to proceed silently — corrupt agent info is a data-integrity
			// issue that needs human attention. The user should delete the file or
			// fix it manually.
			return fmt.Errorf("load agent info %d at %s: %w (delete the file to force re-fetch)", i, infoPath, loadErr)
		}
		if found && info.Name != "" {
			// Already cached. Ensure it's in the known list.
			if !containsInt(meta.KnownAgentIDs, i) {
				meta.KnownAgentIDs = append(meta.KnownAgentIDs, i)
			}
			continue
		}

		// New agent - fetch registration data once.
		var agent Agent
		fetchErr := retryOnRateLimit(ctx, func() error {
			a, err := s.fetchAgentInfo(ctx, regABI, regAddr, i)
			if err != nil {
				return err
			}
			agent = a
			return nil
		})
		if fetchErr != nil {
			s.log.Warn("Failed to fetch agent info", zap.Int64("id", i), zap.Error(fetchErr))
			// Do NOT add to KnownAgentIDs so the next sync will retry the fetch.
			continue
		}
		if err := SaveJSON(infoPath, agent); err != nil {
			return fmt.Errorf("save agent info %d: %w", i, err)
		}
		s.log.Info(fmt.Sprintf("Discovered agent #%d %q", i, agent.Name), zap.Int64("id", i), zap.String("name", agent.Name))

		if !containsInt(meta.KnownAgentIDs, i) {
			meta.KnownAgentIDs = append(meta.KnownAgentIDs, i)
		}
	}
	return nil
}

// fetchAgentInfo calls AgentRegistry.getAgent(id) and returns the registration struct.
// Does NOT touch scores, attestation count, or vault — those are handled by syncState.
func (s *Syncer) fetchAgentInfo(ctx context.Context, regABI abi.ABI, regAddr common.Address, id int64) (Agent, error) {
	bigID := big.NewInt(id)
	agent := Agent{ID: id}

	data, err := regABI.Pack("getAgent", bigID)
	if err != nil {
		return agent, fmt.Errorf("pack getAgent: %w", err)
	}
	result, err := s.client.CallContract(ctx, ethereum.CallMsg{To: &regAddr, Data: data}, nil)
	if err != nil {
		return agent, fmt.Errorf("call getAgent: %w", err)
	}
	vals, err := regABI.Methods["getAgent"].Outputs.Unpack(result)
	if err != nil || len(vals) == 0 {
		return agent, fmt.Errorf("unpack getAgent: %w", err)
	}

	v := reflect.ValueOf(vals[0])
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("Name"); f.IsValid() {
			agent.Name = f.String()
		}
		if f := v.FieldByName("OperatorWallet"); f.IsValid() {
			agent.OperatorWallet = f.Interface().(common.Address).Hex()
		}
		if f := v.FieldByName("AgentWallet"); f.IsValid() {
			agent.AgentWallet = f.Interface().(common.Address).Hex()
		}
		if f := v.FieldByName("Description"); f.IsValid() {
			agent.Description = f.String()
		}
		if f := v.FieldByName("RegisteredAt"); f.IsValid() {
			agent.RegisteredAt = f.Interface().(*big.Int).Int64()
		}
		if f := v.FieldByName("Active"); f.IsValid() {
			agent.Active = f.Bool()
		}
		if f := v.FieldByName("Capabilities"); f.IsValid() {
			agent.Capabilities = f.Interface().([]string)
		}
	}
	return agent, nil
}

// syncState runs the snapshot state phase: batches 4 read calls per agent
// (validation score, attestation count, reputation score, vault claimed)
// into a single JSON-RPC batch request per 50 agents (= 200 calls per batch).
// Writes one raw/agents/{id}/state.json per agent.
//
// Empty re-sync cost: 1 batched HTTP request regardless of agent count.
func (s *Syncer) syncState(ctx context.Context, agentIDs []int64) error {
	if len(agentIDs) == 0 {
		return nil
	}

	valABI := mustParseABI(abiValidation)
	valAddr := common.HexToAddress(ValidationRegistryAddr)
	repABI := mustParseABI(abiReputation)
	repAddr := common.HexToAddress(ReputationRegistryAddr)
	vaultABI := mustParseABI(abiVault)
	vaultAddr := common.HexToAddress(HackathonVaultAddr)

	// Build 4 calls per agent, in a stable order.
	calls := make([]batchCall, 0, 4*len(agentIDs))
	for _, id := range agentIDs {
		bigID := big.NewInt(id)
		calls = append(calls,
			batchCall{Addr: valAddr, ABI: valABI, Method: "getAverageValidationScore", Args: []any{bigID}},
			batchCall{Addr: valAddr, ABI: valABI, Method: "attestationCount", Args: []any{bigID}},
			batchCall{Addr: repAddr, ABI: repABI, Method: "getAverageScore", Args: []any{bigID}},
			batchCall{Addr: vaultAddr, ABI: vaultABI, Method: "hasClaimed", Args: []any{bigID}},
		)
	}

	results, ok := s.batchEthCall(ctx, calls)

	// Decode results per-agent and save. Accumulate all errors rather than
	// early-returning — we want every agent whose RPC succeeded to get its
	// state.json written, even if agent 3 has a disk error.
	now := time.Now().UTC()
	saved := 0
	rpcSkipped := 0
	diskErrors := 0
	var firstDiskErr error
	for i, id := range agentIDs {
		base := i * 4
		agentOk := ok[base+0] && ok[base+1] && ok[base+2] && ok[base+3]
		if !agentOk {
			rpcSkipped++
			continue
		}
		state := AgentState{
			ID:               id,
			ValidationScore:  int(decodeUint(results[base+0])),
			AttestationCount: int(decodeUint(results[base+1])),
			ReputationScore:  int(decodeUint(results[base+2])),
			VaultClaimed:     decodeBool(results[base+3]),
			RefreshedAt:      now,
		}
		if err := SaveJSON(s.paths.AgentState(id), state); err != nil {
			diskErrors++
			if firstDiskErr == nil {
				firstDiskErr = err
			}
			s.log.Warn("Failed to save agent state", zap.Int64("id", id), zap.Error(err))
			continue
		}
		saved++
	}
	s.log.Info("State phase results",
		zap.Int("saved", saved),
		zap.Int("skipped_rpc_errors", rpcSkipped),
		zap.Int("skipped_disk_errors", diskErrors),
	)
	if rpcSkipped > 0 || diskErrors > 0 {
		if firstDiskErr != nil {
			return fmt.Errorf("%d/%d agents skipped (rpc=%d disk=%d), first disk err: %w",
				rpcSkipped+diskErrors, len(agentIDs), rpcSkipped, diskErrors, firstDiskErr)
		}
		return fmt.Errorf("%d/%d agents skipped due to RPC errors", rpcSkipped, len(agentIDs))
	}
	return nil
}

// batchCall describes one read-only contract call.
type batchCall struct {
	Addr   common.Address
	ABI    abi.ABI
	Method string
	Args   []any
}

// batchEthCall sends N read calls in JSON-RPC batches and returns raw hex
// results aligned to input order plus an ok-mask indicating which calls
// succeeded. Callers MUST check the mask — failed calls yield empty string,
// which would decode to zero and silently corrupt state.
//
// Reliability strategy: the primary pass uses batchSize=50 for throughput.
// If any element fails (partial batch response, transient RPC error), the
// failed elements are collected and retried in additional passes with
// progressively smaller batches and exponential backoff. Only pack errors
// (from bad ABI/args) are permanently not-ok; everything else gets retried.
//
// Typical failure we're defending against: "response batch did not contain
// a response to this call" — some RPC providers drop entries from large
// batches under load. Splitting the retry into smaller batches has a much
// higher hit rate than sending the same big batch again.
func (s *Syncer) batchEthCall(ctx context.Context, calls []batchCall) ([]string, []bool) {
	results := make([]string, len(calls))
	ok := make([]bool, len(calls))
	if len(calls) == 0 {
		return results, ok
	}

	// Pre-pack every call once. Pack errors are permanent (bad ABI/args) and
	// will never be retried.
	packed := make([][]byte, len(calls))
	pending := make([]int, 0, len(calls))
	for i, c := range calls {
		data, err := c.ABI.Pack(c.Method, c.Args...)
		if err != nil {
			s.log.Warn("batchEthCall pack failed", zap.String("method", c.Method), zap.Error(err))
			continue
		}
		packed[i] = data
		pending = append(pending, i)
	}

	// Pass 0 = primary (fast), passes 1..N = retries with shrinking batches.
	// With 4 passes we get effective batch sizes [50, 20, 5, 1] — the final
	// pass isolates any pathological single element.
	passBatchSizes := []int{50, 20, 5, 1}

	for pass, batchSize := range passBatchSizes {
		if len(pending) == 0 {
			break
		}
		if pass > 0 {
			// Backoff before retry pass.
			delay := time.Duration(pass) * 2 * time.Second
			select {
			case <-ctx.Done():
				return results, ok
			case <-time.After(delay):
			}
			s.log.Info("batchEthCall retry pass",
				zap.Int("pass", pass),
				zap.Int("remaining", len(pending)),
				zap.Int("batch_size", batchSize),
			)
		}

		var stillPending []int
		for start := 0; start < len(pending); start += batchSize {
			end := start + batchSize
			if end > len(pending) {
				end = len(pending)
			}
			chunkIdx := pending[start:end]

			elems := make([]rpc.BatchElem, len(chunkIdx))
			rawResults := make([]string, len(chunkIdx))
			for i, origIdx := range chunkIdx {
				c := calls[origIdx]
				elems[i] = rpc.BatchElem{
					Method: "eth_call",
					Args: []any{
						map[string]any{
							"to":   c.Addr.Hex(),
							"data": "0x" + common.Bytes2Hex(packed[origIdx]),
						},
						"latest",
					},
					Result: &rawResults[i],
				}
			}

			err := retryOnRateLimit(ctx, func() error {
				return s.rpcClient.BatchCallContext(ctx, elems)
			})
			if err != nil {
				s.log.Warn("batchEthCall batch failed after rate-limit retries",
					zap.Error(err), zap.Int("batch_size", len(chunkIdx)))
				stillPending = append(stillPending, chunkIdx...)
				continue
			}

			for i, elem := range elems {
				origIdx := chunkIdx[i]
				if elem.Error != nil {
					s.log.Debug("batchEthCall element error",
						zap.Int("idx", origIdx), zap.Error(elem.Error))
					stillPending = append(stillPending, origIdx)
					continue
				}
				results[origIdx] = rawResults[i]
				ok[origIdx] = true
			}
		}
		pending = stillPending
	}

	if len(pending) > 0 {
		s.log.Warn("batchEthCall elements still failed after all retry passes",
			zap.Int("failed", len(pending)),
			zap.Int("total", len(calls)),
		)
	}
	return results, ok
}

// decodeUint parses a hex-encoded eth_call result as an int64. Returns 0 on
// empty/error. If the contract returns a uint256 larger than int64 max, the
// value is clamped to int64 max rather than silently wrapping to a negative.
func decodeUint(hex string) int64 {
	if hex == "" || hex == "0x" {
		return 0
	}
	b := common.FromHex(hex)
	if len(b) == 0 {
		return 0
	}
	n := new(big.Int).SetBytes(b)
	if !n.IsInt64() {
		return math.MaxInt64
	}
	return n.Int64()
}

// decodeBool parses a hex-encoded eth_call result as a bool (last byte != 0).
func decodeBool(hex string) bool {
	if hex == "" || hex == "0x" {
		return false
	}
	b := common.FromHex(hex)
	if len(b) == 0 {
		return false
	}
	return b[len(b)-1] != 0
}

// rewindUncommittedEvents drops JSONL records with block > meta.LastSyncedBlock
// across every agent directory on disk. This undoes partial appends written
// by a prior crashed sync before its chunk checkpoint advanced, so the resumed
// run never produces duplicates.
//
// Scans the filesystem rather than meta.KnownAgentIDs because a crashed run
// could have written events for a newly-discovered agent before SaveMeta got
// a chance to persist it in KnownAgentIDs.
//
// No-op on a clean resume (every TrimJSONLAfterBlock returns 0 dropped).
func (s *Syncer) rewindUncommittedEvents(meta Meta) error {
	agentsDir := filepath.Dir(s.paths.AgentDir(0))
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read agents dir %s: %w", agentsDir, err)
	}

	fileFns := []func(int64) string{
		s.paths.AgentIntents,
		s.paths.AgentApprovals,
		s.paths.AgentRejections,
		s.paths.AgentAttestations,
		s.paths.AgentReputation,
	}

	totalDropped := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, err := strconv.ParseInt(e.Name(), 10, 64)
		if err != nil {
			continue // skip non-numeric dirs
		}
		for _, fn := range fileFns {
			n, err := TrimJSONLAfterBlock(fn(id), meta.LastSyncedBlock)
			if err != nil {
				return fmt.Errorf("trim %s: %w", fn(id), err)
			}
			if n > 0 {
				totalDropped += n
				s.log.Warn("Rewound uncommitted JSONL records",
					zap.Int64("agent", id),
					zap.String("file", fn(id)),
					zap.Int("dropped", n),
				)
			}
		}
	}
	if totalDropped > 0 {
		s.log.Warn("Crash recovery: dropped uncommitted event records",
			zap.Int("total_dropped", totalDropped),
			zap.Int64("last_synced_block", meta.LastSyncedBlock),
		)
	}
	return nil
}

// runEventsPhase drives the chunked event-scan loop. For each chunk it scans
// all three event families (trade, reputation, attestation), resolves new block
// timestamps, and then CHECKPOINTS meta.LastSyncedBlock + SaveMeta. On crash,
// the next run resumes from the last fully-committed chunk with no duplicates.
func (s *Syncer) runEventsPhase(ctx context.Context, meta *Meta, fromBlock, toBlock int64) error {
	const chunkSize = int64(10000)

	for chunkStart := fromBlock; chunkStart <= toBlock; chunkStart += chunkSize {
		chunkEnd := chunkStart + chunkSize - 1
		if chunkEnd > toBlock {
			chunkEnd = toBlock
		}

		// Track block numbers that need timestamps within this chunk.
		neededBlocks := make(map[int64]bool)

		// Trade events → per-agent files.
		tradeResult, err := s.syncTradeEventsChunk(ctx, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("trade events chunk [%d,%d]: %w", chunkStart, chunkEnd, err)
		}
		for _, p := range tradeResult.NewPairs {
			canonical := CanonicalPair(p)
			if !containsStr(meta.KnownPairs, canonical) {
				meta.KnownPairs = append(meta.KnownPairs, canonical)
				s.log.Info(fmt.Sprintf("New trading pair discovered: %s", canonical), zap.String("raw", p), zap.String("canonical", canonical))
			}
		}
		for b := range tradeResult.BlocksSeen {
			neededBlocks[b] = true
		}

		// Reputation events → per-agent files.
		repBlocks, err := s.syncReputationEventsChunk(ctx, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("reputation events chunk [%d,%d]: %w", chunkStart, chunkEnd, err)
		}
		for b := range repBlocks {
			neededBlocks[b] = true
		}

		// Attestation events → per-agent files.
		attBlocks, err := s.syncAttestationEventsChunk(ctx, chunkStart, chunkEnd)
		if err != nil {
			return fmt.Errorf("attestation events chunk [%d,%d]: %w", chunkStart, chunkEnd, err)
		}
		for b := range attBlocks {
			neededBlocks[b] = true
		}

		// Resolve timestamps for new blocks in this chunk plus any pending
		// blocks left over from previous failed syncs.
		if len(neededBlocks) > 0 || len(meta.PendingBlocks) > 0 {
			if err := s.resolveNewBlocks(ctx, meta, neededBlocks); err != nil {
				s.log.Warn("Block timestamp resolution partial", zap.Error(err))
				// Non-fatal: unresolved blocks are persisted in meta.PendingBlocks.
			}
		}

		// CHECKPOINT: advance cursor and persist. If we crash after this, the next
		// run starts from chunkEnd+1 and never re-fetches these events.
		meta.LastSyncedBlock = chunkEnd
		meta.LastSyncTime = time.Now().UTC()
		if err := SaveMeta(s.paths, *meta); err != nil {
			return fmt.Errorf("checkpoint meta at block %d: %w", chunkEnd, err)
		}
	}
	return nil
}

// tradeChunkResult is what syncTradeEventsChunk returns from one block range.
type tradeChunkResult struct {
	NewPairs   []string       // canonical pairs discovered in this chunk
	BlocksSeen map[int64]bool // block numbers of events in this chunk
}

// syncTradeEventsChunk scans a single block range for trade events and appends
// them to per-agent JSONL files.
func (s *Syncer) syncTradeEventsChunk(ctx context.Context, fromBlock, toBlock int64) (tradeChunkResult, error) {
	rtrAddr := common.HexToAddress(RiskRouterAddr)
	intentABI := mustParseABI(abiIntentEvent)
	rejectedABI := mustParseABI(abiRejectedEvent)

	result := tradeChunkResult{BlocksSeen: make(map[int64]bool)}

	from := big.NewInt(fromBlock)
	to := big.NewInt(toBlock)

	// Per-agent accumulators for this chunk so file appends are batched.
	intentsByAgent := make(map[int64][]any)
	approvalsByAgent := make(map[int64][]any)
	rejectionsByAgent := make(map[int64][]any)

	// Intents.
	intentLogs, err := s.filterLogsRetry(ctx, ethereum.FilterQuery{
		FromBlock: from, ToBlock: to,
		Addresses: []common.Address{rtrAddr},
		Topics:    [][]common.Hash{{sigTradeIntent}},
	})
	if err != nil {
		return result, fmt.Errorf("fetch intent logs: %w", err)
	}
	for _, log := range intentLogs {
		if len(log.Topics) < 3 {
			continue
		}
		agentID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Int64()
		intentHash := log.Topics[2].Hex()
		vals, err := intentABI.Events["TradeIntentSubmitted"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil || len(vals) < 3 {
			continue
		}
		pair, _ := vals[0].(string)
		action, _ := vals[1].(string)
		amountScaled, _ := vals[2].(*big.Int)
		canonical := CanonicalPair(pair)
		if !containsStr(result.NewPairs, canonical) {
			result.NewPairs = append(result.NewPairs, canonical)
		}
		result.BlocksSeen[int64(log.BlockNumber)] = true

		// Guard against uint256 overflow. In practice amounts are small USD
		// values (×100 = scaled cents) but a buggy or adversarial contract
		// could emit arbitrary uint256. Clamp rather than silently wrap.
		var amount int64
		if amountScaled != nil {
			if amountScaled.IsInt64() {
				amount = amountScaled.Int64()
			} else {
				s.log.Warn("amountUSDScaled overflow, clamping",
					zap.Int64("agent", agentID),
					zap.String("value", amountScaled.String()),
					zap.String("tx", log.TxHash.Hex()),
				)
				amount = math.MaxInt64
			}
		}

		intentsByAgent[agentID] = append(intentsByAgent[agentID], TradeIntent{
			Block:           int64(log.BlockNumber),
			LogIndex:        int(log.Index),
			TxHash:          log.TxHash.Hex(),
			AgentID:         agentID,
			IntentHash:      intentHash,
			Pair:            pair,
			CanonicalPair:   canonical,
			Action:          action,
			AmountUSDScaled: amount,
		})
	}

	// Approvals.
	approvedLogs, err := s.filterLogsRetry(ctx, ethereum.FilterQuery{
		FromBlock: from, ToBlock: to,
		Addresses: []common.Address{rtrAddr},
		Topics:    [][]common.Hash{{sigTradeApproved}},
	})
	if err != nil {
		return result, fmt.Errorf("fetch approval logs: %w", err)
	}
	for _, log := range approvedLogs {
		if len(log.Topics) < 2 {
			continue
		}
		agentID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Int64()
		intentHash := ""
		if len(log.Topics) >= 3 {
			intentHash = log.Topics[2].Hex()
		}
		result.BlocksSeen[int64(log.BlockNumber)] = true
		approvalsByAgent[agentID] = append(approvalsByAgent[agentID], TradeOutcome{
			Block:      int64(log.BlockNumber),
			LogIndex:   int(log.Index),
			TxHash:     log.TxHash.Hex(),
			AgentID:    agentID,
			IntentHash: intentHash,
			Approved:   true,
		})
	}

	// Rejections.
	rejectedLogs, err := s.filterLogsRetry(ctx, ethereum.FilterQuery{
		FromBlock: from, ToBlock: to,
		Addresses: []common.Address{rtrAddr},
		Topics:    [][]common.Hash{{sigTradeRejected}},
	})
	if err != nil {
		return result, fmt.Errorf("fetch rejection logs: %w", err)
	}
	for _, log := range rejectedLogs {
		if len(log.Topics) < 2 {
			continue
		}
		agentID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Int64()
		intentHash := ""
		if len(log.Topics) >= 3 {
			intentHash = log.Topics[2].Hex()
		}
		reason := "unknown"
		if vals, err := rejectedABI.Events["TradeRejected"].Inputs.NonIndexed().Unpack(log.Data); err == nil && len(vals) > 0 {
			if s, ok := vals[0].(string); ok {
				reason = s
			}
		}
		result.BlocksSeen[int64(log.BlockNumber)] = true
		rejectionsByAgent[agentID] = append(rejectionsByAgent[agentID], TradeOutcome{
			Block:      int64(log.BlockNumber),
			LogIndex:   int(log.Index),
			TxHash:     log.TxHash.Hex(),
			AgentID:    agentID,
			IntentHash: intentHash,
			Approved:   false,
			Reason:     reason,
		})
	}

	// Flush per-agent groups (one file append per agent per chunk).
	for id, recs := range intentsByAgent {
		if err := AppendJSONL(s.paths.AgentIntents(id), recs...); err != nil {
			return result, fmt.Errorf("append intents for agent %d: %w", id, err)
		}
	}
	for id, recs := range approvalsByAgent {
		if err := AppendJSONL(s.paths.AgentApprovals(id), recs...); err != nil {
			return result, fmt.Errorf("append approvals for agent %d: %w", id, err)
		}
	}
	for id, recs := range rejectionsByAgent {
		if err := AppendJSONL(s.paths.AgentRejections(id), recs...); err != nil {
			return result, fmt.Errorf("append rejections for agent %d: %w", id, err)
		}
	}

	totalEvents := len(intentLogs) + len(approvedLogs) + len(rejectedLogs)
	if totalEvents > 0 {
		s.log.Info("Synced trade events chunk",
			zap.Int64("from", fromBlock), zap.Int64("to", toBlock),
			zap.Int("intents", len(intentLogs)),
			zap.Int("approvals", len(approvedLogs)),
			zap.Int("rejections", len(rejectedLogs)),
		)
	}
	return result, nil
}

// syncReputationEventsChunk scans a single block range for FeedbackSubmitted
// events, batch-fetches tx calldata for comments, and appends per-agent files.
// Returns the set of block numbers seen in this chunk.
func (s *Syncer) syncReputationEventsChunk(ctx context.Context, fromBlock, toBlock int64) (map[int64]bool, error) {
	repAddr := common.HexToAddress(ReputationRegistryAddr)
	blocks := make(map[int64]bool)

	logs, err := s.filterLogsRetry(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(fromBlock), ToBlock: big.NewInt(toBlock),
		Addresses: []common.Address{repAddr},
		Topics:    [][]common.Hash{{sigFeedbackSubmitted}},
	})
	if err != nil {
		return blocks, fmt.Errorf("fetch reputation logs: %w", err)
	}
	if len(logs) == 0 {
		return blocks, nil
	}

	var feedbacks []ReputationFeedback
	for _, log := range logs {
		agentID, validator, score, outcomeRef, feedbackType, err := decodeReputationEvent(log.Topics, log.Data)
		if err != nil {
			s.log.Debug("Skip reputation event", zap.String("tx", log.TxHash.Hex()), zap.Error(err))
			continue
		}
		blocks[int64(log.BlockNumber)] = true
		feedbacks = append(feedbacks, ReputationFeedback{
			Block:        int64(log.BlockNumber),
			LogIndex:     int(log.Index),
			TxHash:       log.TxHash.Hex(),
			AgentID:      agentID,
			Validator:    validator,
			Score:        score,
			OutcomeRef:   outcomeRef,
			FeedbackType: feedbackType,
		})
	}

	// Batch-fetch comments from tx calldata.
	comments := s.batchFetchComments(ctx, feedbacks)
	for i := range feedbacks {
		feedbacks[i].Comment = comments[feedbacks[i].TxHash]
	}

	// Group by agent and append.
	byAgent := make(map[int64][]ReputationFeedback)
	for _, fb := range feedbacks {
		byAgent[fb.AgentID] = append(byAgent[fb.AgentID], fb)
	}
	for agentID, agentFeedbacks := range byAgent {
		recs := make([]any, len(agentFeedbacks))
		for i := range agentFeedbacks {
			recs[i] = agentFeedbacks[i]
		}
		if err := AppendJSONL(s.paths.AgentReputation(agentID), recs...); err != nil {
			return blocks, fmt.Errorf("append reputation for agent %d: %w", agentID, err)
		}
	}

	s.log.Info("Synced reputation chunk",
		zap.Int64("from", fromBlock), zap.Int64("to", toBlock),
		zap.Int("feedbacks", len(feedbacks)),
	)
	return blocks, nil
}

// syncAttestationEventsChunk scans a single block range for AttestationPosted
// events, batch-fetches tx calldata for notes, and appends per-agent files.
// Returns the set of block numbers seen in this chunk.
//
// Event: AttestationPosted(uint256 indexed agentId, address indexed validator,
//	bytes32 indexed checkpointHash, uint8 score, uint8 proofType)
//
// Notes string is not in the event — it lives in the tx calldata of
// postEIP712Attestation(uint256,bytes32,uint8,string).
func (s *Syncer) syncAttestationEventsChunk(ctx context.Context, fromBlock, toBlock int64) (map[int64]bool, error) {
	valAddr := common.HexToAddress(ValidationRegistryAddr)
	blocks := make(map[int64]bool)

	logs, err := s.filterLogsRetry(ctx, ethereum.FilterQuery{
		FromBlock: big.NewInt(fromBlock), ToBlock: big.NewInt(toBlock),
		Addresses: []common.Address{valAddr},
		Topics:    [][]common.Hash{{sigAttestationPosted}},
	})
	if err != nil {
		return blocks, fmt.Errorf("fetch attestation logs: %w", err)
	}
	if len(logs) == 0 {
		return blocks, nil
	}

	attestations := make([]Attestation, 0, len(logs))
	for _, log := range logs {
		a, err := decodeAttestationEvent(log.Topics, log.Data)
		if err != nil {
			s.log.Debug("Skip attestation event", zap.String("tx", log.TxHash.Hex()), zap.Error(err))
			continue
		}
		a.TxHash = log.TxHash.Hex()
		a.Block = int64(log.BlockNumber)
		a.LogIndex = int(log.Index)
		blocks[int64(log.BlockNumber)] = true
		attestations = append(attestations, a)
	}

	// Batch-fetch notes from tx calldata.
	notes := s.batchFetchAttestationNotes(ctx, attestations)
	for i := range attestations {
		attestations[i].Notes = notes[attestations[i].TxHash]
	}

	// Group by agent and append.
	byAgent := make(map[int64][]Attestation)
	for _, a := range attestations {
		byAgent[a.AgentID] = append(byAgent[a.AgentID], a)
	}
	for agentID, list := range byAgent {
		recs := make([]any, len(list))
		for i := range list {
			recs[i] = list[i]
		}
		if err := AppendJSONL(s.paths.AgentAttestations(agentID), recs...); err != nil {
			return blocks, fmt.Errorf("append attestations for agent %d: %w", agentID, err)
		}
	}

	s.log.Info("Synced attestation chunk",
		zap.Int64("from", fromBlock), zap.Int64("to", toBlock),
		zap.Int("attestations", len(attestations)),
	)
	return blocks, nil
}

// decodeAttestationEvent parses an AttestationPosted event.
// Topics: [sig, agentId, validator, checkpointHash]
// Data:   [score(32b), proofType(32b)]  (64 bytes total)
func decodeAttestationEvent(topics []common.Hash, data []byte) (Attestation, error) {
	var a Attestation
	if len(topics) < 4 {
		return a, fmt.Errorf("need 4 topics, got %d", len(topics))
	}
	if len(data) < 64 {
		return a, fmt.Errorf("need 64 data bytes, got %d", len(data))
	}
	a.AgentID = new(big.Int).SetBytes(topics[1].Bytes()).Int64()
	a.Validator = common.BytesToAddress(topics[2].Bytes()).Hex()
	a.CheckpointHash = topics[3].Hex()
	a.Score = int(data[31])
	a.ProofType = int(data[63])
	return a, nil
}

// batchFetchAttestationNotes fetches tx calldata for a list of attestations and
// extracts the `notes` field from postEIP712Attestation calls. Same pattern as
// batchFetchComments. Returns a map of txHash -> notes.
func (s *Syncer) batchFetchAttestationNotes(ctx context.Context, atts []Attestation) map[string]string {
	seen := make(map[string]bool)
	var txHashes []string
	for _, a := range atts {
		if !seen[a.TxHash] {
			seen[a.TxHash] = true
			txHashes = append(txHashes, a.TxHash)
		}
	}
	if len(txHashes) == 0 {
		return nil
	}

	notes := make(map[string]string)
	parsedABI := mustParseABI(abiPostAttestation)
	batchSize := 50

	for i := 0; i < len(txHashes); i += batchSize {
		end := i + batchSize
		if end > len(txHashes) {
			end = len(txHashes)
		}
		batch := txHashes[i:end]

		elems := make([]rpc.BatchElem, len(batch))
		results := make([]json.RawMessage, len(batch))
		for j, hash := range batch {
			elems[j] = rpc.BatchElem{
				Method: "eth_getTransactionByHash",
				Args:   []any{hash},
				Result: &results[j],
			}
		}

		err := retryOnRateLimit(ctx, func() error {
			return s.rpcClient.BatchCallContext(ctx, elems)
		})
		if err != nil {
			s.log.Warn("Batch attestation notes fetch failed", zap.Error(err), zap.Int("batch_size", len(batch)))
			continue
		}

		for j, elem := range elems {
			if elem.Error != nil {
				continue
			}
			note := extractAttestationNoteFromTxJSON(results[j], parsedABI)
			if note != "" {
				notes[batch[j]] = note
			}
		}

		time.Sleep(500 * time.Millisecond)
	}
	return notes
}

// extractAttestationNoteFromTxJSON decodes the notes field from a raw
// eth_getTransactionByHash response, assuming the tx called postEIP712Attestation.
func extractAttestationNoteFromTxJSON(raw json.RawMessage, parsedABI abi.ABI) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var tx struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(raw, &tx); err != nil || len(tx.Input) < 10 {
		return ""
	}
	data := common.FromHex(tx.Input)
	if len(data) < 4 {
		return ""
	}
	method, err := parsedABI.MethodById(data[:4])
	if err != nil {
		return ""
	}
	args, err := method.Inputs.Unpack(data[4:])
	if err != nil || len(args) < 4 {
		return ""
	}
	notes, _ := args[3].(string)
	return notes
}

// decodeReputationEvent parses a FeedbackSubmitted event.
// Event: FeedbackSubmitted(uint256 indexed agentId, address indexed validator, uint8 score, bytes32 outcomeRef, uint8 feedbackType)
func decodeReputationEvent(topics []common.Hash, data []byte) (agentID int64, validator string, score int, outcomeRef string, feedbackType int, err error) {
	if len(topics) < 3 {
		return 0, "", 0, "", 0, fmt.Errorf("need 3 topics, got %d", len(topics))
	}
	if len(data) < 96 {
		return 0, "", 0, "", 0, fmt.Errorf("need 96 data bytes, got %d", len(data))
	}

	agentID = new(big.Int).SetBytes(topics[1].Bytes()).Int64()
	validator = common.BytesToAddress(topics[2].Bytes()).Hex()
	score = int(data[31])             // uint8 in last byte of first 32-byte word
	outcomeRef = common.BytesToHash(data[32:64]).Hex()
	feedbackType = int(data[95])      // uint8 in last byte of third 32-byte word

	return agentID, validator, score, outcomeRef, feedbackType, nil
}

// batchFetchComments fetches tx calldata in batches and extracts the comment field.
// Returns a map of txHash -> comment. Missing or failed fetches result in empty strings.
func (s *Syncer) batchFetchComments(ctx context.Context, feedbacks []ReputationFeedback) map[string]string {
	// Collect unique tx hashes.
	seen := make(map[string]bool)
	var txHashes []string
	for _, fb := range feedbacks {
		if !seen[fb.TxHash] {
			seen[fb.TxHash] = true
			txHashes = append(txHashes, fb.TxHash)
		}
	}

	if len(txHashes) == 0 {
		return nil
	}

	comments := make(map[string]string)
	parsedABI := mustParseABI(abiSubmitFeedback)
	batchSize := 50

	for i := 0; i < len(txHashes); i += batchSize {
		end := i + batchSize
		if end > len(txHashes) {
			end = len(txHashes)
		}
		batch := txHashes[i:end]

		elems := make([]rpc.BatchElem, len(batch))
		results := make([]json.RawMessage, len(batch))
		for j, hash := range batch {
			elems[j] = rpc.BatchElem{
				Method: "eth_getTransactionByHash",
				Args:   []interface{}{hash},
				Result: &results[j],
			}
		}

		err := retryOnRateLimit(ctx, func() error {
			return s.rpcClient.BatchCallContext(ctx, elems)
		})
		if err != nil {
			s.log.Warn("Batch tx fetch failed after retries", zap.Error(err), zap.Int("batch_size", len(batch)))
			continue
		}

		for j, elem := range elems {
			if elem.Error != nil {
				continue
			}
			comment := extractCommentFromTxJSON(results[j], parsedABI)
			if comment != "" {
				comments[batch[j]] = comment
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return comments
}

// extractCommentFromTxJSON decodes the comment from a raw eth_getTransactionByHash response.
func extractCommentFromTxJSON(raw json.RawMessage, parsedABI abi.ABI) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var tx struct {
		Input string `json:"input"`
	}
	if err := json.Unmarshal(raw, &tx); err != nil || len(tx.Input) < 10 {
		return ""
	}

	data := common.FromHex(tx.Input)
	if len(data) < 4 {
		return ""
	}

	method, err := parsedABI.MethodById(data[:4])
	if err != nil {
		return ""
	}

	args, err := method.Inputs.Unpack(data[4:])
	if err != nil || len(args) < 5 {
		return ""
	}

	comment, _ := args[3].(string)
	return comment
}

// resolveNewBlocks fetches timestamps for the given block numbers (plus any
// meta.PendingBlocks from previous syncs) and persists the updated cache.
//
// Does NOT mutate the caller's blocks map. Unresolved blocks are persisted
// in meta.PendingBlocks and retried on subsequent syncs. After
// MaxPendingBlockAttempts failed tries, a block is moved to
// meta.UnresolvableBlocks and dropped from the retry set — this prevents
// unbounded growth for permanently-pruned blocks.
func (s *Syncer) resolveNewBlocks(ctx context.Context, meta *Meta, blocks map[int64]bool) error {
	// Build the full set of blocks to attempt: new blocks from this chunk +
	// existing pending blocks. Copy into a local set to avoid mutating caller's map.
	target := make(map[int64]bool, len(blocks)+len(meta.PendingBlocks))
	for b := range blocks {
		if b > 0 {
			target[b] = true
		}
	}
	for _, b := range meta.PendingBlocks {
		if b > 0 {
			target[b] = true
		}
	}
	if len(target) == 0 {
		return nil
	}

	ts, err := LoadBlockTimestamps(s.paths)
	if err != nil {
		return err
	}

	// Filter out already-cached blocks.
	missing := make([]int64, 0, len(target))
	for b := range target {
		if _, ok := ts[fmt.Sprintf("%d", b)]; !ok {
			missing = append(missing, b)
		}
	}
	if len(missing) == 0 {
		meta.PendingBlocks = nil
		meta.PendingBlockAttempts = nil
		return SaveBlockTimestamps(s.paths, ts)
	}

	if meta.PendingBlockAttempts == nil {
		meta.PendingBlockAttempts = make(map[int64]int)
	}

	s.log.Info("Resolving block timestamps",
		zap.Int("count", len(missing)),
		zap.Int("from_pending", len(meta.PendingBlocks)),
	)

	var stillPending []int64
	var gaveUp []int64
	for i, block := range missing {
		if i > 0 && i%100 == 0 {
			s.log.Info("Resolving timestamps", zap.Int("done", i), zap.Int("total", len(missing)))
		}
		if err := s.fetchBlockTimestamp(ctx, ts, block); err != nil {
			meta.PendingBlockAttempts[block]++
			if meta.PendingBlockAttempts[block] >= MaxPendingBlockAttempts {
				gaveUp = append(gaveUp, block)
				delete(meta.PendingBlockAttempts, block)
				s.log.Warn("Giving up on block timestamp after max attempts",
					zap.Int64("block", block),
					zap.Int("attempts", MaxPendingBlockAttempts),
				)
			} else {
				stillPending = append(stillPending, block)
			}
		} else {
			// Success - clear any prior attempt count.
			delete(meta.PendingBlockAttempts, block)
		}
	}

	meta.PendingBlocks = stillPending
	if len(gaveUp) > 0 {
		meta.UnresolvableBlocks = append(meta.UnresolvableBlocks, gaveUp...)
	}
	if len(stillPending) > 0 {
		s.log.Warn("Block timestamps still pending",
			zap.Int("count", len(stillPending)),
			zap.String("note", "will retry on next sync"),
		)
	}
	if len(meta.UnresolvableBlocks) > 0 {
		s.log.Warn("Permanently unresolvable blocks",
			zap.Int("total", len(meta.UnresolvableBlocks)),
			zap.String("note", "trades in these blocks have no timestamp and are excluded from PnL"),
		)
	}

	return SaveBlockTimestamps(s.paths, ts)
}

// --- helpers ---

// callUint runs a single read-only uint256 eth_call with retry on rate limits.
// Returns an error on any failure so callers can decide whether to abort.
// For bulk queries, use batchEthCall which sends many calls in one JSON-RPC batch.
func (s *Syncer) callUint(ctx context.Context, parsed abi.ABI, addr common.Address, method string, args ...any) (int64, error) {
	data, err := parsed.Pack(method, args...)
	if err != nil {
		return 0, fmt.Errorf("pack %s: %w", method, err)
	}
	var result []byte
	err = retryOnRateLimit(ctx, func() error {
		var cerr error
		result, cerr = s.client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, nil)
		return cerr
	})
	if err != nil {
		return 0, fmt.Errorf("call %s: %w", method, err)
	}
	vals, err := parsed.Unpack(method, result)
	if err != nil || len(vals) == 0 {
		return 0, fmt.Errorf("unpack %s: %w", method, err)
	}
	if v, ok := vals[0].(*big.Int); ok {
		return v.Int64(), nil
	}
	return 0, fmt.Errorf("unexpected type for %s", method)
}

func mustParseABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("parse abi: %v", err))
	}
	return parsed
}

// fetchBlockTimestamp fetches a single block timestamp with retry on rate limit.
func (s *Syncer) fetchBlockTimestamp(ctx context.Context, ts BlockTimestamps, block int64) error {
	return retryOnRateLimit(ctx, func() error {
		header, err := s.client.HeaderByNumber(ctx, big.NewInt(block))
		if err != nil {
			return err
		}
		ts[fmt.Sprintf("%d", block)] = int64(header.Time)
		return nil
	})
}

// filterLogsRetry wraps FilterLogs with retry on rate limit.
// Auto-bisects the block range when the RPC returns "more than 10000 results".
func (s *Syncer) filterLogsRetry(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	return s.filterLogsWithBisect(ctx, q, 0)
}

const maxBisectDepth = 8

func (s *Syncer) filterLogsWithBisect(ctx context.Context, q ethereum.FilterQuery, depth int) ([]types.Log, error) {
	var logs []types.Log
	err := retryOnRateLimit(ctx, func() error {
		var ferr error
		logs, ferr = s.client.FilterLogs(ctx, q)
		return ferr
	})
	if err == nil {
		return logs, nil
	}
	if !isResultOverflow(err) || depth >= maxBisectDepth {
		return nil, err
	}

	from := q.FromBlock.Int64()
	to := q.ToBlock.Int64()
	if from >= to {
		return nil, err
	}
	mid := from + (to-from)/2

	s.log.Debug("Bisecting log query", zap.Int64("from", from), zap.Int64("mid", mid), zap.Int64("to", to))

	qLeft := q
	qLeft.FromBlock = big.NewInt(from)
	qLeft.ToBlock = big.NewInt(mid)
	left, err := s.filterLogsWithBisect(ctx, qLeft, depth+1)
	if err != nil {
		return nil, err
	}

	// Brief pause between halves to avoid rate-limiting on bisect fan-out.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(500 * time.Millisecond):
	}

	qRight := q
	qRight.FromBlock = big.NewInt(mid + 1)
	qRight.ToBlock = big.NewInt(to)
	right, err := s.filterLogsWithBisect(ctx, qRight, depth+1)
	if err != nil {
		return nil, err
	}

	return append(left, right...), nil
}

// retryOnRateLimit retries fn up to 5 times with exponential backoff on rate-limit errors.
// Returns immediately on success, non-rate-limit errors, or context cancellation.
func retryOnRateLimit(ctx context.Context, fn func() error) error {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if !isRateLimited(err) {
			return err
		}
		delay := time.Duration(attempt+1) * 2 * time.Second
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}
	return fmt.Errorf("rate limited after 5 retries: %w", err)
}

// isRateLimited checks if an error is a 429 Too Many Requests.
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "429") || strings.Contains(msg, "Too Many Requests") || strings.Contains(msg, "rate limit")
}

func isResultOverflow(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "more than 10000 results")
}

func containsInt(slice []int64, val int64) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

func containsStr(slice []string, val string) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}
