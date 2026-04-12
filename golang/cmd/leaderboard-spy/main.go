// leaderboard-spy reads all agent data from the hackathon's on-chain contracts.
//
// Usage:
//
//	go run ./cmd/leaderboard-spy
//
// Reads from: AgentRegistry, RiskRouter, ValidationRegistry, ReputationRegistry, HackathonVault.
// All calls are read-only (eth_call) - no gas, no signing needed.
//
// Required env vars:
//
//	CHAIN_RPC_URL              - Sepolia RPC URL
//	HACKATHON_IDENTITY_ADDR    - AgentRegistry
//	HACKATHON_VALIDATION_ADDR  - ValidationRegistry
//	HACKATHON_REPUTATION_ADDR  - ReputationRegistry
//	HACKATHON_RISK_ROUTER_ADDR           - RiskRouter
//	HACKATHON_VAULT_ADDR       - HackathonVault
package main

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"reflect"
	"strings"
	"text/tabwriter"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Minimal ABIs - only view functions we need.
const registryABI = `[
  {"inputs":[],"name":"totalAgents","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAgent","outputs":[{"type":"tuple","components":[{"type":"address","name":"operatorWallet"},{"type":"address","name":"agentWallet"},{"type":"string","name":"name"},{"type":"string","name":"description"},{"type":"string[]","name":"capabilities"},{"type":"uint256","name":"registeredAt"},{"type":"bool","name":"active"}]}],"stateMutability":"view","type":"function"}
]`

const routerABI = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getTradeRecord","outputs":[{"type":"uint256","name":"count"},{"type":"uint256","name":"windowStart"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getIntentNonce","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const validationABI = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAverageValidationScore","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"attestationCount","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const reputationABI = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAverageScore","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

const vaultABI = `[
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"hasClaimed","outputs":[{"type":"bool"}],"stateMutability":"view","type":"function"},
  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getBalance","outputs":[{"type":"uint256"}],"stateMutability":"view","type":"function"}
]`

func main() {
	cmd := "agents"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	ctx := context.Background()
	rpcURL := mustEnv("CHAIN_RPC_URL")
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		fatal("dial rpc: %v", err)
	}

	switch cmd {
	case "agents":
		runAgents(ctx, client)
	case "trades":
		runTrades(ctx, client)
	case "attestations":
		runAttestations(ctx, client)
	default:
		_, _ = fmt.Fprintf(os.Stderr, "Usage: leaderboard-spy [agents|trades|attestations <agentId>]\n")
		os.Exit(1)
	}
}

func runAgents(ctx context.Context, client *ethclient.Client) {
	reg := mustContract(registryABI)
	rtr := mustContract(routerABI)
	val := mustContract(validationABI)
	rep := mustContract(reputationABI)
	vlt := mustContract(vaultABI)

	regAddr := mustAddr("HACKATHON_IDENTITY_ADDR")
	rtrAddr := mustAddr("HACKATHON_RISK_ROUTER_ADDR")
	valAddr := mustAddr("HACKATHON_VALIDATION_ADDR")
	repAddr := mustAddr("HACKATHON_REPUTATION_ADDR")
	vltAddr := mustAddr("HACKATHON_VAULT_ADDR")

	total := callUint(ctx, client, reg, regAddr, "totalAgents")
	fmt.Printf("Total agents registered: %d\n\n", total)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "ID\tName\tOperator\tWallet\tTrades\tNonce\tVal.Score\tAttestations\tReputation\tClaimed\n")
	_, _ = fmt.Fprintf(w, "--\t----\t--------\t------\t------\t-----\t---------\t------------\t----------\t-------\n")

	for i := int64(0); i < total; i++ {
		id := big.NewInt(i)

		name, operator, wallet := getAgentInfo(ctx, client, reg, regAddr, id)
		if name == "" {
			name = fmt.Sprintf("Agent #%d", i)
		}

		trades := callUint(ctx, client, rtr, rtrAddr, "getTradeRecord", id)
		nonce := callUint(ctx, client, rtr, rtrAddr, "getIntentNonce", id)
		valScore := callUint(ctx, client, val, valAddr, "getAverageValidationScore", id)
		attestCount := callUint(ctx, client, val, valAddr, "attestationCount", id)
		repScore := callUint(ctx, client, rep, repAddr, "getAverageScore", id)
		claimed := callBool(ctx, client, vlt, vltAddr, "hasClaimed", id)

		claimedStr := "no"
		if claimed {
			claimedStr = "yes"
		}

		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
			i,
			truncate(name, 25),
			shortAddr(operator),
			shortAddr(wallet),
			trades,
			nonce,
			valScore,
			attestCount,
			repScore,
			claimedStr,
		)
	}
	_ = w.Flush()
}

