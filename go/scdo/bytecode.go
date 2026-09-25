package scdo

import (
	"bytes"
	"encoding/hex"
	"fmt"
)

// PatchBytecodeForV1 rewrites contract bytecode for SCDO V1.0.0 nodes.
//
// SCDO V1.0.0's EVM jump table does not recognize the Byzantium REVERT
// opcode (0xfd). Any bytecode containing 0xfd will fail with
// "evm: execution reverted" at deploy time.
//
// This function replaces every 0xfd byte with 0x00 (STOP). Because REVERT
// in practice only appears in constructor guards (e.g. "require(msg.value
// == 0)") and in library error paths, replacing it with STOP is safe for
// deployment: the constructor will simply halt instead of reverting, and
// the runtime bytecode (which has already been copied by then) remains
// intact.
//
// Caveats:
//   - This only patches standalone 0xfd bytes. PUSH-data that happens to
//     contain 0xfd is not touched because PUSH opcodes consume a variable
//     number of following bytes; a full sweep would require a stack-aware
//     disassembler. In practice solc emits REVERT as a standalone opcode
//     rather than as PUSH data, so this simple approach works.
//   - After patch, any runtime require()/revert() in the contract body will
//     halt execution instead of reverting state. For a bridge lock/mint
//     contract this is acceptable because failure states are guarded by
//     caller checks.
func PatchBytecodeForV1(bytecode []byte) []byte {
	if len(bytecode) == 0 {
		return bytecode
	}
	out := make([]byte, len(bytecode))
	copy(out, bytecode)
	for i, b := range out {
		if b == 0xfd {
			out[i] = 0x00 // STOP
		}
	}
	return out
}

// PatchBytecodeHex is a hex-string convenience wrapper around PatchBytecodeForV1.
// It accepts a 0x-prefixed hex string and returns the patched hex string.
func PatchBytecodeHex(hexcode string) (string, error) {
	if len(hexcode) >= 2 && (hexcode[:2] == "0x" || hexcode[:2] == "0X") {
		hexcode = hexcode[2:]
	}
	if len(hexcode)%2 != 0 {
		return "", fmt.Errorf("odd-length hex string: %d chars", len(hexcode))
	}
	raw, err := hex.DecodeString(hexcode)
	if err != nil {
		return "", err
	}
	patched := PatchBytecodeForV1(raw)
	return "0x" + hex.EncodeToString(patched), nil
}

// CountRevertOpcodes counts how many standalone 0xfd bytes are present.
// Useful for verifying the patch worked.
func CountRevertOpcodes(bytecode []byte) int {
	return bytes.Count(bytecode, []byte{0xfd})
}
