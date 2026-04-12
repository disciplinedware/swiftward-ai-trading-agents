package chain

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

// eip712Digest computes keccak256("\x19\x01" || domainSeparator || messageHash).
// Uses explicit byte concatenation instead of fmt.Sprintf to avoid fragility
// with raw binary data in a cryptographic signing path.
func eip712Digest(domainSeparator, messageHash []byte) []byte {
	raw := make([]byte, 0, 2+len(domainSeparator)+len(messageHash))
	raw = append(raw, 0x19, 0x01)
	raw = append(raw, domainSeparator...)
	raw = append(raw, messageHash...)
	return crypto.Keccak256(raw)
}

// TradeIntentData holds the fields for a TradeIntent EIP-712 message.
// Matches the hackathon reference template (ai-trading-agent-template) and the
// shared RiskRouter contract exactly. The contract verifies EIP-712 signatures
// using this typehash - any field mismatch causes ecrecover to fail.
type TradeIntentData struct {
	AgentID         *big.Int       // ERC-721 agent token ID from Identity Registry
	AgentWallet     common.Address // agent's hot wallet (must match registry record)
	Pair            string         // e.g. "ETH-USD"
	Action          string         // "BUY" or "SELL" (uppercase, matching reference)
	AmountUsdScaled *big.Int       // USD * 100 (e.g. 50000 = $500.00)
	MaxSlippageBps  *big.Int       // slippage tolerance in basis points (50 = 0.5%)
	Nonce           *big.Int       // per-agent replay protection counter
	Deadline        *big.Int       // unix timestamp (seconds) after which intent is invalid
}

// tradeIntentTypedData builds the EIP-712 typed data structure for a TradeIntent.
// Domain: name="RiskRouter", version="1" - matches the shared hackathon contract.
func tradeIntentTypedData(intent *TradeIntentData, chainID *big.Int, routerAddr common.Address) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TradeIntent": {
				{Name: "agentId", Type: "uint256"},
				{Name: "agentWallet", Type: "address"},
				{Name: "pair", Type: "string"},
				{Name: "action", Type: "string"},
				{Name: "amountUsdScaled", Type: "uint256"},
				{Name: "maxSlippageBps", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "deadline", Type: "uint256"},
			},
		},
		PrimaryType: "TradeIntent",
		Domain: apitypes.TypedDataDomain{
			Name:              "RiskRouter",
			Version:           "1",
			ChainId:           (*math.HexOrDecimal256)(chainID),
			VerifyingContract: routerAddr.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"agentId":         intent.AgentID.String(),
			"agentWallet":     intent.AgentWallet.Hex(),
			"pair":            intent.Pair,
			"action":          intent.Action,
			"amountUsdScaled": intent.AmountUsdScaled.String(),
			"maxSlippageBps":  intent.MaxSlippageBps.String(),
			"nonce":           intent.Nonce.String(),
			"deadline":        intent.Deadline.String(),
		},
	}
}

// SignTradeIntent signs a TradeIntent using EIP-712 typed data.
// Returns a 65-byte signature (r[32] + s[32] + v[1]).
func SignTradeIntent(intent *TradeIntentData, key *ecdsa.PrivateKey, chainID *big.Int, routerAddr common.Address) ([]byte, error) {
	typedData := tradeIntentTypedData(intent, chainID, routerAddr)

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, fmt.Errorf("hash message: %w", err)
	}

	digest := eip712Digest(domainSeparator, messageHash)

	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// go-ethereum returns v=0/1, EIP-712 expects v=27/28
	sig[64] += 27

	return sig, nil
}

// TradeCheckpointData holds the fields for a TradeCheckpoint EIP-712 message.
// Used for postEIP712Attestation on the ValidationRegistry - the hackathon leaderboard scoring entry.
// Domain: name="AITradingAgent", verifyingContract=identity_registry_address.
type TradeCheckpointData struct {
	AgentID          *big.Int // ERC-721 agent token ID
	Timestamp        *big.Int // unix seconds
	Action           string   // "BUY" or "SELL"
	Asset            string   // base asset, e.g. "ETH"
	Pair             string   // full pair, e.g. "ETH-USD"
	AmountUsdScaled  *big.Int // USD * 100
	PriceUsdScaled   *big.Int // USD * 100
	ReasoningHash    [32]byte // keccak256 of reasoning text
	ConfidenceScaled *big.Int // confidence * 1000
	IntentHash       [32]byte // keccak256 of the approved TradeIntent (or zero for no on-chain intent)
}

