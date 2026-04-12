package chain

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap"

	"ai-trading-agents/internal/observability"
)

// Minimal ABIs — only the functions and events we call.
// Using minimal ABIs avoids embedding large JSON files and keeps the build simple.
const identityRegistryABI = `[
  {
    "inputs": [
      {"internalType": "address",  "name": "agentWallet",   "type": "address"},
      {"internalType": "string",   "name": "name",          "type": "string"},
      {"internalType": "string",   "name": "description",   "type": "string"},
      {"internalType": "string[]", "name": "capabilities",  "type": "string[]"},
      {"internalType": "string",   "name": "agentURI",      "type": "string"}
    ],
    "name": "register",
    "outputs": [{"internalType": "uint256", "name": "agentId", "type": "uint256"}],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [{"internalType": "address", "name": "agentWallet", "type": "address"}],
    "name": "walletToAgentId",
    "outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [{"internalType": "uint256", "name": "agentId", "type": "uint256"}],
    "name": "isRegistered",
    "outputs": [{"internalType": "bool", "name": "", "type": "bool"}],
    "stateMutability": "view",
    "type": "function"
  },
  {
    "inputs": [{"internalType": "uint256", "name": "tokenId", "type": "uint256"}],
    "name": "ownerOf",
    "outputs": [{"internalType": "address", "name": "", "type": "address"}],
    "stateMutability": "view",
    "type": "function"
  }
]`

const validationRegistryABI = `[
  {
    "inputs": [
      {"internalType": "address",  "name": "validatorAddress", "type": "address"},
      {"internalType": "uint256",  "name": "agentId",          "type": "uint256"},
      {"internalType": "string",   "name": "requestURI",       "type": "string"},
      {"internalType": "bytes32",  "name": "requestHash",      "type": "bytes32"}
    ],
    "name": "validationRequest",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"internalType": "bytes32", "name": "requestHash",  "type": "bytes32"},
      {"internalType": "uint8",   "name": "response",     "type": "uint8"},
      {"internalType": "string",  "name": "responseURI",  "type": "string"},
      {"internalType": "bytes32", "name": "responseHash", "type": "bytes32"},
      {"internalType": "string",  "name": "tag",          "type": "string"}
    ],
    "name": "validationResponse",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"internalType": "uint256", "name": "agentId",        "type": "uint256"},
      {"internalType": "bytes32", "name": "checkpointHash", "type": "bytes32"},
      {"internalType": "uint8",   "name": "score",          "type": "uint8"},
      {"internalType": "string",  "name": "notes",          "type": "string"}
    ],
    "name": "postEIP712Attestation",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"internalType": "uint256", "name": "agentId",        "type": "uint256"},
      {"internalType": "bytes32", "name": "checkpointHash", "type": "bytes32"},
      {"internalType": "uint8",   "name": "score",          "type": "uint8"},
      {"internalType": "uint8",   "name": "proofType",      "type": "uint8"},
      {"internalType": "bytes",   "name": "proofData",      "type": "bytes"},
      {"internalType": "string",  "name": "notes",          "type": "string"}
    ],
    "name": "postAttestation",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  }
]`

// Standard ERC-8004 registry ABI - different from hackathon's (1-param register, different event).
const standardRegistryABI = `[
  {
    "inputs": [{"internalType": "string", "name": "agentURI", "type": "string"}],
    "name": "register",
    "outputs": [{"internalType": "uint256", "name": "agentId", "type": "uint256"}],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [{"internalType": "address", "name": "owner", "type": "address"}],
    "name": "balanceOf",
    "outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
    "stateMutability": "view",
    "type": "function"
  }
]`

// Event signatures for the two registries.
var (
	// Hackathon: AgentRegistered(uint256 indexed agentId, address indexed operatorWallet, address indexed agentWallet, string name)
	agentRegisteredEventSig = crypto.Keccak256Hash([]byte("AgentRegistered(uint256,address,address,string)"))
	// Standard: Registered(uint256 indexed agentId, string agentURI, address indexed owner)
	standardRegisteredEventSig = crypto.Keccak256Hash([]byte("Registered(uint256,string,address)"))
)

