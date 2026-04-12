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

// Minimal ABI for the hackathon RiskRouter contract.
// Source: contracts/RiskRouter.sol in ai-trading-agent-template.
const riskRouterABI = `[
  {
    "name": "submitTradeIntent",
    "type": "function",
    "stateMutability": "nonpayable",
    "inputs": [
      {
        "name": "intent",
        "type": "tuple",
        "components": [
          { "name": "agentId", "type": "uint256" },
          { "name": "agentWallet", "type": "address" },
          { "name": "pair", "type": "string" },
          { "name": "action", "type": "string" },
          { "name": "amountUsdScaled", "type": "uint256" },
          { "name": "maxSlippageBps", "type": "uint256" },
          { "name": "nonce", "type": "uint256" },
          { "name": "deadline", "type": "uint256" }
        ]
      },
      { "name": "signature", "type": "bytes" }
    ],
    "outputs": [
      { "name": "approved", "type": "bool" },
      { "name": "reason", "type": "string" }
    ]
  },
  {
    "name": "getIntentNonce",
    "type": "function",
    "stateMutability": "view",
    "inputs": [
      { "name": "agentId", "type": "uint256" }
    ],
    "outputs": [
      { "name": "", "type": "uint256" }
    ]
  },
  {
    "anonymous": false,
    "name": "TradeApproved",
    "type": "event",
    "inputs": [
      { "name": "agentId", "type": "uint256", "indexed": true },
      { "name": "intentHash", "type": "bytes32", "indexed": true },
      { "name": "amountUsdScaled", "type": "uint256", "indexed": false }
    ]
  },
  {
    "anonymous": false,
    "name": "TradeRejected",
    "type": "event",
    "inputs": [
      { "name": "agentId", "type": "uint256", "indexed": true },
      { "name": "intentHash", "type": "bytes32", "indexed": true },
      { "name": "reason", "type": "string", "indexed": false }
    ]
  }
]`

// Event topic signatures for receipt log parsing.
var (
	tradeApprovedEventSig = crypto.Keccak256Hash([]byte("TradeApproved(uint256,bytes32,uint256)"))
	tradeRejectedEventSig = crypto.Keccak256Hash([]byte("TradeRejected(uint256,bytes32,string)"))
)

// RiskRouter handles interactions with the hackathon-provided Risk Router contract.
//
// The Risk Router receives EIP-712 signed TradeIntents, verifies signatures against
// AgentRegistry, enforces on-chain limits (max position size, trades per hour), and
// records approved trades on-chain. The leaderboard reads trade counts from here.
type RiskRouter struct {
	client     *Client
	routerAddr common.Address
	log        *zap.Logger
}

// NewRiskRouter creates a new Risk Router client.
func NewRiskRouter(client *Client, routerAddr common.Address, log *zap.Logger) *RiskRouter {
	return &RiskRouter{
		client:     client,
		routerAddr: routerAddr,
		log:        log,
	}
}

// TradeSubmission is the result of submitting a signed trade to the Risk Router.
type TradeSubmission struct {
	TxHash      common.Hash
	Success     bool   // true if RiskRouter approved the trade
	ErrorReason string // rejection reason (empty if approved)
}

// intentTuple matches the RiskRouter.TradeIntent struct for ABI encoding.
// Field names are matched case-insensitively by go-ethereum's ABI packer.
type intentTuple struct {
	AgentId         *big.Int
	AgentWallet     common.Address
	Pair            string
	Action          string
	AmountUsdScaled *big.Int
	MaxSlippageBps  *big.Int
	Nonce           *big.Int
	Deadline        *big.Int
}

// GetIntentNonce reads the current on-chain nonce for an agent from the RiskRouter contract.
// This is a view call (no gas, no signing).
func (r *RiskRouter) GetIntentNonce(ctx context.Context, agentID *big.Int) (*big.Int, error) {
	parsedABI, err := abi.JSON(strings.NewReader(riskRouterABI))
	if err != nil {
		return nil, fmt.Errorf("parse risk router ABI: %w", err)
	}

	data, err := parsedABI.Pack("getIntentNonce", agentID)
	if err != nil {
		return nil, fmt.Errorf("pack getIntentNonce: %w", err)
	}

	result, err := r.client.CallContract(ctx, r.routerAddr, data)
	if err != nil {
		return nil, fmt.Errorf("call getIntentNonce: %w", err)
	}

	values, err := parsedABI.Unpack("getIntentNonce", result)
	if err != nil {
		return nil, fmt.Errorf("unpack getIntentNonce: %w", err)
	}

	if len(values) == 0 {
		return nil, fmt.Errorf("getIntentNonce returned no values")
	}

	nonce, ok := values[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("getIntentNonce: unexpected type %T", values[0])
	}

	return nonce, nil
}

