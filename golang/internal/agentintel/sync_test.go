package agentintel

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestDecodeReputationEvent(t *testing.T) {
	// Build topics: [eventSig, agentId=5, validator=0x84f8d24c...]
	eventSig := crypto.Keccak256Hash([]byte("FeedbackSubmitted(uint256,address,uint8,bytes32,uint8)"))
	agentTopic := common.BigToHash(big.NewInt(5))
	validatorAddr := common.HexToAddress("0x84f8d24ccb005302e79bda40942cd4429820f2a3")
	validatorTopic := common.BytesToHash(validatorAddr.Bytes())

	// Build data: score=76 (uint8 padded to 32), outcomeRef (bytes32), feedbackType=3 (uint8 padded to 32)
	// Matches real on-chain data from block 10603733.
	buildData := func(score byte, feedbackType byte) []byte {
		data := make([]byte, 96)
		data[31] = score                                                   // uint8 score
		copy(data[32:64], common.FromHex("8940ef7fc462921840928e8c870906bed074078e755623f08f67dcee3268814a")) // outcomeRef
		data[95] = feedbackType                                            // uint8 feedbackType
		return data
	}

	tests := []struct {
		name         string
		topics       []common.Hash
		data         []byte
		wantAgent    int64
		wantScore    int
		wantType     int
		wantErr      bool
	}{
		{
			name:      "valid event score=76 type=general",
			topics:    []common.Hash{eventSig, agentTopic, validatorTopic},
			data:      buildData(76, 3),
			wantAgent: 5,
			wantScore: 76,
			wantType:  3,
		},
		{
			name:      "score=0",
			topics:    []common.Hash{eventSig, agentTopic, validatorTopic},
			data:      buildData(0, 0),
			wantAgent: 5,
			wantScore: 0,
			wantType:  0,
		},
		{
			name:      "score=100",
			topics:    []common.Hash{eventSig, agentTopic, validatorTopic},
			data:      buildData(100, 2),
			wantAgent: 5,
			wantScore: 100,
			wantType:  2,
		},
		{
			name:    "too few topics",
			topics:  []common.Hash{eventSig, agentTopic},
			data:    buildData(76, 3),
			wantErr: true,
		},
		{
			name:    "data too short",
			topics:  []common.Hash{eventSig, agentTopic, validatorTopic},
			data:    make([]byte, 64),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agentID, validator, score, outcomeRef, feedbackType, err := decodeReputationEvent(tt.topics, tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if agentID != tt.wantAgent {
				t.Errorf("agentID = %d, want %d", agentID, tt.wantAgent)
			}
			if score != tt.wantScore {
				t.Errorf("score = %d, want %d", score, tt.wantScore)
			}
			if feedbackType != tt.wantType {
				t.Errorf("feedbackType = %d, want %d", feedbackType, tt.wantType)
			}
			if validator != validatorAddr.Hex() {
				t.Errorf("validator = %s, want %s", validator, validatorAddr.Hex())
			}
			if outcomeRef == "" {
				t.Error("outcomeRef is empty")
			}
			// Verify outcomeRef is always 66 chars (0x + 64 hex digits) for bytes32.
			if len(outcomeRef) != 66 {
				t.Errorf("outcomeRef length = %d, want 66 (0x + 64 hex digits)", len(outcomeRef))
			}
		})
	}
}

func TestDecodeReputationCalldata(t *testing.T) {
	parsedABI := mustParseABI(abiSubmitFeedback)

	// Pack a valid submitFeedback call.
	agentId := big.NewInt(18)
	score := uint8(98)
	outcomeRef := [32]byte{0x89, 0x40, 0xef}
	comment := "HOLD WETH/USDC $2070 | validated cycle 1400"
	feedbackType := uint8(3)

	packABI, _ := abi.JSON(strings.NewReader(abiSubmitFeedback))
	calldata, err := packABI.Pack("submitFeedback", agentId, score, outcomeRef, comment, feedbackType)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	// Wrap in a minimal tx JSON like eth_getTransactionByHash returns.
	txJSON, _ := json.Marshal(map[string]string{
		"input": fmt.Sprintf("0x%x", calldata),
	})

	got := extractCommentFromTxJSON(json.RawMessage(txJSON), parsedABI)
	if got != comment {
		t.Errorf("comment = %q, want %q", got, comment)
	}
}

