package scdo

import (
	"crypto/ecdsa"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"
)

// PubkeyToAddress derives a SCDO address from a public key.
// SCDO uses a 20-byte address with a Base58 checksum encoding prefixed by "1".
// This helper returns the raw hex form (0x-prefixed) which matches what
// SCDO RPC methods accept.
//
// NOTE: For production SCDO addresses (Base58 "1..." format), use go-scdo's
// crypto.GetAddress which applies the shard byte and checksum. This function
// returns the internal 20-byte representation.
func PubkeyToAddress(pubkey *ecdsa.PublicKey) (string, error) {
	addr := crypto.PubkeyToAddress(*pubkey)
	return addr.Hex(), nil
}

// HexToECDSA parses a hex-encoded private key (with or without 0x prefix).
func HexToECDSA(hexkey string) (*ecdsa.PrivateKey, error) {
	return crypto.HexToECDSA(hexkey)
}

// SignTransaction produces a signature over the transaction.
// SCDO uses secp256k1 but its own RLP/hash derivation. This is a stub —
// in production, use go-scdo's crypto.Sign directly.
func SignTransaction(privKey *ecdsa.PrivateKey, txObj map[string]interface{}) (map[string]interface{}, error) {
	// TODO: implement exact SCDO tx hash + signing.
	// For now, this placeholder demonstrates the wiring.
	return map[string]interface{}{
		"method":  "placeholder",
		"message": fmt.Sprintf("sign tx nonce=%v", txObj["AccountNonce"]),
	}, nil
}