// Event signatures for RiskRouter
var (
	tradeIntentSubmittedSig = crypto.Keccak256Hash([]byte("TradeIntentSubmitted(uint256,bytes32,string,string,uint256)"))
	spyTradeApprovedSig     = crypto.Keccak256Hash([]byte("TradeApproved(uint256,bytes32,uint256)"))
	spyTradeRejectedSig     = crypto.Keccak256Hash([]byte("TradeRejected(uint256,bytes32,string)"))
)

func runTrades(ctx context.Context, client *ethclient.Client) {
	rtrAddr := mustAddr("HACKATHON_RISK_ROUTER_ADDR")
	regAddr := mustAddr("HACKATHON_IDENTITY_ADDR")
	reg := mustContract(registryABI)

	// Build agent name lookup
	total := callUint(ctx, client, reg, regAddr, "totalAgents")
	names := make(map[int64]string)
	for i := int64(0); i < total; i++ {
		name, _, _ := getAgentInfo(ctx, client, reg, regAddr, big.NewInt(i))
		if name == "" {
			name = fmt.Sprintf("#%d", i)
		}
		names[i] = name
	}

	// Query TradeIntentSubmitted events from RiskRouter.
	// These contain pair, action, and amount for every submission.
	// We also query TradeApproved/TradeRejected to know the outcome.

	// Hackathon started March 30. Sepolia ~12s blocks.
	// Query in chunks to avoid RPC rate limits.
	latest, err := client.BlockNumber(ctx)
	if err != nil {
		fatal("get block number: %v", err)
	}
	// ~7 days back (hackathon started Mar 30, it's Apr 7)
	fromBlock := int64(latest) - 50000
	if fromBlock < 0 {
		fromBlock = 0
	}

	fmt.Println("Fetching trade events from RiskRouter...")
	fmt.Printf("Block range: %d - %d\n", fromBlock, latest)

	// Query in 10k-block chunks to avoid RPC rate limits
	var approvedLogs, rejectedLogs, intentLogs []types.Log
	chunkSize := int64(10000)
	for start := fromBlock; start <= int64(latest); start += chunkSize {
		end := start + chunkSize - 1
		if end > int64(latest) {
			end = int64(latest)
		}
		from := big.NewInt(start)
		to := big.NewInt(end)

		logs, err := client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: from, ToBlock: to,
			Addresses: []common.Address{rtrAddr},
			Topics:    [][]common.Hash{{spyTradeApprovedSig}},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: approved logs chunk %d-%d: %v\n", start, end, err)
		}
		approvedLogs = append(approvedLogs, logs...)

		logs, err = client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: from, ToBlock: to,
			Addresses: []common.Address{rtrAddr},
			Topics:    [][]common.Hash{{spyTradeRejectedSig}},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: rejected logs chunk %d-%d: %v\n", start, end, err)
		}
		rejectedLogs = append(rejectedLogs, logs...)

		logs, err = client.FilterLogs(ctx, ethereum.FilterQuery{
			FromBlock: from, ToBlock: to,
			Addresses: []common.Address{rtrAddr},
			Topics:    [][]common.Hash{{tradeIntentSubmittedSig}},
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: intent logs chunk %d-%d: %v\n", start, end, err)
		}
		intentLogs = append(intentLogs, logs...)
	}
	fmt.Println()

	// Parse the intent ABI for non-indexed fields
	intentEventABI := mustContract(`[
	  {"anonymous":false,"name":"TradeIntentSubmitted","type":"event","inputs":[
	    {"type":"uint256","name":"agentId","indexed":true},
	    {"type":"bytes32","name":"intentHash","indexed":true},
	    {"type":"string","name":"pair","indexed":false},
	    {"type":"string","name":"action","indexed":false},
	    {"type":"uint256","name":"amountUsdScaled","indexed":false}
	  ]}
	]`)

	rejectedEventABI := mustContract(`[
	  {"anonymous":false,"name":"TradeRejected","type":"event","inputs":[
	    {"type":"uint256","name":"agentId","indexed":true},
	    {"type":"bytes32","name":"intentHash","indexed":true},
	    {"type":"string","name":"reason","indexed":false}
	  ]}
	]`)

	// Build intentHash -> outcome map
	type outcome struct {
		approved bool
		reason   string
	}
	outcomes := make(map[common.Hash]outcome)
	for _, log := range approvedLogs {
		if len(log.Topics) >= 2 {
			outcomes[log.Topics[1]] = outcome{approved: true}
		}
	}
	for _, log := range rejectedLogs {
		if len(log.Topics) >= 2 {
			reason := "unknown"
			if vals, err := rejectedEventABI.Events["TradeRejected"].Inputs.NonIndexed().Unpack(log.Data); err == nil && len(vals) > 0 {
				if s, ok := vals[0].(string); ok {
					reason = s
				}
			}
			outcomes[log.Topics[1]] = outcome{approved: false, reason: reason}
		}
	}

	fmt.Printf("Total intents submitted: %d (approved: %d, rejected: %d)\n\n",
		len(intentLogs), len(approvedLogs), len(rejectedLogs))

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "Block\tAgent\tPair\tAction\tAmount($)\tResult\tReason\n")
	_, _ = fmt.Fprintf(w, "-----\t-----\t----\t------\t---------\t------\t------\n")

	for _, log := range intentLogs {
		if len(log.Topics) < 2 {
			continue
		}
		agentID := new(big.Int).SetBytes(log.Topics[1].Bytes()).Int64()
		intentHash := log.Topics[1]
		_ = intentHash

		// Parse non-indexed data: pair, action, amountUsdScaled
		vals, err := intentEventABI.Events["TradeIntentSubmitted"].Inputs.NonIndexed().Unpack(log.Data)
		if err != nil || len(vals) < 3 {
			continue
		}

		pair, _ := vals[0].(string)
		action, _ := vals[1].(string)
		amountScaled, _ := vals[2].(*big.Int)
		amountUsd := float64(0)
		if amountScaled != nil {
			amountUsd = float64(amountScaled.Int64()) / 100.0
		}

		// Lookup intent hash from Topics[2] if available
		var intentH common.Hash
		if len(log.Topics) >= 3 {
			intentH = log.Topics[2]
		}

		result := "???"
		reason := ""
		if o, ok := outcomes[intentH]; ok {
			if o.approved {
				result = "APPROVED"
			} else {
				result = "REJECTED"
				reason = o.reason
			}
		}

		agentName := names[agentID]
		if agentName == "" {
			agentName = fmt.Sprintf("#%d", agentID)
		}

		_, _ = fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%.2f\t%s\t%s\n",
			log.BlockNumber,
			truncate(agentName, 20),
			pair,
			action,
			amountUsd,
			result,
			truncate(reason, 30),
		)
	}
	_ = w.Flush()
}