func TestDecodeReputationCalldataWrongSelector(t *testing.T) {
	parsedABI := mustParseABI(abiSubmitFeedback)

	// Random calldata with wrong function selector.
	txJSON, _ := json.Marshal(map[string]string{
		"input": "0xdeadbeef0000000000000000000000000000000000000000000000000000000000000001",
	})

	got := extractCommentFromTxJSON(json.RawMessage(txJSON), parsedABI)
	if got != "" {
		t.Errorf("expected empty comment for wrong selector, got %q", got)
	}
}

func TestRetryOnRateLimit(t *testing.T) {
	tests := []struct {
		name         string
		fn           func() (int, error)
		wantAttempts int
		wantErr      bool
	}{
		{
			name: "success on first try",
			fn: func() func() (int, error) {
				attempts := 0
				return func() (int, error) {
					attempts++
					return attempts, nil
				}
			}(),
			wantAttempts: 1,
		},
		{
			name: "rate limited twice then success",
			fn: func() func() (int, error) {
				attempts := 0
				return func() (int, error) {
					attempts++
					if attempts <= 2 {
						return attempts, fmt.Errorf("429 Too Many Requests")
					}
					return attempts, nil
				}
			}(),
			wantAttempts: 3,
		},
		{
			name: "non-rate-limit error returns immediately",
			fn: func() func() (int, error) {
				attempts := 0
				return func() (int, error) {
					attempts++
					return attempts, fmt.Errorf("connection refused")
				}
			}(),
			wantAttempts: 1,
			wantErr:      true,
		},
		{
			name: "all 5 attempts rate limited",
			fn: func() func() (int, error) {
				attempts := 0
				return func() (int, error) {
					attempts++
					return attempts, fmt.Errorf("rate limit exceeded")
				}
			}(),
			wantAttempts: 5,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lastAttempt int
			err := retryOnRateLimit(context.Background(), func() error {
				var ferr error
				lastAttempt, ferr = tt.fn()
				return ferr
			})
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lastAttempt != tt.wantAttempts {
				t.Errorf("attempts = %d, want %d", lastAttempt, tt.wantAttempts)
			}
		})
	}
}

func TestDecodeAttestationEvent(t *testing.T) {
	eventSig := crypto.Keccak256Hash([]byte("AttestationPosted(uint256,address,bytes32,uint8,uint8)"))
	agentTopic := common.BigToHash(big.NewInt(11))
	validatorAddr := common.HexToAddress("0x92bf63e5c7ac6980f237a7164ab413be226187f1")
	validatorTopic := common.BytesToHash(validatorAddr.Bytes())
	ckptHash := common.HexToHash("0x7d385a0eb9f50fbfc67e93df967606fdcb36921f5e1f6670e110cdb968375a1f")

	// Build data: score (32-byte padded), proofType (32-byte padded).
	buildData := func(score byte, proofType byte) []byte {
		data := make([]byte, 64)
		data[31] = score
		data[63] = proofType
		return data
	}

	tests := []struct {
		name          string
		topics        []common.Hash
		data          []byte
		wantAgent     int64
		wantScore     int
		wantProofType int
		wantErr       bool
	}{
		{
			name:          "valid score=50 proofType=1 (matches on-chain sample)",
			topics:        []common.Hash{eventSig, agentTopic, validatorTopic, ckptHash},
			data:          buildData(0x32, 1),
			wantAgent:     11,
			wantScore:     0x32,
			wantProofType: 1,
		},
		{
			name:          "score=100",
			topics:        []common.Hash{eventSig, agentTopic, validatorTopic, ckptHash},
			data:          buildData(100, 0),
			wantAgent:     11,
			wantScore:     100,
			wantProofType: 0,
		},
		{
			name:    "missing checkpointHash topic",
			topics:  []common.Hash{eventSig, agentTopic, validatorTopic},
			data:    buildData(50, 1),
			wantErr: true,
		},
		{
			name:    "data too short",
			topics:  []common.Hash{eventSig, agentTopic, validatorTopic, ckptHash},
			data:    make([]byte, 32),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a, err := decodeAttestationEvent(tt.topics, tt.data)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if a.AgentID != tt.wantAgent {
				t.Errorf("agentID = %d, want %d", a.AgentID, tt.wantAgent)
			}
			if a.Score != tt.wantScore {
				t.Errorf("score = %d, want %d", a.Score, tt.wantScore)
			}
			if a.ProofType != tt.wantProofType {
				t.Errorf("proofType = %d, want %d", a.ProofType, tt.wantProofType)
			}
			if a.Validator != validatorAddr.Hex() {
				t.Errorf("validator = %s, want %s", a.Validator, validatorAddr.Hex())
			}
			if a.CheckpointHash == "" {
				t.Error("checkpointHash empty")
			}
			if len(a.CheckpointHash) != 66 {
				t.Errorf("checkpointHash length = %d, want 66", len(a.CheckpointHash))
			}
		})
	}
}

