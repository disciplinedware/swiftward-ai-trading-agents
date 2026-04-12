package chain

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"

	"ai-trading-agents/internal/observability"
)

const (
	// Gas bumping: resubmit with higher fees if not confirmed within this window.
	gasBumpInterval   = 10 * time.Second
	gasBumpMultiplier = 120 // 120/100 = 1.20x (+20% each bump)
	gasBumpMaxRetries = 3
	gasEstimateBuffer = 120 // 120/100 = 1.20x (20% buffer on estimated gas)

	// Rate limit retry: exponential backoff on 429 responses.
	rpcRetryMaxAttempts = 4
	rpcRetryBaseBackoff = 2 * time.Second
)

// Client wraps an Ethereum JSON-RPC client with signing capabilities.
type Client struct {
	eth     *ethclient.Client
	chainID *big.Int
	log     *zap.Logger
	txMu    sync.Mutex // serializes all write transactions to prevent concurrent RPC bursts
}

// NewClient creates a new chain client.
func NewClient(rpcURL string, chainID string, log *zap.Logger) (*Client, error) {
	eth, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", rpcURL, err)
	}

	id := new(big.Int)
	if _, ok := id.SetString(chainID, 10); !ok {
		return nil, fmt.Errorf("invalid chain_id: %s", chainID)
	}

	return &Client{
		eth:     eth,
		chainID: id,
		log:     log,
	}, nil
}

// ChainID returns the configured chain ID.
func (c *Client) ChainID() *big.Int {
	return new(big.Int).Set(c.chainID)
}

// GetLatestBlock returns the latest block number.
func (c *Client) GetLatestBlock(ctx context.Context) (uint64, error) {
	return c.eth.BlockNumber(ctx)
}

// Close closes the underlying ethclient connection.
func (c *Client) Close() {
	c.eth.Close()
}

// isRateLimited checks if an RPC error is a 429 / rate limit response.
// go-ethereum surfaces HTTP 429 as a string error containing the status code.
func isRateLimited(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "429") ||
		strings.Contains(msg, "too many requests") ||
		strings.Contains(msg, "rate limit") ||
		strings.Contains(msg, "request rate exceeded") ||
		strings.Contains(msg, "-32005")
}

