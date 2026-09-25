package main

import (
	"math/big"
	"testing"
)

func TestCalcNextBaseFee_Increase(t *testing.T) {
	// current=100 wei, gasUsed=24,000,000
	// next = 100 * (1 + (24M-15M)/15M/8) = 100 * 1.075 = 107 wei
	current := big.NewInt(100)
	gasUsed := uint64(24_000_000)
	want := big.NewInt(107)

	got := CalcNextBaseFee(current, gasUsed)
	if got.Cmp(want) != 0 {
		t.Errorf("CalcNextBaseFee() = %s, want %s", got.String(), want.String())
	}
}

func TestCalcNextBaseFee_Decrease(t *testing.T) {
	// current=100, gasUsed=0 -> delta = -15M
	// fraction = 100 * -15M / (15M*8) = -100/8 = -12 (truncated)
	// next = 88
	current := big.NewInt(100)
	gasUsed := uint64(0)
	want := big.NewInt(88)

	got := CalcNextBaseFee(current, gasUsed)
	if got.Cmp(want) != 0 {
		t.Errorf("CalcNextBaseFee() = %s, want %s", got.String(), want.String())
	}
}

func TestCalcNextBaseFee_MinimumOneWei(t *testing.T) {
	current := big.NewInt(1)
	gasUsed := uint64(0)
	got := CalcNextBaseFee(current, gasUsed)
	if got.Cmp(big.NewInt(1)) < 0 {
		t.Errorf("minimum base fee failed, got %s", got.String())
	}
}

func TestCalcNextBaseFee_TargetGasUnchanged(t *testing.T) {
	current := big.NewInt(100)
	gasUsed := TargetGas
	got := CalcNextBaseFee(current, gasUsed)
	if got.Cmp(current) != 0 {
		t.Errorf("at target gas base fee should be unchanged: got %s, want %s", got.String(), current.String())
	}
}
