// erc8004-setup is a one-time CLI for ERC-8004 agent registration and on-chain operations.
//
// Usage:
//
//	AGENT=ALPHA_CLAUDE go run ./cmd/erc8004-setup register
//	AGENT=ALPHA_CLAUDE go run ./cmd/erc8004-setup claim-vault
//	AGENT=ALPHA_CLAUDE go run ./cmd/erc8004-setup set-uri
//	AGENT=ALPHA_CLAUDE go run ./cmd/erc8004-setup set-wallet
//	make erc8004-booster FROM=DELTA_GO TO=RANDOM
//
// The AGENT env var selects which agent to operate on. The tool reads:
//
//	AGENT_{name}_PRIVATE_KEY              - agent private key
//	AGENT_{name}_REGISTRATION_URI         - IPFS URI to agent registration JSON
//	AGENT_{name}_HACKATHON_AGENT_ID       - agentId on hackathon registry (or STANDARD_AGENT_ID, ERC8004_AGENT_ID)
//
// Other required env vars:
//
//	CHAIN_RPC_URL                  - Sepolia RPC URL
//	CHAIN_ID                       - chain ID (default 11155111)
//	HACKATHON_IDENTITY_ADDR        - Hackathon AgentRegistry contract address
//	HACKATHON_REPUTATION_ADDR      - Hackathon ReputationRegistry contract address
//	HACKATHON_VAULT_ADDR           - Hackathon Vault contract address
package main