func TestDecodeAttestationCalldata(t *testing.T) {
	parsedABI := mustParseABI(abiPostAttestation)

	// Pack a valid postEIP712Attestation call.
	agentId := big.NewInt(11)
	checkpointHash := [32]byte{0x7d, 0x38, 0x5a, 0x0e}
	score := uint8(85)
	notes := "Trade executed: BUY 0.001 BTC at $68500. Swiftward approved, risk score 0.23"

	packABI, _ := abi.JSON(strings.NewReader(abiPostAttestation))
	calldata, err := packABI.Pack("postEIP712Attestation", agentId, checkpointHash, score, notes)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}

	txJSON, _ := json.Marshal(map[string]string{
		"input": fmt.Sprintf("0x%x", calldata),
	})

	got := extractAttestationNoteFromTxJSON(json.RawMessage(txJSON), parsedABI)
	if got != notes {
		t.Errorf("notes = %q, want %q", got, notes)
	}
}

func TestDecodeUintBool(t *testing.T) {
	tests := []struct {
		name string
		hex  string
		want int64
	}{
		{"empty", "", 0},
		{"0x", "0x", 0},
		{"zero padded", "0x0000000000000000000000000000000000000000000000000000000000000000", 0},
		{"one", "0x0000000000000000000000000000000000000000000000000000000000000001", 1},
		{"100", "0x0000000000000000000000000000000000000000000000000000000000000064", 100},
	}
	for _, tt := range tests {
		t.Run("uint_"+tt.name, func(t *testing.T) {
			if got := decodeUint(tt.hex); got != tt.want {
				t.Errorf("decodeUint(%q) = %d, want %d", tt.hex, got, tt.want)
			}
		})
	}

	boolTests := []struct {
		name string
		hex  string
		want bool
	}{
		{"empty", "", false},
		{"false", "0x0000000000000000000000000000000000000000000000000000000000000000", false},
		{"true", "0x0000000000000000000000000000000000000000000000000000000000000001", true},
	}
	for _, tt := range boolTests {
		t.Run("bool_"+tt.name, func(t *testing.T) {
			if got := decodeBool(tt.hex); got != tt.want {
				t.Errorf("decodeBool(%q) = %v, want %v", tt.hex, got, tt.want)
			}
		})
	}
}

