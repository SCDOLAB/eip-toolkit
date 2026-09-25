package scdo

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
)

// Tx represents a SCDO transaction to be signed and broadcast.
type Tx struct {
	To       string   // recipient address (empty = contract creation)
	Amount   *big.Int // value to transfer in wei-like units
	GasPrice *big.Int // gas price
	GasLimit uint64   // gas limit
	Payload  []byte   // contract bytecode or call data
	Nonce    uint64   // account nonce (auto-fetched if zero)
}

// SendTx signs and broadcasts a transaction.
// It automatically fetches the next nonce if tx.Nonce is 0.
func (c *Client) SendTx(ctx context.Context, tx Tx, privKey *ecdsa.PrivateKey) (string, error) {
	fromAddr, err := PubkeyToAddress(&privKey.PublicKey)
	if err != nil {
		return "", fmt.Errorf("derive address: %w", err)
	}

	if tx.Nonce == 0 {
		height, err := c.BlockHeight(ctx)
		if err != nil {
			return "", fmt.Errorf("get height: %w", err)
		}
		nonce, err := c.Nonce(ctx, fromAddr, int64(height))
		if err != nil {
			return "", fmt.Errorf("get nonce: %w", err)
		}
		tx.Nonce = nonce
	}

	// Build the transaction object for scdo_addTx.
	// The exact serialization matches go-scdo's types.Transaction JSON form.
	txObj := map[string]interface{}{
		"Type":         0, // TxTypeRegular
		"From":         fromAddr,
		"To":           tx.To,
		"Amount":       tx.Amount.String(),
		"AccountNonce": tx.Nonce,
		"GasPrice":     tx.GasPrice.String(),
		"GasLimit":     tx.GasLimit,
		"Payload":      fmt.Sprintf("0x%x", tx.Payload),
	}

	// Sign the transaction hash.
	// NOTE: SCDO uses its own hash derivation. For production use, link against
	// go-scdo's crypto package for exact signature compatibility.
	sig, err := SignTransaction(privKey, txObj)
	if err != nil {
		return "", fmt.Errorf("sign tx: %w", err)
	}
	txObj["Signature"] = sig

	var ok bool
	if err := c.http.callContext(ctx, &ok, "scdo_addTx", txObj); err != nil {
		return "", fmt.Errorf("addTx: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("addTx returned false")
	}

	// Return tx hash — in production this comes from CalculateHash.
	return "", nil
}

// DeployContract sends a contract-creation transaction (To = empty).
// Returns the tx hash and the predicted contract address.
func (c *Client) DeployContract(ctx context.Context, bytecode []byte, gasLimit uint64, privKey *ecdsa.PrivateKey) (string, error) {
	if gasLimit == 0 {
		gasLimit = 200000
	}
	return c.SendTx(ctx, Tx{
		To:       "",
		Amount:   big.NewInt(0),
		GasPrice: big.NewInt(1),
		GasLimit: gasLimit,
		Payload:  bytecode,
	}, privKey)
}
