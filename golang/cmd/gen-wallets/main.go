// gen-wallets generates a single Ethereum keypair (private key + address).
// Use it however you need: agent signing, validator, deployer, etc.
//
// Run: go run ./cmd/gen-wallets
// Copy the output into .env, keep private keys secure.
package main

import (
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	k, err := crypto.GenerateKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate key: %v\n", err)
		os.Exit(1)
	}

	addr := crypto.PubkeyToAddress(k.PublicKey).Hex()
	key := fmt.Sprintf("0x%x", crypto.FromECDSA(k))

	fmt.Printf("Address:     %s\n", addr)
	fmt.Printf("Private key: %s\n", key)
	fmt.Println()
	fmt.Println("Fund with Sepolia ETH: https://cloud.google.com/application/web3/faucet/ethereum/sepolia")
	fmt.Println("WARNING: Save the private key securely. It will not be shown again.")
}