func TestHackathonScoreFormula(t *testing.T) {
	tests := []struct {
		name        string
		summary     AgentSummary
		state       AgentState
		wantScore   int
		wantValHalf int
	}{
		{
			name: "perfect 100/100",
			summary: AgentSummary{
				ApprovedTrades:     50,
				AvgValidationScore: 100,
				AvgReputationScore: 100,
				AttestationCount:   500,
			},
			state:       AgentState{VaultClaimed: true},
			wantValHalf: 100, // 50 + 30 + 10 + 10 = 100
			wantScore:   100, // (100 + 100) / 2
		},
		{
			name: "inactive but high rep",
			summary: AgentSummary{
				ApprovedTrades:     0,
				AvgValidationScore: 100,
				AvgReputationScore: 100,
				AttestationCount:   0,
			},
			state:       AgentState{VaultClaimed: false},
			wantValHalf: 50, // 50 + 0 + 0 + 0 = 50
			wantScore:   75, // (50 + 100) / 2
		},
		{
			name: "10-trade cap (11 approved vs 10 approved same score)",
			summary: AgentSummary{
				ApprovedTrades:     11,
				AvgValidationScore: 0,
				AvgReputationScore: 0,
				AttestationCount:   1,
			},
			state:       AgentState{VaultClaimed: false},
			wantValHalf: 40, // 0 + 30 (capped) + 0 + 10 = 40
			wantScore:   20, // (40 + 0) / 2
		},
		{
			name: "vault only (no trades, no attestations)",
			summary: AgentSummary{
				AvgValidationScore: 0,
				AvgReputationScore: 50,
			},
			state:       AgentState{VaultClaimed: true},
			wantValHalf: 10, // 0 + 0 + 10 + 0 = 10
			wantScore:   30, // (10 + 50) / 2
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.summary
			computeHackathonScore(&s, tt.state)
			if s.HackathonValidation != tt.wantValHalf {
				t.Errorf("HackathonValidation = %d, want %d", s.HackathonValidation, tt.wantValHalf)
			}
			if s.HackathonScore != tt.wantScore {
				t.Errorf("HackathonScore = %d, want %d", s.HackathonScore, tt.wantScore)
			}
		})
	}
}

func TestPhaseBitmask(t *testing.T) {
	if PhaseAll != PhaseEvents|PhaseState|PhaseMarket {
		t.Error("PhaseAll should be union of all phases")
	}
	if PhaseEvents&PhaseState != 0 {
		t.Error("PhaseEvents and PhaseState should not overlap")
	}
	if PhaseState&PhaseMarket != 0 {
		t.Error("PhaseState and PhaseMarket should not overlap")
	}
}

func TestReadJSONLTolerantOfTornLastLine(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.jsonl"

	// Write two complete lines plus a partial third line (simulates crash mid-append).
	content := `{"block":100,"log_index":1,"tx_hash":"0xaaa","agent_id":1,"score":50}
{"block":101,"log_index":2,"tx_hash":"0xbbb","agent_id":1,"score":75}
{"block":102,"log_index":3,"tx_hash":"0xccc","agent_id":1,"sco`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ReadJSONL[ReputationFeedback](path)
	if err != nil {
		t.Fatalf("ReadJSONL should tolerate torn last line: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %d records, want 2 (torn last line should be skipped)", len(got))
	}
	if got[0].Score != 50 || got[1].Score != 75 {
		t.Errorf("decoded records wrong: %+v", got)
	}
}

