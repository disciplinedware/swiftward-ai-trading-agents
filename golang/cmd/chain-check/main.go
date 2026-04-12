// Quick one-off checks against hackathon contracts.
package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	client, err := ethclient.Dial(os.Getenv("CHAIN_RPC_URL"))
	if err != nil {
		fmt.Println("ERROR:", err)
		return
	}

	valAddr := common.HexToAddress(os.Getenv("HACKATHON_VALIDATION_ADDR"))
	agentAddr := common.HexToAddress("0x6Cd7DdABD496b545bAE05a04044F2828C1395d13")
	ctx := context.Background()

	// 1. Check openValidation
	a, _ := abi.JSON(strings.NewReader(`[{"inputs":[],"name":"openValidation","outputs":[{"type":"bool"}],"stateMutability":"view","type":"function"}]`))
	data, _ := a.Pack("openValidation")
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &valAddr, Data: data}, nil)
	if err != nil {
		fmt.Println("openValidation ERROR:", err)
	} else {
		vals, _ := a.Unpack("openValidation", result)
		fmt.Printf("ValidationRegistry.openValidation = %v\n", vals[0])
	}

	// 2. Check wallet balance
	bal, _ := client.BalanceAt(ctx, agentAddr, nil)
	eth := new(big.Float).Quo(new(big.Float).SetInt(bal), new(big.Float).SetFloat64(1e18))
	fmt.Printf("Agent wallet balance: %s ETH\n", eth.Text('f', 6))

	// 3. Check RiskRouter default params for our agent
	rtrAddr := common.HexToAddress(os.Getenv("HACKATHON_RISK_ROUTER_ADDR"))
	r, _ := abi.JSON(strings.NewReader(`[{"inputs":[{"type":"uint256"}],"name":"getIntentNonce","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}]`))
	data, _ = r.Pack("getIntentNonce", big.NewInt(32))
	result, err = client.CallContract(ctx, ethereum.CallMsg{To: &rtrAddr, Data: data}, nil)
	if err != nil {
		fmt.Println("getIntentNonce ERROR:", err)
	} else {
		vals, _ := r.Unpack("getIntentNonce", result)
		fmt.Printf("RiskRouter nonce for agentId=32: %v\n", vals[0])
	}
}
