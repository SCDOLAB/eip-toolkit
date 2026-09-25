// Package main implements the EIP-1559 base fee calculator.
//
// BaseFeeNext = BaseFeeCurrent * (1 + (GasUsed - TargetGas) / TargetGas / 8)
//   - TargetGas    = 15,000,000 (per EIP-1559)
//   - block limit  = 2 * TargetGas
//   - denominator  = 8, so max per-block move is +/-12.5%
//   - result is truncated to an integer number of wei, minimum 1 wei.
package main

import (
	"fmt"
	"math/big"
)

// TargetGas is the EIP-1559 target gas used per block.
const TargetGas uint64 = 15_000_000

// CalcNextBaseFee computes the base fee for the block after the one that
// consumed gasUsed, given currentBaseFee in wei.
//
// It follows the two-step division in the EIP-1559 reference spec:
//
//	abs_delta   = |gasUsed - target|
//	scaled      = currentBaseFee * abs_delta / target
//	delta       = scaled / base_fee_max_change_denominator (== 8)
//	next        = currentBaseFee +/- delta
func CalcNextBaseFee(currentBaseFee *big.Int, gasUsed uint64) *big.Int {
	delta := new(big.Int).SetUint64(gasUsed)
	delta.Sub(delta, new(big.Int).SetUint64(TargetGas))

	absDelta := new(big.Int).Abs(delta)
	scaled := new(big.Int).Mul(currentBaseFee, absDelta)
	scaled.Div(scaled, new(big.Int).SetUint64(TargetGas))
	scaled.Div(scaled, new(big.Int).SetUint64(8))
	if delta.Sign() < 0 {
		scaled.Neg(scaled)
	}

	next := new(big.Int).Add(currentBaseFee, scaled)
	if next.Cmp(big.NewInt(1)) < 0 {
		return big.NewInt(1)
	}
	return next
}

func main() {
	current := big.NewInt(100)
	gasUsed := uint64(24_000_000)
	fmt.Printf("Next BaseFee = %s wei\n", CalcNextBaseFee(current, gasUsed).String())
}