func checkpointTypedData(cp *TradeCheckpointData, chainID *big.Int, registryAddr common.Address) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"TradeCheckpoint": {
				{Name: "agentId", Type: "uint256"},
				{Name: "timestamp", Type: "uint256"},
				{Name: "action", Type: "string"},
				{Name: "asset", Type: "string"},
				{Name: "pair", Type: "string"},
				{Name: "amountUsdScaled", Type: "uint256"},
				{Name: "priceUsdScaled", Type: "uint256"},
				{Name: "reasoningHash", Type: "bytes32"},
				{Name: "confidenceScaled", Type: "uint256"},
				{Name: "intentHash", Type: "bytes32"},
			},
		},
		PrimaryType: "TradeCheckpoint",
		Domain: apitypes.TypedDataDomain{
			Name:              "AITradingAgent",
			Version:           "1",
			ChainId:           (*math.HexOrDecimal256)(chainID),
			VerifyingContract: registryAddr.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"agentId":          cp.AgentID.String(),
			"timestamp":        cp.Timestamp.String(),
			"action":           cp.Action,
			"asset":            cp.Asset,
			"pair":             cp.Pair,
			"amountUsdScaled":  cp.AmountUsdScaled.String(),
			"priceUsdScaled":   cp.PriceUsdScaled.String(),
			"reasoningHash":    cp.ReasoningHash[:],
			"confidenceScaled": cp.ConfidenceScaled.String(),
			"intentHash":       cp.IntentHash[:],
		},
	}
}

// SignCheckpoint signs a TradeCheckpoint using EIP-712 typed data.
// Returns (65-byte signature, 32-byte checkpoint hash for postEIP712Attestation, error).
func SignCheckpoint(cp *TradeCheckpointData, key *ecdsa.PrivateKey, chainID *big.Int, registryAddr common.Address) ([]byte, [32]byte, error) {
	typedData := checkpointTypedData(cp, chainID, registryAddr)

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("hash message: %w", err)
	}

	digest := eip712Digest(domainSeparator, messageHash)

	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("sign: %w", err)
	}
	sig[64] += 27

	// checkpointHash is the full EIP-712 digest (matching ethers.TypedDataEncoder.hash).
	// This is keccak256("\x19\x01" || domainSeparator || structHash), NOT just structHash.
	var checkpointHash [32]byte
	copy(checkpointHash[:], digest)

	return sig, checkpointHash, nil
}

// ReasoningHash computes keccak256 of a reasoning text string.
func ReasoningHash(reasoning string) [32]byte {
	hash := crypto.Keccak256([]byte(reasoning))
	var result [32]byte
	copy(result[:], hash)
	return result
}

// ComputeIntentHash computes keccak256(abi.encode(agentId, agentWallet, pair, action, amountUsdScaled, nonce))
// matching the reference template's intent hash for checkpoint correlation.
//
// This is a FIXED on-chain format (matches ai-trading-agent-template/src/onchain/riskRouter.ts).
// Do NOT modify the field set or encoding - the hash must match what the RiskRouter contract emits
// in TradeApproved events. For our own evolvable hashing, use evidence.ComputeDecisionHash (canonical JSON).
func ComputeIntentHash(intent *TradeIntentData) [32]byte {
	// Use go-ethereum ABI encoding to match the reference template exactly:
	// keccak256(abi.encode(uint256, address, string, string, uint256, uint256))
	uint256Ty, _ := abi.NewType("uint256", "", nil)
	addressTy, _ := abi.NewType("address", "", nil)
	stringTy, _ := abi.NewType("string", "", nil)

	args := abi.Arguments{
		{Type: uint256Ty},
		{Type: addressTy},
		{Type: stringTy},
		{Type: stringTy},
		{Type: uint256Ty},
		{Type: uint256Ty},
	}
	encoded, err := args.Pack(intent.AgentID, intent.AgentWallet, intent.Pair, intent.Action, intent.AmountUsdScaled, intent.Nonce)
	if err != nil {
		// Encoding should never fail with valid inputs; return zero hash on error.
		return [32]byte{}
	}

	hash := crypto.Keccak256(encoded)
	var result [32]byte
	copy(result[:], hash)
	return result
}

