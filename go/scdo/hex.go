package scdo

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"
)

// HexBig is a big.Int that unmarshals from a hex string like "0x6648".
type HexBig big.Int

// UnmarshalJSON implements json.Unmarshaler.
func (h *HexBig) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")
	if s == "" || s == "null" {
		(*big.Int)(h).SetInt64(0)
		return nil
	}
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return fmt.Errorf("invalid hex big int: %s", string(data))
	}
	(*big.Int)(h).Set(n)
	return nil
}

// Int returns the underlying *big.Int.
func (h *HexBig) Int() *big.Int {
	return (*big.Int)(h)
}

// MarshalJSON implements json.Marshaler.
func (h *HexBig) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%x", (*big.Int)(h)))
}