func runAttestations(ctx context.Context, client *ethclient.Client) {
	valAddr := mustAddr("HACKATHON_VALIDATION_ADDR")
	regAddr := mustAddr("HACKATHON_IDENTITY_ADDR")
	reg := mustContract(registryABI)

	// Which agent to inspect
	agentID := int64(0)
	if len(os.Args) > 2 {
		_, _ = fmt.Sscanf(os.Args[2], "%d", &agentID)
	}

	// Get agent name
	name, _, _ := getAgentInfo(ctx, client, reg, regAddr, big.NewInt(agentID))
	if name == "" {
		name = fmt.Sprintf("Agent #%d", agentID)
	}

	// ABI for reading attestations
	attABI := mustContract(`[
	  {"inputs":[{"type":"uint256","name":"agentId"}],"name":"getAttestations","outputs":[{"type":"tuple[]","components":[{"type":"uint256","name":"agentId"},{"type":"address","name":"validator"},{"type":"bytes32","name":"checkpointHash"},{"type":"uint8","name":"score"},{"type":"uint8","name":"proofType"},{"type":"bytes","name":"proof"},{"type":"string","name":"notes"},{"type":"uint256","name":"timestamp"}]}],"stateMutability":"view","type":"function"}
	]`)

	data, err := attABI.Pack("getAttestations", big.NewInt(agentID))
	if err != nil {
		fatal("pack getAttestations: %v", err)
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &valAddr, Data: data}, nil)
	if err != nil {
		fatal("call getAttestations: %v", err)
	}
	vals, err := attABI.Methods["getAttestations"].Outputs.Unpack(result)
	if err != nil {
		fatal("unpack getAttestations: %v", err)
	}

	if len(vals) == 0 {
		fmt.Printf("No attestations for %s (id=%d)\n", name, agentID)
		return
	}

	// vals[0] is a slice of anonymous structs
	attestations := reflect.ValueOf(vals[0])
	count := attestations.Len()
	fmt.Printf("Attestations for %s (id=%d): %d total\n\n", name, agentID, count)

	// Show last 20 (or all if fewer)
	start := 0
	if count > 20 {
		start = count - 20
		fmt.Printf("(showing last 20 of %d)\n\n", count)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "#\tScore\tNotes\n")
	_, _ = fmt.Fprintf(w, "-\t-----\t-----\n")

	for i := start; i < count; i++ {
		att := attestations.Index(i)
		score := att.FieldByName("Score").Uint()
		notes := att.FieldByName("Notes").String()
		_, _ = fmt.Fprintf(w, "%d\t%d\t%s\n", i, score, truncate(notes, 80))
	}
	_ = w.Flush()
}