// rpcRetry retries an RPC call with exponential backoff on 429 rate limit errors.
// Non-rate-limit errors are returned immediately.
func rpcRetry[T any](ctx context.Context, log *zap.Logger, name string, fn func() (T, error)) (T, error) {
	backoff := rpcRetryBaseBackoff
	for attempt := range rpcRetryMaxAttempts {
		result, err := fn()
		if err == nil || !isRateLimited(err) {
			return result, err
		}
		if attempt == rpcRetryMaxAttempts-1 {
			return result, fmt.Errorf("%s: rate limited after %d retries: %w", name, rpcRetryMaxAttempts, err)
		}
		log.Warn("RPC rate limited, backing off",
			zap.String("call", name),
			zap.Int("attempt", attempt+1),
			zap.Duration("backoff", backoff),
		)
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	var zero T
	return zero, errors.New("unreachable")
}

// SendTx signs and broadcasts an EIP-1559 transaction with gas bumping.
//
// If the tx is not confirmed within gasBumpInterval, it resubmits with the same
// nonce but +20% higher fees (replace-by-fee). Up to gasBumpMaxRetries attempts.
//
// Serialized by txMu to prevent concurrent RPC bursts from multiple goroutines.
func (c *Client) SendTx(ctx context.Context, from *ecdsa.PrivateKey, to common.Address, data []byte) (*types.Receipt, error) {
	c.txMu.Lock()
	defer c.txMu.Unlock()

	log := observability.LoggerFromCtx(ctx, c.log)
	fromAddr := AddressFromKey(from)

	nonce, err := rpcRetry(ctx, log, "PendingNonceAt", func() (uint64, error) {
		return c.eth.PendingNonceAt(ctx, fromAddr)
	})
	if err != nil {
		return nil, fmt.Errorf("pending nonce: %w", err)
	}

	gasLimit, err := rpcRetry(ctx, log, "EstimateGas", func() (uint64, error) {
		return c.eth.EstimateGas(ctx, ethereum.CallMsg{
			From: fromAddr,
			To:   &to,
			Data: data,
		})
	})
	if err != nil {
		return nil, fmt.Errorf("estimate gas: %w", err)
	}
	gasLimit = gasLimit * gasEstimateBuffer / 100

	// EIP-1559 fees: base fee from latest header + priority fee tip.
	header, err := rpcRetry(ctx, log, "HeaderByNumber", func() (*types.Header, error) {
		return c.eth.HeaderByNumber(ctx, nil)
	})
	if err != nil {
		return nil, fmt.Errorf("get latest header: %w", err)
	}

	gasTipCap, err := rpcRetry(ctx, log, "SuggestGasTipCap", func() (*big.Int, error) {
		return c.eth.SuggestGasTipCap(ctx)
	})
	if err != nil {
		return nil, fmt.Errorf("suggest gas tip cap: %w", err)
	}

	// maxFeePerGas = 2 * baseFee + tipCap (plenty of headroom, actual cost is baseFee + tipCap).
	baseFee := header.BaseFee
	gasFeeCap := new(big.Int).Add(new(big.Int).Mul(baseFee, big.NewInt(2)), gasTipCap)

	signer := types.NewLondonSigner(c.chainID)

	// Track all sent tx hashes so we can fetch the receipt if an earlier one lands.
	var sentHashes []common.Hash

	for attempt := range gasBumpMaxRetries {
		tx := types.NewTx(&types.DynamicFeeTx{
			ChainID:   c.chainID,
			Nonce:     nonce,
			GasTipCap: gasTipCap,
			GasFeeCap: gasFeeCap,
			Gas:       gasLimit,
			To:        &to,
			Data:      data,
		})

		signedTx, signErr := types.SignTx(tx, signer, from)
		if signErr != nil {
			return nil, fmt.Errorf("sign tx: %w", signErr)
		}

		_, sendErr := rpcRetry(ctx, log, "SendTransaction", func() (bool, error) {
			return true, c.eth.SendTransaction(ctx, signedTx)
		})
		if sendErr != nil {
			errMsg := sendErr.Error()

			// "nonce too low" means a tx with this nonce was already confirmed.
			if strings.Contains(errMsg, "nonce too low") && len(sentHashes) > 0 {
				log.Info("Nonce already used, checking previous tx receipts")
				if receipt, found := c.findConfirmedReceipt(ctx, sentHashes); found {
					return c.handleReceipt(log, receipt)
				}
			}

			// "already known" means the node already has this exact tx in its
			// mempool (e.g. network glitch made us think a prior send failed).
			// The tx IS pending - treat it as successfully sent and wait for it.
			if strings.Contains(errMsg, "already known") {
				log.Info("Transaction already in mempool, waiting for confirmation",
					zap.String("hash", signedTx.Hash().Hex()),
				)
				sentHashes = append(sentHashes, signedTx.Hash())
				receipt, waitErr := c.waitForAny(ctx, sentHashes, gasBumpInterval)
				if waitErr == nil {
					return c.handleReceipt(log, receipt)
				}
				// Not confirmed - fall through to bump on next attempt.
				if attempt < gasBumpMaxRetries-1 {
					gasTipCap = bumpFee(gasTipCap)
					gasFeeCap = bumpFee(gasFeeCap)
					continue
				}
				return nil, fmt.Errorf("tx %s not confirmed after %d attempts",
					signedTx.Hash().Hex(), attempt+1)
			}

			// "replacement underpriced" means our bumped fee isn't high enough
			// to replace the pending tx. Bump more aggressively.
			if strings.Contains(errMsg, "replacement transaction underpriced") {
				if attempt < gasBumpMaxRetries-1 {
					log.Warn("Replacement underpriced, bumping gas",
						zap.Int("attempt", attempt+1),
					)
					gasTipCap = bumpFee(gasTipCap)
					gasFeeCap = bumpFee(gasFeeCap)
					continue
				}
				// Last attempt - wait for any previously sent tx.
				if receipt, waitErr := c.waitForAny(ctx, sentHashes, gasBumpInterval); waitErr == nil {
					return c.handleReceipt(log, receipt)
				}
			}

			return nil, fmt.Errorf("send tx: %w", sendErr)
		}

		sentHashes = append(sentHashes, signedTx.Hash())

		log.Info(fmt.Sprintf("Tx %s sent, waiting for confirmation", signedTx.Hash().Hex()[:10]),
			zap.String("hash", signedTx.Hash().Hex()),
			zap.String("from", fromAddr.Hex()),
			zap.String("to", to.Hex()),
			zap.Int("attempt", attempt+1),
			zap.String("max_fee_gwei", weiToGwei(gasFeeCap)),
			zap.String("tip_gwei", weiToGwei(gasTipCap)),
		)

		// Poll ALL sent hashes - an earlier tx with lower fees could land first.
		receipt, waitErr := c.waitForAny(ctx, sentHashes, gasBumpInterval)
		if waitErr == nil {
			return c.handleReceipt(log, receipt)
		}

		// Not confirmed in time - bump fees for replacement tx (same nonce).
		if attempt < gasBumpMaxRetries-1 {
			gasTipCap = bumpFee(gasTipCap)
			gasFeeCap = bumpFee(gasFeeCap)
			log.Warn("Transaction not confirmed, bumping gas",
				zap.String("hash", signedTx.Hash().Hex()),
				zap.Int("next_attempt", attempt+2),
				zap.String("new_max_fee_gwei", weiToGwei(gasFeeCap)),
				zap.String("new_tip_gwei", weiToGwei(gasTipCap)),
			)
			continue
		}

		return nil, fmt.Errorf("tx %s not confirmed after %d attempts (%v)",
			signedTx.Hash().Hex(), gasBumpMaxRetries,
			time.Duration(gasBumpMaxRetries)*gasBumpInterval)
	}

	// Unreachable, but satisfies the compiler.
	return nil, errors.New("gas bumping loop exhausted")
}

// waitForAny polls for a receipt of ANY of the given tx hashes until one
// appears or the timeout elapses. This handles the case where an earlier
// RBF tx (lower fees) gets confirmed before a later replacement.
func (c *Client) waitForAny(ctx context.Context, hashes []common.Hash, timeout time.Duration) (*types.Receipt, error) {
	if len(hashes) == 0 {
		return nil, errors.New("no tx hashes to wait for")
	}

	deadline := time.After(timeout)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("timeout waiting for %d tx(es)", len(hashes))
		case <-ticker.C:
			if receipt, found := c.findConfirmedReceipt(ctx, hashes); found {
				return receipt, nil
			}
		}
	}
}