func TestReadJSONLMiddleCorruptionStillFails(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/test.jsonl"

	// Middle line is corrupt — this should still error (real data corruption).
	content := `{"block":100,"log_index":1,"tx_hash":"0xaaa","agent_id":1,"score":50}
this is not valid json
{"block":102,"log_index":3,"tx_hash":"0xccc","agent_id":1,"score":90}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadJSONL[ReputationFeedback](path)
	if err == nil {
		t.Fatal("ReadJSONL should fail on corrupt middle line")
	}
}

func TestTrimJSONLAfterBlock(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		maxBlock    int64
		wantDropped int
		wantLines   []string
	}{
		{
			name: "drops_records_above_threshold",
			content: `{"block":100,"log_index":1,"agent_id":1}
{"block":101,"log_index":1,"agent_id":1}
{"block":200,"log_index":1,"agent_id":1}
{"block":201,"log_index":1,"agent_id":1}
`,
			maxBlock:    150,
			wantDropped: 2,
			wantLines: []string{
				`{"block":100,"log_index":1,"agent_id":1}`,
				`{"block":101,"log_index":1,"agent_id":1}`,
			},
		},
		{
			name: "noop_when_all_below",
			content: `{"block":100,"log_index":1,"agent_id":1}
{"block":101,"log_index":1,"agent_id":1}
`,
			maxBlock:    200,
			wantDropped: 0,
			wantLines: []string{
				`{"block":100,"log_index":1,"agent_id":1}`,
				`{"block":101,"log_index":1,"agent_id":1}`,
			},
		},
		{
			name: "drops_torn_last_line",
			content: `{"block":100,"log_index":1,"agent_id":1}
{"block":101,"log_index":1,"agent_id":1}
{"block":102,"log_in`,
			maxBlock:    200,
			wantDropped: 1,
			wantLines: []string{
				`{"block":100,"log_index":1,"agent_id":1}`,
				`{"block":101,"log_index":1,"agent_id":1}`,
			},
		},
		{
			name: "drops_everything_when_threshold_zero",
			content: `{"block":100,"log_index":1,"agent_id":1}
{"block":101,"log_index":1,"agent_id":1}
`,
			maxBlock:    0,
			wantDropped: 2,
			wantLines:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := dir + "/test.jsonl"
			if err := os.WriteFile(path, []byte(tt.content), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := TrimJSONLAfterBlock(path, tt.maxBlock)
			if err != nil {
				t.Fatalf("TrimJSONLAfterBlock: %v", err)
			}
			if got != tt.wantDropped {
				t.Errorf("dropped = %d, want %d", got, tt.wantDropped)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			// When nothing is dropped the file is left untouched (content as-is).
			// When records are dropped the rewritten file has one record per line
			// with a trailing newline.
			if tt.wantDropped == 0 {
				if string(data) != tt.content {
					t.Errorf("file changed when nothing should drop:\ngot:  %q\nwant: %q", data, tt.content)
				}
				return
			}
			want := ""
			for _, l := range tt.wantLines {
				want += l + "\n"
			}
			if string(data) != want {
				t.Errorf("rewritten file mismatch:\ngot:  %q\nwant: %q", data, want)
			}
		})
	}
}

func TestTrimJSONLAfterBlockMissingFile(t *testing.T) {
	dropped, err := TrimJSONLAfterBlock(t.TempDir()+"/does-not-exist.jsonl", 100)
	if err != nil {
		t.Fatalf("missing file should be a no-op, got: %v", err)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0", dropped)
	}
}

func TestPathHelpersPerAgent(t *testing.T) {
	p := NewPaths("/tmp/test")
	aid := int64(42)

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AgentDir", p.AgentDir(aid), "/tmp/test/data/agent-intel/raw/agents/42"},
		{"AgentInfo", p.AgentInfo(aid), "/tmp/test/data/agent-intel/raw/agents/42/info.json"},
		{"AgentState", p.AgentState(aid), "/tmp/test/data/agent-intel/raw/agents/42/state.json"},
		{"AgentIntents", p.AgentIntents(aid), "/tmp/test/data/agent-intel/raw/agents/42/intents.jsonl"},
		{"AgentApprovals", p.AgentApprovals(aid), "/tmp/test/data/agent-intel/raw/agents/42/approvals.jsonl"},
		{"AgentRejections", p.AgentRejections(aid), "/tmp/test/data/agent-intel/raw/agents/42/rejections.jsonl"},
		{"AgentAttestations", p.AgentAttestations(aid), "/tmp/test/data/agent-intel/raw/agents/42/attestations.jsonl"},
		{"AgentReputation", p.AgentReputation(aid), "/tmp/test/data/agent-intel/raw/agents/42/reputation.jsonl"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.got != c.want {
				t.Errorf("%s = %s, want %s", c.name, c.got, c.want)
			}
		})
	}
}