// IdentityRegistry handles interactions with the ERC-8004 Identity Registry contract.
// Agents are registered here to get an ERC-721 tokenID that represents their on-chain identity.
type IdentityRegistry struct {
	client       *Client
	registryAddr common.Address
	log          *zap.Logger
}

// NewIdentityRegistry creates a new identity registry client.
func NewIdentityRegistry(client *Client, registryAddr common.Address, log *zap.Logger) *IdentityRegistry {
	return &IdentityRegistry{
		client:       client,
		registryAddr: registryAddr,
		log:          log,
	}
}

// RegisterHackathon registers an agent on the hackathon's AgentRegistry.
// ABI: register(agentWallet, name, description, capabilities, agentURI) - 5 params.
func (r *IdentityRegistry) RegisterHackathon(ctx context.Context, agentKey *ecdsa.PrivateKey, name, description string, capabilities []string, agentURI string) (*big.Int, error) {
	agentAddr := AddressFromKey(agentKey)
	r.log.Info("Registering agent on AgentRegistry",
		zap.String("agent", agentAddr.Hex()),
		zap.String("name", name),
		zap.String("uri", agentURI),
		zap.String("registry", r.registryAddr.Hex()),
	)

	parsedABI, err := abi.JSON(strings.NewReader(identityRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("parse identity abi: %w", err)
	}

	data, err := parsedABI.Pack("register", agentAddr, name, description, capabilities, agentURI)
	if err != nil {
		return nil, fmt.Errorf("pack register: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, agentKey, r.registryAddr, data)
	if err != nil {
		return nil, fmt.Errorf("send register tx: %w", err)
	}

	// Extract agentId from AgentRegistered(uint256 indexed agentId, address indexed operatorWallet, address indexed agentWallet, string name).
	// Topics layout: [eventSig, agentId, operatorWallet, agentWallet]
	for _, log := range receipt.Logs {
		if len(log.Topics) >= 2 && log.Topics[0] == agentRegisteredEventSig {
			agentID := new(big.Int).SetBytes(log.Topics[1].Bytes())
			r.log.Info("Agent registered successfully",
				zap.String("agentId", agentID.String()),
				zap.String("txHash", receipt.TxHash.Hex()),
			)
			return agentID, nil
		}
	}

	return nil, fmt.Errorf("registered event not found in receipt (txHash=%s)", receipt.TxHash.Hex())
}

// RegisterStandard registers an agent on the standard ERC-8004 registry.
// ABI: register(agentURI) - 1 param.
func (r *IdentityRegistry) RegisterStandard(ctx context.Context, agentKey *ecdsa.PrivateKey, agentURI string) (*big.Int, error) {
	agentAddr := AddressFromKey(agentKey)
	r.log.Info("Registering agent on standard ERC-8004 registry",
		zap.String("agent", agentAddr.Hex()),
		zap.String("uri", agentURI),
		zap.String("registry", r.registryAddr.Hex()),
	)

	parsedABI, err := abi.JSON(strings.NewReader(standardRegistryABI))
	if err != nil {
		return nil, fmt.Errorf("parse standard abi: %w", err)
	}

	data, err := parsedABI.Pack("register", agentURI)
	if err != nil {
		return nil, fmt.Errorf("pack register: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, agentKey, r.registryAddr, data)
	if err != nil {
		return nil, fmt.Errorf("send register tx: %w", err)
	}

	// Extract agentId from Registered(uint256 indexed agentId, string agentURI, address indexed owner).
	for _, log := range receipt.Logs {
		if len(log.Topics) >= 2 && log.Topics[0] == standardRegisteredEventSig {
			agentID := new(big.Int).SetBytes(log.Topics[1].Bytes())
			r.log.Info("Agent registered on standard registry",
				zap.String("agentId", agentID.String()),
				zap.String("txHash", receipt.TxHash.Hex()),
			)
			return agentID, nil
		}
	}

	return nil, fmt.Errorf("registered event not found in receipt (txHash=%s)", receipt.TxHash.Hex())
}

// IsAgentRegistered checks if a wallet already has a registered agent (walletToAgentId > 0).
// Returns true if registered.
func (r *IdentityRegistry) IsAgentRegistered(ctx context.Context, walletAddr common.Address) (bool, error) {
	parsedABI, err := abi.JSON(strings.NewReader(identityRegistryABI))
	if err != nil {
		return false, fmt.Errorf("parse identity abi: %w", err)
	}

	data, err := parsedABI.Pack("walletToAgentId", walletAddr)
	if err != nil {
		return false, fmt.Errorf("pack walletToAgentId: %w", err)
	}

	result, err := r.client.CallContract(ctx, r.registryAddr, data)
	if err != nil {
		return false, fmt.Errorf("call walletToAgentId: %w", err)
	}

	agentID := new(big.Int).SetBytes(result)
	return agentID.Sign() > 0, nil
}

// VerifyAgentOwnership checks on-chain that the given agentId exists and is owned by the expected wallet.
// Free read-only call (eth_call), no gas.
func (r *IdentityRegistry) VerifyAgentOwnership(ctx context.Context, agentID *big.Int, expectedOwner common.Address) error {
	parsedABI, err := abi.JSON(strings.NewReader(identityRegistryABI))
	if err != nil {
		return fmt.Errorf("parse identity abi: %w", err)
	}

	data, err := parsedABI.Pack("ownerOf", agentID)
	if err != nil {
		return fmt.Errorf("pack ownerOf: %w", err)
	}

	result, err := r.client.CallContract(ctx, r.registryAddr, data)
	if err != nil {
		return fmt.Errorf("call ownerOf for agentId %s: %w", agentID.String(), err)
	}

	if len(result) < 32 {
		return fmt.Errorf("ownerOf returned unexpected data length %d for agentId %s", len(result), agentID.String())
	}

	owner := common.BytesToAddress(result)
	if owner != expectedOwner {
		return fmt.Errorf("agentId %s is owned by %s, not %s", agentID.String(), owner.Hex(), expectedOwner.Hex())
	}

	return nil
}

// SetAgentURI updates the registration URI for an already-registered agent.
// Used after updating the IPFS JSON (e.g., adding the registrations field post-registration).
func (r *IdentityRegistry) SetAgentURI(ctx context.Context, ownerKey *ecdsa.PrivateKey, agentID *big.Int, newURI string) error {
	r.log.Info("Updating agent URI",
		zap.String("agentId", agentID.String()),
		zap.String("newURI", newURI),
	)

	parsedABI, err := abi.JSON(strings.NewReader(identityRegistryABI))
	if err != nil {
		return fmt.Errorf("parse identity abi: %w", err)
	}

	data, err := parsedABI.Pack("setAgentURI", agentID, newURI)
	if err != nil {
		return fmt.Errorf("pack setAgentURI: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, ownerKey, r.registryAddr, data)
	if err != nil {
		return fmt.Errorf("send setAgentURI tx: %w", err)
	}

	r.log.Info("Agent URI updated",
		zap.String("txHash", receipt.TxHash.Hex()),
	)
	return nil
}

// SetAgentWallet links an EIP-1271 smart wallet contract to the agent's on-chain identity.
// The wallet contract (e.g., SwiftwardAgentWallet) validates signatures on behalf of the agent.
// Requires an EIP-712 authorization signature from the wallet contract itself.
func (r *IdentityRegistry) SetAgentWallet(ctx context.Context, ownerKey *ecdsa.PrivateKey, agentID *big.Int, walletAddr common.Address, deadline *big.Int, signature []byte) error {
	r.log.Info("Setting agent wallet",
		zap.String("agentId", agentID.String()),
		zap.String("wallet", walletAddr.Hex()),
	)

	parsedABI, err := abi.JSON(strings.NewReader(identityRegistryABI))
	if err != nil {
		return fmt.Errorf("parse identity abi: %w", err)
	}

	data, err := parsedABI.Pack("setAgentWallet", agentID, walletAddr, deadline, signature)
	if err != nil {
		return fmt.Errorf("pack setAgentWallet: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, ownerKey, r.registryAddr, data)
	if err != nil {
		return fmt.Errorf("send setAgentWallet tx: %w", err)
	}

	r.log.Info("Agent wallet set",
		zap.String("txHash", receipt.TxHash.Hex()),
	)
	return nil
}

// RegistryAddr returns the Identity Registry contract address.
func (r *IdentityRegistry) RegistryAddr() common.Address {
	return r.registryAddr
}

// ValidationRegistry handles interactions with the ERC-8004 Validation Registry contract.
// After a successful trade, a validationRequest is posted here for independent validators.
type ValidationRegistry struct {
	client       *Client
	registryAddr common.Address
	log          *zap.Logger
}

// NewValidationRegistry creates a new validation registry client.
func NewValidationRegistry(client *Client, registryAddr common.Address, log *zap.Logger) *ValidationRegistry {
	return &ValidationRegistry{
		client:       client,
		registryAddr: registryAddr,
		log:          log,
	}
}

// PostValidationRequest submits a validation request to the ERC-8004 Validation Registry.
// Called by the trading agent after a trade to request independent validation.
//
// Parameters:
//   - agentKey:       agent's private key (signs the transaction)
//   - validatorAddr:  address of the validator expected to respond
//   - agentTokenID:   the ERC-721 agentId from the Identity Registry
//   - traceURL:       public URL to the evidence bundle (GEB JSON)
//   - decisionHash:   keccak256 of the GEB JSON — the on-chain commitment
func (r *ValidationRegistry) PostValidationRequest(
	ctx context.Context,
	agentKey *ecdsa.PrivateKey,
	validatorAddr common.Address,
	agentTokenID *big.Int,
	traceURL string,
	decisionHash [32]byte,
) (common.Hash, error) {
	agentAddr := AddressFromKey(agentKey)
	r.log.Info("Posting ERC-8004 validation request",
		zap.String("agent", agentAddr.Hex()),
		zap.String("agentId", agentTokenID.String()),
		zap.String("validator", validatorAddr.Hex()),
		zap.String("traceURL", traceURL),
	)

	parsedABI, err := abi.JSON(strings.NewReader(validationRegistryABI))
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse validation abi: %w", err)
	}

	data, err := parsedABI.Pack("validationRequest", validatorAddr, agentTokenID, traceURL, decisionHash)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack validationRequest: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, agentKey, r.registryAddr, data)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send validationRequest tx: %w", err)
	}

	r.log.Info("Validation request posted",
		zap.String("txHash", receipt.TxHash.Hex()),
		zap.Uint64("block", receipt.BlockNumber.Uint64()),
	)

	return receipt.TxHash, nil
}

// PostValidationResponse posts a validation response. Must be called from the validator wallet.
//
// Parameters:
//   - validatorKey:  validator's private key (must match validatorAddress in the request)
//   - requestHash:   keccak256 of the original GEB JSON (links response to request)
//   - response:      score 0-255 (e.g. 95 = high confidence valid)
//   - responseURI:   public URL to the validator's response JSON
//   - responseHash:  keccak256 of the response JSON
//   - tag:           optional label (e.g. "swiftward-v1")
func (r *ValidationRegistry) PostValidationResponse(
	ctx context.Context,
	validatorKey *ecdsa.PrivateKey,
	requestHash [32]byte,
	response uint8,
	responseURI string,
	responseHash [32]byte,
	tag string,
) (common.Hash, error) {
	validatorAddr := AddressFromKey(validatorKey)
	r.log.Info("Posting ERC-8004 validation response",
		zap.String("validator", validatorAddr.Hex()),
		zap.Uint8("response", response),
		zap.String("responseURI", responseURI),
		zap.String("tag", tag),
	)

	parsedABI, err := abi.JSON(strings.NewReader(validationRegistryABI))
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse validation abi: %w", err)
	}

	data, err := parsedABI.Pack("validationResponse", requestHash, response, responseURI, responseHash, tag)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack validationResponse: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, validatorKey, r.registryAddr, data)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send validationResponse tx: %w", err)
	}

	r.log.Info("Validation response posted",
		zap.String("txHash", receipt.TxHash.Hex()),
		zap.Uint64("block", receipt.BlockNumber.Uint64()),
	)

	return receipt.TxHash, nil
}

// PostEIP712Attestation posts a self-attestation for a trade checkpoint.
// This is the hackathon leaderboard scoring entry (getAverageValidationScore).
// Called as fire-and-forget after each fill.
func (r *ValidationRegistry) PostEIP712Attestation(
	ctx context.Context,
	agentKey *ecdsa.PrivateKey,
	agentTokenID *big.Int,
	checkpointHash [32]byte,
	score uint8,
	notes string,
) (common.Hash, error) {
	log := observability.LoggerFromCtx(ctx, r.log)
	agentAddr := AddressFromKey(agentKey)
	log.Info("Posting EIP-712 attestation",
		zap.String("agent", agentAddr.Hex()),
		zap.String("agentId", agentTokenID.String()),
		zap.Uint8("score", score),
	)

	parsedABI, err := abi.JSON(strings.NewReader(validationRegistryABI))
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse validation abi: %w", err)
	}

	data, err := parsedABI.Pack("postEIP712Attestation", agentTokenID, checkpointHash, score, notes)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack postEIP712Attestation: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, agentKey, r.registryAddr, data)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send postEIP712Attestation tx: %w", err)
	}

	log.Info("EIP-712 attestation posted",
		zap.String("txHash", receipt.TxHash.Hex()),
		zap.Uint64("block", receipt.BlockNumber.Uint64()),
	)

	return receipt.TxHash, nil
}