// SubmitTrade signs a TradeIntent and submits it to the RiskRouter contract on-chain.
//
// Flow:
//  1. Sign TradeIntent with EIP-712 (agent's private key)
//  2. ABI-encode submitTradeIntent(intent, signature)
//  3. Send transaction via Client.SendTx
//  4. Parse TradeApproved/TradeRejected events from receipt
//  5. Return result
func (r *RiskRouter) SubmitTrade(ctx context.Context, intent *TradeIntentData, agentKey *ecdsa.PrivateKey) (*TradeSubmission, error) {
	log := observability.LoggerFromCtx(ctx, r.log)

	// 1. Sign the trade intent with EIP-712.
	sig, err := SignTradeIntent(intent, agentKey, r.client.chainID, r.routerAddr)
	if err != nil {
		return nil, fmt.Errorf("sign trade intent: %w", err)
	}

	log.Info("Trade intent signed",
		zap.String("agent_id", intent.AgentID.String()),
		zap.String("wallet", intent.AgentWallet.Hex()),
		zap.String("pair", intent.Pair),
		zap.String("action", intent.Action),
		zap.String("amount_usd_scaled", intent.AmountUsdScaled.String()),
		zap.String("nonce", intent.Nonce.String()),
	)

	// 2. ABI-encode the contract call.
	parsedABI, err := abi.JSON(strings.NewReader(riskRouterABI))
	if err != nil {
		return nil, fmt.Errorf("parse risk router ABI: %w", err)
	}

	data, err := parsedABI.Pack("submitTradeIntent",
		intentTuple{
			AgentId:         intent.AgentID,
			AgentWallet:     intent.AgentWallet,
			Pair:            intent.Pair,
			Action:          intent.Action,
			AmountUsdScaled: intent.AmountUsdScaled,
			MaxSlippageBps:  intent.MaxSlippageBps,
			Nonce:           intent.Nonce,
			Deadline:        intent.Deadline,
		},
		sig,
	)
	if err != nil {
		return nil, fmt.Errorf("pack submitTradeIntent: %w", err)
	}

	// 3. Send the transaction.
	log.Info("Submitting trade intent to RiskRouter",
		zap.String("router", r.routerAddr.Hex()),
		zap.String("agent_id", intent.AgentID.String()),
	)

	receipt, err := r.client.SendTx(ctx, agentKey, r.routerAddr, data)
	if err != nil && receipt == nil {
		// Network/signing error - no receipt at all.
		return nil, fmt.Errorf("send submitTradeIntent tx: %w", err)
	}
	// receipt != nil: tx was mined (possibly reverted). Parse events.

	// 4. Parse events from receipt.
	for _, entry := range receipt.Logs {
		if len(entry.Topics) < 1 {
			continue
		}

		switch entry.Topics[0] {
		case tradeApprovedEventSig:
			log.Info("Trade approved by RiskRouter",
				zap.String("tx_hash", receipt.TxHash.Hex()),
				zap.Uint64("block", receipt.BlockNumber.Uint64()),
			)
			return &TradeSubmission{
				TxHash:  receipt.TxHash,
				Success: true,
			}, nil

		case tradeRejectedEventSig:
			reason := "unknown"
			rejectedEvent := parsedABI.Events["TradeRejected"]
			if vals, uerr := rejectedEvent.Inputs.NonIndexed().Unpack(entry.Data); uerr == nil && len(vals) > 0 {
				if s, ok := vals[0].(string); ok {
					reason = s
				}
			}

			log.Warn("Trade rejected by RiskRouter",
				zap.String("tx_hash", receipt.TxHash.Hex()),
				zap.String("reason", reason),
			)
			return &TradeSubmission{
				TxHash:      receipt.TxHash,
				Success:     false,
				ErrorReason: reason,
			}, nil
		}
	}

	// 5. No recognized event - fall back to receipt status.
	if err != nil {
		// tx reverted (err from SendTx) but no parseable events.
		return &TradeSubmission{
			TxHash:      receipt.TxHash,
			Success:     false,
			ErrorReason: "tx reverted with no recognized events",
		}, nil
	}

	// Successful tx but no recognized events - fail closed.
	// This can happen on ABI drift or if the contract changes event signatures.
	log.Error("RiskRouter tx succeeded but no TradeApproved/TradeRejected events found - failing closed",
		zap.String("tx_hash", receipt.TxHash.Hex()),
	)
	return &TradeSubmission{
		TxHash:      receipt.TxHash,
		Success:     false,
		ErrorReason: "no TradeApproved/TradeRejected events in receipt",
	}, nil
}

// RouterAddr returns the Risk Router contract address.
func (r *RiskRouter) RouterAddr() common.Address {
	return r.routerAddr
}