// setAgentWalletTypedData builds the EIP-712 typed data for IdentityRegistry.setAgentWallet.
// Domain: name="ERC8004IdentityRegistry", version="1", chainId, verifyingContract=registryAddr.
// Message: AgentWalletSet(uint256 agentId, address newWallet, address owner, uint256 deadline).
// Must match the contract's AGENT_WALLET_SET_TYPEHASH exactly.
func setAgentWalletTypedData(agentID *big.Int, newWallet common.Address, owner common.Address, deadline *big.Int, chainID *big.Int, registryAddr common.Address) apitypes.TypedData {
	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"AgentWalletSet": {
				{Name: "agentId", Type: "uint256"},
				{Name: "newWallet", Type: "address"},
				{Name: "owner", Type: "address"},
				{Name: "deadline", Type: "uint256"},
			},
		},
		PrimaryType: "AgentWalletSet",
		Domain: apitypes.TypedDataDomain{
			Name:              "ERC8004IdentityRegistry",
			Version:           "1",
			ChainId:           (*math.HexOrDecimal256)(chainID),
			VerifyingContract: registryAddr.Hex(),
		},
		Message: apitypes.TypedDataMessage{
			"agentId":   agentID.String(),
			"newWallet": newWallet.Hex(),
			"owner":     owner.Hex(),
			"deadline":  deadline.String(),
		},
	}
}

// SignSetAgentWallet signs the EIP-712 message for IdentityRegistry.setAgentWallet.
// The signature proves the wallet consents to being linked to the agent.
// owner is the current owner of the agent NFT (msg.sender of the setAgentWallet call).
// Must be signed by the wallet's guardian (the agent's EOA key).
// Returns a 65-byte signature (r[32] + s[32] + v[1]).
func SignSetAgentWallet(agentID *big.Int, newWallet common.Address, owner common.Address, deadline *big.Int, key *ecdsa.PrivateKey, chainID *big.Int, registryAddr common.Address) ([]byte, error) {
	typedData := setAgentWalletTypedData(agentID, newWallet, owner, deadline, chainID, registryAddr)

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return nil, fmt.Errorf("hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return nil, fmt.Errorf("hash message: %w", err)
	}

	digest := eip712Digest(domainSeparator, messageHash)

	sig, err := crypto.Sign(digest, key)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// go-ethereum returns v=0/1, EIP-712 expects v=27/28
	sig[64] += 27

	return sig, nil
}

// RecoverSigner recovers the signer address from an EIP-712 TradeIntent signature.
func RecoverSigner(intent *TradeIntentData, sig []byte, chainID *big.Int, routerAddr common.Address) (common.Address, error) {
	if len(sig) != 65 {
		return common.Address{}, fmt.Errorf("invalid signature length: %d", len(sig))
	}

	typedData := tradeIntentTypedData(intent, chainID, routerAddr)

	domainSeparator, err := typedData.HashStruct("EIP712Domain", typedData.Domain.Map())
	if err != nil {
		return common.Address{}, fmt.Errorf("hash domain: %w", err)
	}

	messageHash, err := typedData.HashStruct(typedData.PrimaryType, typedData.Message)
	if err != nil {
		return common.Address{}, fmt.Errorf("hash message: %w", err)
	}

	digest := eip712Digest(domainSeparator, messageHash)

	// Convert v from 27/28 back to 0/1 for ecrecover.
	// Only handles v=27/28 (not 0/1) because all signatures entering this function
	// come from our own SignTradeIntent which always adds 27 after crypto.Sign.
	// External signatures with v=0/1 would underflow - not a concern since we only
	// verify our own output.
	recoverSig := make([]byte, 65)
	copy(recoverSig, sig)
	recoverSig[64] -= 27

	pubKey, err := crypto.SigToPub(digest, recoverSig)
	if err != nil {
		return common.Address{}, fmt.Errorf("recover pubkey: %w", err)
	}

	return crypto.PubkeyToAddress(*pubKey), nil
}