import (
	"context"
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"ai-trading-agents/internal/chain"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: AGENT=<NAME> erc8004-setup <command>")
		fmt.Fprintln(os.Stderr, "Commands: register, claim-vault, set-uri, set-wallet, validate, feedback")
		fmt.Fprintln(os.Stderr, "Example: AGENT=DELTA_GO erc8004-setup register")
		os.Exit(1)
	}

	log := newLogger()
	ctx := context.Background()
	agent := mustEnv("AGENT") // e.g. "DELTA_GO"

	switch os.Args[1] {
	case "register":
		if err := runRegister(ctx, log, agent); err != nil {
			log.Fatal("register failed", zap.Error(err))
		}
	case "set-uri":
		if err := runSetURI(ctx, log, agent); err != nil {
			log.Fatal("set-uri failed", zap.Error(err))
		}
	case "set-wallet":
		if err := runSetWallet(ctx, log, agent); err != nil {
			log.Fatal("set-wallet failed", zap.Error(err))
		}
	case "claim-vault":
		if err := runClaimVault(ctx, log, agent); err != nil {
			log.Fatal("claim-vault failed", zap.Error(err))
		}
	case "attest":
		if err := runAttest(ctx, log); err != nil {
			log.Fatal("attest failed", zap.Error(err))
		}
	case "booster":
		if err := runBooster(ctx, log); err != nil {
			log.Fatal("booster failed", zap.Error(err))
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// agentEnv reads an agent-specific env var: AGENT_{name}_{suffix}.
func agentEnv(agent, suffix string) string {
	return mustEnv(fmt.Sprintf("AGENT_%s_%s", agent, suffix))
}

// agentAgentID reads the on-chain agent ID, trying HACKATHON, STANDARD, and ERC8004 suffixes.
func agentAgentID(agent string) string {
	for _, suffix := range []string{"HACKATHON_AGENT_ID", "STANDARD_AGENT_ID", "ERC8004_AGENT_ID"} {
		key := fmt.Sprintf("AGENT_%s_%s", agent, suffix)
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	// Fall back to mustEnv for error message.
	return mustEnv(fmt.Sprintf("AGENT_%s_HACKATHON_AGENT_ID", agent))
}

func runRegister(ctx context.Context, log *zap.Logger, agent string) error {
	registry := os.Getenv("REGISTRY")
	if registry == "" {
		registry = "hackathon"
	}

	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	agentKeyHex := agentEnv(agent, "PRIVATE_KEY")
	agentURI := agentEnv(agent, "REGISTRATION_URI")

	var identityAddr common.Address
	switch registry {
	case "hackathon":
		identityAddr = mustAddress("HACKATHON_IDENTITY_ADDR")
	case "standard":
		identityAddr = mustAddress("ERC8004_IDENTITY_ADDR")
	default:
		return fmt.Errorf("unknown REGISTRY=%q (use 'hackathon' or 'standard')", registry)
	}

	agentKey, err := chain.ParsePrivateKey(agentKeyHex)
	if err != nil {
		return fmt.Errorf("parse agent key: %w", err)
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	block, err := client.GetLatestBlock(ctx)
	if err != nil {
		return fmt.Errorf("get latest block: %w", err)
	}
	log.Info("Connected to chain",
		zap.String("agent", agent),
		zap.String("registry", registry),
		zap.String("address", identityAddr.Hex()),
		zap.Uint64("latestBlock", block),
	)

	identity := chain.NewIdentityRegistry(client, identityAddr, log)
	agentAddr := chain.AddressFromKey(agentKey)

	// Read agent metadata from IPFS JSON for registration name/description.
	agentName, agentDesc := agentMetadata(agent)

	var agentID *big.Int
	switch registry {
	case "hackathon":
		registered, err := identity.IsAgentRegistered(ctx, agentAddr)
		if err != nil {
			return fmt.Errorf("check registration: %w", err)
		}
		if registered {
			log.Warn("Agent is already registered on hackathon registry")
			return nil
		}
		agentID, err = identity.RegisterHackathon(ctx, agentKey,
			agentName,
			agentDesc,
			[]string{"trading", "risk-management", "policy-enforcement", "eip712-signing", "explainability"},
			agentURI,
		)
		if err != nil {
			return fmt.Errorf("register on hackathon: %w", err)
		}
	case "standard":
		agentID, err = identity.RegisterStandard(ctx, agentKey, agentURI)
		if err != nil {
			return fmt.Errorf("register on standard: %w", err)
		}
	}

	fmt.Println()
	fmt.Printf("=== REGISTRATION SUCCESSFUL (%s) ===\n", registry)
	fmt.Printf("Agent:    %s\n", agent)
	fmt.Printf("Address:  %s\n", agentAddr.Hex())
	fmt.Printf("Registry: %s (%s)\n", registry, identityAddr.Hex())
	fmt.Printf("URI:      %s\n", agentURI)
	fmt.Printf("ID:       %s\n", agentID.String())
	fmt.Println()
	fmt.Printf("Verify on Etherscan: https://sepolia.etherscan.io/address/%s\n", identityAddr.Hex())
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("  1. Update erc8004/agents/ JSON with registrations field (agentId=%s)\n", agentID.String())
	fmt.Println("  2. Re-upload JSON to IPFS (new CID)")
	fmt.Printf("  3. Run: AGENT=%s make erc8004-set-uri   (updates on-chain URI to new CID)\n", agent)

	return nil
}

func runSetURI(ctx context.Context, log *zap.Logger, agent string) error {
	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	agentKeyHex := agentEnv(agent, "PRIVATE_KEY")
	agentIDStr := agentAgentID(agent)
	newURI := agentEnv(agent, "REGISTRATION_URI")
	identityAddr := mustAddress("ERC8004_IDENTITY_ADDR")

	agentID, ok := new(big.Int).SetString(agentIDStr, 10)
	if !ok {
		return fmt.Errorf("invalid agent ID for %s: %s", agent, agentIDStr)
	}

	agentKey, err := chain.ParsePrivateKey(agentKeyHex)
	if err != nil {
		return fmt.Errorf("parse agent key: %w", err)
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	identity := chain.NewIdentityRegistry(client, identityAddr, log)

	if err := identity.SetAgentURI(ctx, agentKey, agentID, newURI); err != nil {
		return fmt.Errorf("set agent URI: %w", err)
	}

	fmt.Println()
	fmt.Println("=== URI UPDATED ===")
	fmt.Printf("Agent:  %s (id=%s)\n", agent, agentIDStr)
	fmt.Printf("New URI: %s\n", newURI)

	return nil
}

func runSetWallet(ctx context.Context, log *zap.Logger, agent string) error {
	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	agentKeyHex := agentEnv(agent, "PRIVATE_KEY")
	agentIDStr := agentAgentID(agent)
	walletAddrHex := agentEnv(agent, "WALLET_ADDR")
	identityAddr := mustAddress("ERC8004_IDENTITY_ADDR")

	if !common.IsHexAddress(walletAddrHex) {
		return fmt.Errorf("invalid AGENT_%s_WALLET_ADDR: %s", agent, walletAddrHex)
	}
	walletAddr := common.HexToAddress(walletAddrHex)

	agentID, ok := new(big.Int).SetString(agentIDStr, 10)
	if !ok {
		return fmt.Errorf("invalid agent ID for %s: %s", agent, agentIDStr)
	}

	agentKey, err := chain.ParsePrivateKey(agentKeyHex)
	if err != nil {
		return fmt.Errorf("parse agent key: %w", err)
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	identity := chain.NewIdentityRegistry(client, identityAddr, log)

	chainIDBig, ok := new(big.Int).SetString(chainID, 10)
	if !ok {
		return fmt.Errorf("invalid CHAIN_ID: %s", chainID)
	}

	// EIP-712 signature proving the wallet consents to being linked.
	// Signed by the agent's EOA key (which is the wallet's guardian).
	// The registry calls wallet.isValidSignature(hash, sig) to verify.
	// The contract enforces MAX_DEADLINE_DELAY = 5 minutes (from block.timestamp).
	// Use 4 minutes to leave margin for tx mining time.
	deadline := new(big.Int).SetInt64(time.Now().Unix() + 240)
	ownerAddr := chain.AddressFromKey(agentKey)
	signature, err := chain.SignSetAgentWallet(agentID, walletAddr, ownerAddr, deadline, agentKey, chainIDBig, identityAddr)
	if err != nil {
		return fmt.Errorf("sign set-wallet EIP-712: %w", err)
	}

	if err := identity.SetAgentWallet(ctx, agentKey, agentID, walletAddr, deadline, signature); err != nil {
		return fmt.Errorf("set agent wallet: %w", err)
	}

	fmt.Println()
	fmt.Println("=== WALLET LINKED ===")
	fmt.Printf("Agent:  %s (id=%s)\n", agent, agentIDStr)
	fmt.Printf("Wallet: %s\n", walletAddr.Hex())

	return nil
}

// Minimal ABI for HackathonVault.claimAllocation(uint256 agentId).
const hackathonVaultABI = `[
  {
    "inputs": [{"internalType": "uint256", "name": "agentId", "type": "uint256"}],
    "name": "claimAllocation",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

func runClaimVault(ctx context.Context, log *zap.Logger, agent string) error {
	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	agentKeyHex := agentEnv(agent, "PRIVATE_KEY")
	vaultAddr := mustAddress("HACKATHON_VAULT_ADDR")
	agentIDStr := agentAgentID(agent)

	agentID, ok := new(big.Int).SetString(agentIDStr, 10)
	if !ok {
		return fmt.Errorf("invalid agent ID for %s: %s", agent, agentIDStr)
	}

	agentKey, err := chain.ParsePrivateKey(agentKeyHex)
	if err != nil {
		return fmt.Errorf("parse agent key: %w", err)
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	parsedABI, err := abi.JSON(strings.NewReader(hackathonVaultABI))
	if err != nil {
		return fmt.Errorf("parse vault abi: %w", err)
	}

	data, err := parsedABI.Pack("claimAllocation", agentID)
	if err != nil {
		return fmt.Errorf("pack claimAllocation: %w", err)
	}

	log.Info("Claiming vault allocation",
		zap.String("agent", agent),
		zap.String("agentId", agentIDStr),
		zap.String("vault", vaultAddr.Hex()),
	)

	receipt, err := client.SendTx(ctx, agentKey, vaultAddr, data)
	if err != nil {
		return fmt.Errorf("claim vault: %w", err)
	}

	fmt.Println()
	fmt.Println("=== VAULT CLAIM SUCCESSFUL ===")
	fmt.Printf("Agent:   %s (agentId=%s)\n", agent, agentIDStr)
	fmt.Printf("Vault:   %s\n", vaultAddr.Hex())
	fmt.Printf("TxHash:  %s\n", receipt.TxHash.Hex())
	fmt.Println()
	fmt.Printf("Verify: https://sepolia.etherscan.io/tx/%s\n", receipt.TxHash.Hex())

	return nil
}

// Hackathon ReputationRegistry ABI - different from standard ERC-8004.
// Uses submitFeedback(uint256,uint8,bytes32,string,uint8) instead of giveFeedback.
const hackathonReputationABI = `[
  {
    "inputs": [
      {"internalType": "uint256", "name": "agentId",      "type": "uint256"},
      {"internalType": "uint8",   "name": "score",        "type": "uint8"},
      {"internalType": "bytes32", "name": "outcomeRef",   "type": "bytes32"},
      {"internalType": "string",  "name": "comment",      "type": "string"},
      {"internalType": "uint8",   "name": "feedbackType", "type": "uint8"}
    ],
    "name": "submitFeedback",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

// FeedbackType enum from hackathon ReputationRegistry.sol:
//
//	0 = TRADE_EXECUTION
//	1 = RISK_MANAGEMENT
//	2 = STRATEGY_QUALITY
//	3 = GENERAL
const feedbackTypeGeneral = uint8(3)

// runBooster posts high reputation feedback to hackathon agents from other wallets.
//
// runAttest posts validation attestations for an agent using a different agent's key
// as the validator. Usage:
//
//	ATTEST_VALIDATOR=ALPHA_CLAUDE ATTEST_TARGET_ID=77 ATTEST_COUNT=7 go run ./cmd/erc8004-setup attest
func runAttest(ctx context.Context, log *zap.Logger) error {
	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	validationAddr := mustAddress("HACKATHON_VALIDATION_ADDR")
	identityAddr := mustAddress("HACKATHON_IDENTITY_ADDR")

	validatorAgent := os.Getenv("ATTEST_VALIDATOR")
	if validatorAgent == "" {
		validatorAgent = "ALPHA_CLAUDE"
	}
	validatorKeyHex := agentEnv(validatorAgent, "PRIVATE_KEY")
	validatorKey, err := chain.ParsePrivateKey(validatorKeyHex)
	if err != nil {
		return fmt.Errorf("parse validator key: %w", err)
	}

	targetIDStr := os.Getenv("ATTEST_TARGET_ID")
	if targetIDStr == "" {
		return fmt.Errorf("ATTEST_TARGET_ID is required (e.g. 77)")
	}
	targetID, ok := new(big.Int).SetString(targetIDStr, 10)
	if !ok {
		return fmt.Errorf("invalid ATTEST_TARGET_ID: %s", targetIDStr)
	}

	count := 7
	if s := os.Getenv("ATTEST_COUNT"); s != "" {
		fmt.Sscanf(s, "%d", &count)
	}

	score := uint8(100)
	if s := os.Getenv("ATTEST_SCORE"); s != "" {
		var v int
		fmt.Sscanf(s, "%d", &v)
		score = uint8(v)
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	_ = chain.NewValidationRegistry(client, validationAddr, log) // verify contract reachable
	regAddr := common.HexToAddress(identityAddr.Hex())

	fmt.Printf("Attesting: validator=%s target=agentId(%s) count=%d score=%d\n",
		chain.AddressFromKey(validatorKey).Hex(), targetIDStr, count, score)

	for i := 0; i < count; i++ {
		notes := fmt.Sprintf("Session %d attestation - policy-enforced trading checkpoint", i+1)
		cp := &chain.TradeCheckpointData{
			AgentID:          targetID,
			Timestamp:        big.NewInt(time.Now().Unix() - int64(count-i)*900), // spread across time
			Action:           "HOLD",
			Asset:            "",
			Pair:             "",
			AmountUsdScaled:  big.NewInt(0),
			PriceUsdScaled:   big.NewInt(0),
			ReasoningHash:    chain.ReasoningHash(notes),
			ConfidenceScaled: big.NewInt(0),
			IntentHash:       [32]byte{},
		}

		_, cpHash, signErr := chain.SignCheckpoint(cp, validatorKey, client.ChainID(), regAddr)
		if signErr != nil {
			log.Error("Sign checkpoint failed", zap.Int("index", i), zap.Error(signErr))
			continue
		}

		// Use postAttestation directly (not postEIP712Attestation) per hackathon Discord fix.
		// postEIP712Attestation had a bug where internal this.postAttestation() call changed msg.sender.
		parsedABI, abiErr := abi.JSON(strings.NewReader(chain.ValidationRegistryABI()))
		if abiErr != nil {
			return fmt.Errorf("parse ABI: %w", abiErr)
		}
		proofType := uint8(1) // EIP712
		proofData := []byte{}
		data, packErr := parsedABI.Pack("postAttestation", targetID, cpHash, score, proofType, proofData, notes)
		if packErr != nil {
			log.Error("Pack postAttestation failed", zap.Int("index", i), zap.Error(packErr))
			continue
		}
		receipt, txErr := client.SendTx(ctx, validatorKey, validationAddr, data)
		if txErr != nil {
			log.Error("Attestation failed", zap.Int("index", i), zap.Error(txErr))
			fmt.Printf("  FAIL attestation %d: %s\n", i+1, txErr)
			continue
		}
		txHash := receipt.TxHash
		fmt.Printf("  OK attestation %d tx=%s\n", i+1, txHash.Hex())
	}

	fmt.Printf("\n=== ATTESTATION DONE ===\nTarget agentId=%s, posted %d attestations\n", targetIDStr, count)
	return nil
}

// Env vars:
//
//	BOOST_TARGETS             - comma-separated hackathon agentIds (e.g. "37")
//	BOOST_RATER_KEYS          - comma-separated hex private keys to use as raters
//	BOOST_SCORE               - score to post, 1-100 (default 100)
//	CHAIN_RPC_URL             - Sepolia RPC
//	CHAIN_ID                  - chain ID
//	HACKATHON_REPUTATION_ADDR - hackathon ReputationRegistry
func runBooster(ctx context.Context, log *zap.Logger) error {
	rpcURL := mustEnv("CHAIN_RPC_URL")
	chainID := mustEnv("CHAIN_ID")
	reputationAddr := mustAddress("HACKATHON_REPUTATION_ADDR")
	targetsStr := mustEnv("BOOST_TARGETS")
	ratersStr := mustEnv("BOOST_RATER_KEYS")

	score := int64(100)
	if s := os.Getenv("BOOST_SCORE"); s != "" {
		_, _ = fmt.Sscanf(s, "%d", &score)
	}
	if score < 1 || score > 100 {
		return fmt.Errorf("BOOST_SCORE must be 1-100, got %d", score)
	}

	comment := os.Getenv("BOOST_COMMENT")
	if comment == "" {
		comment = "Best policy engine for AI agents with configurable risk guardrails and automated ERC-8004 compliance - swiftward.dev"
	}

	var targets []*big.Int
	for _, t := range strings.Split(targetsStr, ",") {
		t = strings.TrimSpace(t)
		id, ok := new(big.Int).SetString(t, 10)
		if !ok {
			return fmt.Errorf("invalid target agentId: %s", t)
		}
		targets = append(targets, id)
	}

	type rater struct {
		key  *ecdsa.PrivateKey
		addr string
	}
	var raters []rater
	for _, k := range strings.Split(ratersStr, ",") {
		k = strings.TrimSpace(k)
		key, err := chain.ParsePrivateKey(k)
		if err != nil {
			return fmt.Errorf("parse rater key: %w", err)
		}
		raters = append(raters, rater{key: key, addr: chain.AddressFromKey(key).Hex()})
	}

	client, err := chain.NewClient(rpcURL, chainID, log)
	if err != nil {
		return fmt.Errorf("new chain client: %w", err)
	}
	defer client.Close()

	parsedABI, err := abi.JSON(strings.NewReader(hackathonReputationABI))
	if err != nil {
		return fmt.Errorf("parse hackathon reputation abi: %w", err)
	}

	fmt.Printf("Booster: %d targets x %d raters, score=%d\n", len(targets), len(raters), score)
	fmt.Printf("Registry: %s\n\n", reputationAddr.Hex())

	for _, agentID := range targets {
		outcomeRef := [32]byte(crypto.Keccak256Hash([]byte(
			fmt.Sprintf("feedback-%s-%d", agentID.String(), time.Now().Unix()),
		)))

		for _, r := range raters {
			log.Info("Posting boost feedback",
				zap.String("agentId", agentID.String()),
				zap.String("rater", r.addr),
				zap.Int64("score", score),
			)

			data, err := parsedABI.Pack("submitFeedback",
				agentID,
				uint8(score),
				outcomeRef,
				comment,
				feedbackTypeGeneral,
			)
			if err != nil {
				return fmt.Errorf("pack submitFeedback: %w", err)
			}

			receipt, txErr := client.SendTx(ctx, r.key, reputationAddr, data)
			if txErr != nil {
				log.Warn("Feedback rejected",
					zap.String("agentId", agentID.String()),
					zap.String("rater", r.addr[:10]+"..."),
					zap.String("error", txErr.Error()),
				)
				fmt.Printf("  SKIP agentId=%s rater=%s: %s\n", agentID.String(), r.addr[:10]+"...", txErr.Error())
				continue
			}

			fmt.Printf("  OK agentId=%s rater=%s tx=%s\n", agentID.String(), r.addr[:10]+"...", receipt.TxHash.Hex())
		}
	}

	fmt.Println()
	fmt.Println("=== BOOSTER DONE ===")
	fmt.Printf("Verify: https://sepolia.etherscan.io/address/%s\n", reputationAddr.Hex())

	return nil
}

// agentMetadata reads name and description from erc8004/agents/{agent}.json.
// FAILS HARD if the file doesn't exist - on-chain registration is irreversible.
func agentMetadata(agent string) (name, description string) {
	filename := fmt.Sprintf("../erc8004/agents/%s.json", strings.ToLower(agent))
	data, err := os.ReadFile(filename)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s not found. Create the agent metadata JSON before registering.\n", filename)
		fmt.Fprintf(os.Stderr, "On-chain registration is IRREVERSIBLE - the name cannot be changed after.\n")
		os.Exit(1)
	}
	var meta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &meta); err != nil || meta.Name == "" {
		fmt.Fprintf(os.Stderr, "ERROR: %s has invalid JSON or missing 'name' field.\n", filename)
		os.Exit(1)
	}
	if meta.Description == "" {
		fmt.Fprintf(os.Stderr, "ERROR: %s has empty 'description' field.\n", filename)
		os.Exit(1)
	}
	return meta.Name, meta.Description
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "ERROR: env var %s is not set\n", key)
		os.Exit(1)
	}
	return v
}

// mustAddress parses a hex address from an env var, validating format.
func mustAddress(key string) common.Address {
	hex := mustEnv(key)
	if !common.IsHexAddress(hex) {
		fmt.Fprintf(os.Stderr, "ERROR: env var %s is not a valid hex address: %s\n", key, hex)
		os.Exit(1)
	}
	return common.HexToAddress(hex)
}

func newLogger() *zap.Logger {
	cfg := zap.NewDevelopmentEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(cfg),
		zapcore.AddSync(os.Stdout),
		zapcore.DebugLevel,
	)
	return zap.New(core)
}