// bumpFee increases a fee value by gasBumpMultiplier (20%).
// If the result is zero (e.g. tip=0 on testnets), returns at least 1 gwei
// so that replacement txs actually have higher fees.
func bumpFee(fee *big.Int) *big.Int {
	bumped := new(big.Int).Mul(fee, big.NewInt(int64(gasBumpMultiplier)))
	bumped.Div(bumped, big.NewInt(100))
	if bumped.Sign() == 0 {
		bumped.SetInt64(1_000_000_000) // 1 gwei floor
	}
	return bumped
}

// handleReceipt logs and returns a mined receipt, checking for reverts.
func (c *Client) handleReceipt(log *zap.Logger, receipt *types.Receipt) (*types.Receipt, error) {
	if receipt.Status != types.ReceiptStatusSuccessful {
		return receipt, fmt.Errorf("tx reverted (hash=%s)", receipt.TxHash.Hex())
	}
	log.Info(fmt.Sprintf("Tx %s confirmed in block %d", receipt.TxHash.Hex()[:10], receipt.BlockNumber.Uint64()),
		zap.String("hash", receipt.TxHash.Hex()),
		zap.Uint64("block", receipt.BlockNumber.Uint64()),
		zap.Uint64("gas_used", receipt.GasUsed),
	)
	return receipt, nil
}

// findConfirmedReceipt checks if any previously sent tx hash has been confirmed.
// Backs off on 429 rate limit errors instead of treating them as "not found".
func (c *Client) findConfirmedReceipt(ctx context.Context, hashes []common.Hash) (*types.Receipt, bool) {
	for _, h := range hashes {
		receipt, err := rpcRetry(ctx, c.log, "TransactionReceipt", func() (*types.Receipt, error) {
			return c.eth.TransactionReceipt(ctx, h)
		})
		if err == nil {
			return receipt, true
		}
	}
	return nil, false
}

// weiToGwei formats wei as a human-readable gwei string.
func weiToGwei(wei *big.Int) string {
	gwei := new(big.Int).Div(wei, big.NewInt(1_000_000_000))
	return gwei.String()
}

// CallContract calls a read-only (view) function on a contract.
func (c *Client) CallContract(ctx context.Context, to common.Address, data []byte) ([]byte, error) {
	return c.eth.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, nil)
}

// BalanceWei returns the balance of an address in wei (free eth_call, no gas).
func (c *Client) BalanceWei(ctx context.Context, addr common.Address) (*big.Int, error) {
	wei, err := c.eth.BalanceAt(ctx, addr, nil)
	if err != nil {
		return nil, fmt.Errorf("balance at %s: %w", addr.Hex(), err)
	}
	return wei, nil
}

// ParsePrivateKey parses a hex-encoded private key (with or without 0x prefix).
func ParsePrivateKey(hexKey string) (*ecdsa.PrivateKey, error) {
	hexKey = strings.TrimPrefix(hexKey, "0x")
	return crypto.HexToECDSA(hexKey)
}

// AddressFromKey derives the Ethereum address from a private key.
func AddressFromKey(key *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(key.PublicKey)
}