func getAgentInfo(ctx context.Context, client *ethclient.Client, reg abi.ABI, addr common.Address, id *big.Int) (name string, operator, wallet common.Address) {
	data, err := reg.Pack("getAgent", id)
	if err != nil {
		return
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, nil)
	if err != nil {
		return
	}
	vals, err := reg.Methods["getAgent"].Outputs.Unpack(result)
	if err != nil || len(vals) == 0 {
		return
	}
	// go-ethereum returns the tuple as an anonymous struct via reflection.
	// Use fmt.Sprintf + reflect to extract fields safely.
	v := reflect.ValueOf(vals[0])
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("Name"); f.IsValid() {
			name = f.String()
		}
		if f := v.FieldByName("OperatorWallet"); f.IsValid() {
			operator = f.Interface().(common.Address)
		}
		if f := v.FieldByName("AgentWallet"); f.IsValid() {
			wallet = f.Interface().(common.Address)
		}
	}
	return
}

func callUint(ctx context.Context, client *ethclient.Client, parsed abi.ABI, addr common.Address, method string, args ...interface{}) int64 {
	data, err := parsed.Pack(method, args...)
	if err != nil {
		return 0
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, nil)
	if err != nil {
		return 0
	}
	vals, err := parsed.Unpack(method, result)
	if err != nil || len(vals) == 0 {
		return 0
	}
	if v, ok := vals[0].(*big.Int); ok {
		return v.Int64()
	}
	return 0
}

func callBool(ctx context.Context, client *ethclient.Client, parsed abi.ABI, addr common.Address, method string, args ...interface{}) bool {
	data, err := parsed.Pack(method, args...)
	if err != nil {
		return false
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &addr, Data: data}, nil)
	if err != nil {
		return false
	}
	vals, err := parsed.Unpack(method, result)
	if err != nil || len(vals) == 0 {
		return false
	}
	if v, ok := vals[0].(bool); ok {
		return v
	}
	return false
}

func mustContract(abiJSON string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		fatal("parse abi: %v", err)
	}
	return parsed
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fatal("env var %s not set", key)
	}
	return v
}

func mustAddr(key string) common.Address {
	return common.HexToAddress(mustEnv(key))
}

func shortAddr(a common.Address) string {
	s := a.Hex()
	return s[:6] + "..." + s[len(s)-4:]
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-2] + ".."
}

func fatal(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}
