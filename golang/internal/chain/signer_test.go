package chain

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignTradeIntent(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	expectedAddr := crypto.PubkeyToAddress(key.PublicKey)

	tests := []struct {
		name    string
		intent  TradeIntentData
		chainID int64
		router  common.Address
	}{
		{
			"basic_buy",
			TradeIntentData{
				AgentID:         big.NewInt(1612),
				AgentWallet:     expectedAddr,
				Pair:            "ETH-USD",
				Action:          "BUY",
				AmountUsdScaled: big.NewInt(100000), // $1000.00
				MaxSlippageBps:  big.NewInt(50),
				Nonce:           big.NewInt(0),
				Deadline:        big.NewInt(1700000000),
			},
			11155111,
			common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		},
		{
			"sell_large_amount",
			TradeIntentData{
				AgentID:         big.NewInt(1612),
				AgentWallet:     expectedAddr,
				Pair:            "BTC-USD",
				Action:          "SELL",
				AmountUsdScaled: big.NewInt(500000), // $5000.00
				MaxSlippageBps:  big.NewInt(100),
				Nonce:           big.NewInt(1),
				Deadline:        big.NewInt(1700000000),
			},
			11155111,
			common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"),
		},
		{
			"different_chain",
			TradeIntentData{
				AgentID:         big.NewInt(42),
				AgentWallet:     expectedAddr,
				Pair:            "SOL-USD",
				Action:          "BUY",
				AmountUsdScaled: big.NewInt(50000),
				MaxSlippageBps:  big.NewInt(50),
				Nonce:           big.NewInt(2),
				Deadline:        big.NewInt(1700000000),
			},
			1,
			common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sig, err := SignTradeIntent(&tt.intent, key, big.NewInt(tt.chainID), tt.router)
			require.NoError(t, err)
			assert.Len(t, sig, 65, "signature should be r(32)+s(32)+v(1)")

			// v should be 27 or 28
			assert.True(t, sig[64] == 27 || sig[64] == 28, "v should be 27 or 28, got %d", sig[64])

			// Verify ecrecover returns the expected address
			recovered, err := RecoverSigner(&tt.intent, sig, big.NewInt(tt.chainID), tt.router)
			require.NoError(t, err)
			assert.Equal(t, expectedAddr, recovered, "recovered address should match signer")
		})
	}
}

func TestSignDeterminism(t *testing.T) {
	key, err := crypto.GenerateKey()
	require.NoError(t, err)

	intent := &TradeIntentData{
		AgentID:         big.NewInt(1612),
		AgentWallet:     crypto.PubkeyToAddress(key.PublicKey),
		Pair:            "ETH-USD",
		Action:          "BUY",
		AmountUsdScaled: big.NewInt(100000),
		MaxSlippageBps:  big.NewInt(50),
		Nonce:           big.NewInt(0),
		Deadline:        big.NewInt(1700000000),
	}
	chainID := big.NewInt(11155111)
	router := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	sig1, err := SignTradeIntent(intent, key, chainID, router)
	require.NoError(t, err)
	sig2, err := SignTradeIntent(intent, key, chainID, router)
	require.NoError(t, err)

	assert.Equal(t, sig1, sig2, "same inputs must produce same signature")
}

func TestSignDifferentKeysProduceDifferentSignatures(t *testing.T) {
	key1, _ := crypto.GenerateKey()
	key2, _ := crypto.GenerateKey()

	intent := &TradeIntentData{
		AgentID:         big.NewInt(1612),
		AgentWallet:     crypto.PubkeyToAddress(key1.PublicKey),
		Pair:            "ETH-USD",
		Action:          "BUY",
		AmountUsdScaled: big.NewInt(100000),
		MaxSlippageBps:  big.NewInt(50),
		Nonce:           big.NewInt(0),
		Deadline:        big.NewInt(1700000000),
	}
	chainID := big.NewInt(11155111)
	router := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	sig1, err := SignTradeIntent(intent, key1, chainID, router)
	require.NoError(t, err)
	sig2, err := SignTradeIntent(intent, key2, chainID, router)
	require.NoError(t, err)

	assert.NotEqual(t, sig1, sig2, "different keys must produce different signatures")
}

func TestRecoverSignerInvalidSignature(t *testing.T) {
	intent := &TradeIntentData{
		AgentID:         big.NewInt(1612),
		AgentWallet:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Pair:            "ETH-USD",
		Action:          "BUY",
		AmountUsdScaled: big.NewInt(100000),
		MaxSlippageBps:  big.NewInt(50),
		Nonce:           big.NewInt(0),
		Deadline:        big.NewInt(1700000000),
	}
	chainID := big.NewInt(11155111)
	router := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	tests := []struct {
		name string
		sig  []byte
	}{
		{"too_short", make([]byte, 64)},
		{"too_long", make([]byte, 66)},
		{"empty", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RecoverSigner(intent, tt.sig, chainID, router)
			assert.Error(t, err)
		})
	}
}

func TestDomainSeparation(t *testing.T) {
	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	intent := &TradeIntentData{
		AgentID:         big.NewInt(1612),
		AgentWallet:     addr,
		Pair:            "ETH-USD",
		Action:          "BUY",
		AmountUsdScaled: big.NewInt(100000),
		MaxSlippageBps:  big.NewInt(50),
		Nonce:           big.NewInt(0),
		Deadline:        big.NewInt(1700000000),
	}
	router := common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")

	// Same intent, different chain IDs -> different signatures
	sig1, _ := SignTradeIntent(intent, key, big.NewInt(1), router)
	sig2, _ := SignTradeIntent(intent, key, big.NewInt(11155111), router)
	assert.NotEqual(t, sig1, sig2, "different chain IDs must produce different signatures")

	// Same intent, different router addresses -> different signatures
	router2 := common.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")
	sig3, _ := SignTradeIntent(intent, key, big.NewInt(11155111), router)
	sig4, _ := SignTradeIntent(intent, key, big.NewInt(11155111), router2)
	assert.NotEqual(t, sig3, sig4, "different router addresses must produce different signatures")
}

func TestDomainNameIsRiskRouter(t *testing.T) {
	// Verify the domain name matches the hackathon shared contract exactly.
	intent := &TradeIntentData{
		AgentID:         big.NewInt(1),
		AgentWallet:     common.HexToAddress("0x1111111111111111111111111111111111111111"),
		Pair:            "ETH-USD",
		Action:          "BUY",
		AmountUsdScaled: big.NewInt(100000),
		MaxSlippageBps:  big.NewInt(50),
		Nonce:           big.NewInt(0),
		Deadline:        big.NewInt(1700000000),
	}
	td := tradeIntentTypedData(intent, big.NewInt(11155111), common.HexToAddress("0x1234567890abcdef1234567890abcdef12345678"))
	assert.Equal(t, "RiskRouter", td.Domain.Name, "domain name must be 'RiskRouter' to match shared contract")
	assert.Equal(t, "TradeIntent", td.PrimaryType)
	assert.Len(t, td.Types["TradeIntent"], 8, "TradeIntent must have exactly 8 fields matching reference")
}
