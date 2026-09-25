// Package scdo provides a Go client for the SCDO PoW blockchain.
//
// SCDO is an EVM-compatible PoW chain with a custom RPC namespace (scdo_*)
// and Base58 addresses starting with "1". This package wraps the JSON-RPC
// interface and provides helpers for constructing, signing, and broadcasting
// transactions, as well as deploying smart contracts.
//
// Example:
//
//	client, err := scdo.Dial("http://192.168.50.50:8037")
//	if err != nil { log.Fatal(err) }
//
//	privKey, _ := crypto.HexToECDSA("0x...")
//	addr, _ := scdo.PubkeyToAddress(&privKey.PublicKey)
//
//	tx, err := client.SendTx(context.Background(), scdo.Tx{
//		To:       common.HexToAddress("1S01..."),
//		Amount:   big.NewInt(1e18),
//		GasPrice: big.NewInt(1),
//		GasLimit: 21000,
//	}, privKey)
package scdo

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// Client is a SCDO RPC client.
type Client struct {
	rpcURL string
	http   *jsonRPC
}

// Dial connects to a SCDO node's HTTP RPC endpoint.
func Dial(rpcURL string) (*Client, error) {
	return &Client{
		rpcURL: rpcURL,
		http:   newJSONRPC(rpcURL),
	}, nil
}

// BlockHeight returns the current chain tip height.
func (c *Client) BlockHeight(ctx context.Context) (uint64, error) {
	var height uint64
	err := c.http.callContext(ctx, &height, "scdo_getBlockHeight")
	return height, err
}

// Balance returns the balance of the given address at the specified height.
// If height is -1, the latest state is used.
func (c *Client) Balance(ctx context.Context, addr string, height int64) (*big.Int, error) {
	var hexBalance string
	err := c.http.callContext(ctx, &hexBalance, "scdo_getBalance", addr, "", height)
	if err != nil {
		return nil, err
	}
	balance, ok := new(big.Int).SetString(hexBalance, 0)
	if !ok {
		return nil, fmt.Errorf("invalid balance hex: %s", hexBalance)
	}
	return balance, nil
}

// Nonce returns the account nonce (transaction count) for the given address.
func (c *Client) Nonce(ctx context.Context, addr string, height int64) (uint64, error) {
	var nonce uint64
	err := c.http.callContext(ctx, &nonce, "scdo_getAccountNonce", addr, "", height)
	return nonce, err
}

// Receipt is a transaction receipt returned by scdo_getReceiptByTxHash.
type Receipt struct {
	TxHash          string            `json:"txhash"`
	ContractAddress string            `json:"contract"`
	Failed          bool              `json:"failed"`
	UsedGas         uint64            `json:"usedGas"`
	PostState       string            `json:"poststate"`
	Result          string            `json:"result"`
	TotalFee        *HexBig           `json:"totalFee"`
	Logs            []json.RawMessage `json:"logs"`
}

// GetReceipt queries a transaction receipt by its hash.
// Returns (nil, nil) if the receipt is not found yet (tx not mined).
func (c *Client) GetReceipt(ctx context.Context, txHash string) (*Receipt, error) {
	var r Receipt
	err := c.http.callContext(ctx, &r, "scdo_getReceiptByTxHash", txHash, "")
	if err != nil {
		// SCDO returns "leveldb: not found" when tx is not yet mined.
		if strings.Contains(err.Error(), "leveldb: not found") {
			return nil, nil
		}
		return nil, err
	}
	// Empty receipt means not found
	if r.TxHash == "" && r.ContractAddress == "" && r.UsedGas == 0 {
		return nil, nil
	}
	return &r, nil
}
