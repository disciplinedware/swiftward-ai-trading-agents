package chain

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

const reputationRegistryABI = `[
  {
    "inputs": [
      {"internalType": "uint256", "name": "agentId",       "type": "uint256"},
      {"internalType": "int128",  "name": "value",         "type": "int128"},
      {"internalType": "uint8",   "name": "valueDecimals", "type": "uint8"},
      {"internalType": "string",  "name": "tag1",          "type": "string"},
      {"internalType": "string",  "name": "tag2",          "type": "string"},
      {"internalType": "string",  "name": "endpoint",      "type": "string"},
      {"internalType": "string",  "name": "feedbackURI",   "type": "string"},
      {"internalType": "bytes32", "name": "feedbackHash",  "type": "bytes32"}
    ],
    "name": "giveFeedback",
    "outputs": [],
    "stateMutability": "nonpayable",
    "type": "function"
  },
  {
    "inputs": [
      {"internalType": "uint256",   "name": "agentId",          "type": "uint256"},
      {"internalType": "address[]", "name": "clientAddresses",  "type": "address[]"},
      {"internalType": "string",    "name": "tag1",             "type": "string"},
      {"internalType": "string",    "name": "tag2",             "type": "string"}
    ],
    "name": "getSummary",
    "outputs": [
      {"internalType": "uint64",  "name": "count",                "type": "uint64"},
      {"internalType": "int128",  "name": "summaryValue",         "type": "int128"},
      {"internalType": "uint8",   "name": "summaryValueDecimals", "type": "uint8"}
    ],
    "stateMutability": "view",
    "type": "function"
  }
]`

// ReputationRegistry handles interactions with the ERC-8004 Reputation Registry contract.
// Validators post numeric feedback (performance metrics, compliance scores) about agents.
// Self-feedback is blocked by the contract - the caller must NOT be the agent's owner.
type ReputationRegistry struct {
	client       *Client
	registryAddr common.Address
	log          *zap.Logger
}

// NewReputationRegistry creates a new reputation registry client.
func NewReputationRegistry(client *Client, registryAddr common.Address, log *zap.Logger) *ReputationRegistry {
	return &ReputationRegistry{
		client:       client,
		registryAddr: registryAddr,
		log:          log,
	}
}

// FeedbackParams holds the parameters for a giveFeedback call.
type FeedbackParams struct {
	AgentID       *big.Int // ERC-721 agentId from Identity Registry
	Value         *big.Int // int128: the metric value (scaled by ValueDecimals)
	ValueDecimals uint8    // number of decimal places (e.g., 2 means value=182 represents 1.82)
	Tag1          string   // category (e.g., "perf", "risk", "trust")
	Tag2          string   // metric name (e.g., "sharpe", "max_drawdown_pct", "compliance_pct")
	Endpoint      string   // service endpoint URL
	FeedbackURI   string   // URI to detailed feedback JSON
	FeedbackHash  [32]byte // keccak256 of the feedback JSON
}

// GiveFeedback posts a numeric reputation feedback entry for an agent.
// Must be called from the validator wallet (not the agent's own wallet).
func (r *ReputationRegistry) GiveFeedback(ctx context.Context, validatorKey *ecdsa.PrivateKey, params *FeedbackParams) (common.Hash, error) {
	validatorAddr := AddressFromKey(validatorKey)
	r.log.Info("Posting reputation feedback",
		zap.String("validator", validatorAddr.Hex()),
		zap.String("agentId", params.AgentID.String()),
		zap.String("tag1", params.Tag1),
		zap.String("tag2", params.Tag2),
		zap.String("value", params.Value.String()),
		zap.Uint8("decimals", params.ValueDecimals),
	)

	parsedABI, err := abi.JSON(strings.NewReader(reputationRegistryABI))
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse reputation abi: %w", err)
	}

	data, err := parsedABI.Pack("giveFeedback",
		params.AgentID,
		params.Value,
		params.ValueDecimals,
		params.Tag1,
		params.Tag2,
		params.Endpoint,
		params.FeedbackURI,
		params.FeedbackHash,
	)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack giveFeedback: %w", err)
	}

	receipt, err := r.client.SendTx(ctx, validatorKey, r.registryAddr, data)
	if err != nil {
		return common.Hash{}, fmt.Errorf("send giveFeedback tx: %w", err)
	}

	r.log.Info("Reputation feedback posted",
		zap.String("txHash", receipt.TxHash.Hex()),
		zap.Uint64("block", receipt.BlockNumber.Uint64()),
		zap.String("tag", params.Tag1+"/"+params.Tag2),
	)

	return receipt.TxHash, nil
}

// ReputationRegistryAddr returns the Reputation Registry contract address.
func (r *ReputationRegistry) ReputationRegistryAddr() common.Address {
	return r.registryAddr
}