// ValidationRegistryABI returns the raw ABI JSON for external callers.
func ValidationRegistryABI() string { return validationRegistryABI }

// ValidationRegistryAddr returns the Validation Registry contract address.
func (r *ValidationRegistry) ValidationRegistryAddr() common.Address {
	return r.registryAddr
}

// AgentOnChainState holds on-chain state for a registered agent.
type AgentOnChainState struct {
	Key     *ecdsa.PrivateKey
	Address common.Address
	TokenID *big.Int // ERC-721 agentId from Identity Registry
	Name    string
	URI     string
	Nonce   uint64 // incremented per trade intent
}

// NextNonce returns the current nonce and increments it.
func (a *AgentOnChainState) NextNonce() *big.Int {
	n := new(big.Int).SetUint64(a.Nonce)
	a.Nonce++
	return n
}

// EnsureRegistered returns the agent's on-chain state.
// If knownTokenID is provided (> 0), skips registration and uses that ID.
// Otherwise checks the registry and registers if needed.
func EnsureRegistered(
	ctx context.Context,
	identity *IdentityRegistry,
	agentKey *ecdsa.PrivateKey,
	name, uri string,
	knownTokenID *big.Int,
) (*AgentOnChainState, error) {
	addr := AddressFromKey(agentKey)

	if knownTokenID != nil && knownTokenID.Sign() > 0 {
		return &AgentOnChainState{
			Key:     agentKey,
			Address: addr,
			TokenID: knownTokenID,
			Name:    name,
			URI:     uri,
		}, nil
	}

	registered, err := identity.IsAgentRegistered(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("check registration: %w", err)
	}
	if registered {
		return nil, fmt.Errorf("agent %s is already registered — store the agentId in config and pass it as knownTokenID", addr.Hex())
	}

	tokenID, err := identity.RegisterHackathon(ctx, agentKey, name, "", []string{"trading"}, uri)
	if err != nil {
		return nil, fmt.Errorf("register agent: %w", err)
	}

	return &AgentOnChainState{
		Key:     agentKey,
		Address: addr,
		TokenID: tokenID,
		Name:    name,
		URI:     uri,
	}, nil
}
