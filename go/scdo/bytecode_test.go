package scdo

import (
	"strings"
	"testing"
)

func TestPatchBytecodeForV1_Empty(t *testing.T) {
	result := PatchBytecodeForV1(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %v", result)
	}
}

func TestPatchBytecodeForV1_NoRevert(t *testing.T) {
	// All valid Frontier opcodes, no 0xfd
	input := []byte{0x60, 0x80, 0x60, 0x40, 0x52, 0x00}
	result := PatchBytecodeForV1(input)
	if string(result) != string(input) {
		t.Errorf("expected unchanged, got %v", result)
	}
}

func TestPatchBytecodeForV1_RevertReplaced(t *testing.T) {
	// solc empty-contract constructor snippet: ... 60 00 80 fd 5b ...
	input := []byte{0x60, 0x00, 0x80, 0xfd, 0x5b, 0x50}
	expected := []byte{0x60, 0x00, 0x80, 0x00, 0x5b, 0x50}
	result := PatchBytecodeForV1(input)
	if string(result) != string(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestPatchBytecodeForV1_MultipleReverts(t *testing.T) {
	input := []byte{0xfd, 0x5b, 0xfd, 0x00, 0xfd}
	expected := []byte{0x00, 0x5b, 0x00, 0x00, 0x00}
	result := PatchBytecodeForV1(input)
	if string(result) != string(expected) {
		t.Errorf("expected %v, got %v", expected, result)
	}
}

func TestPatchBytecodeHex(t *testing.T) {
	// Real solc 0.5.16 empty-contract snippet with 0xfd
	hexcode := "0x6080604052348015600080fd5b50"
	result, err := PatchBytecodeHex(hexcode)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result, "fd") == false {
		// after patch, no standalone fd should remain
	}
	// Verify the fd was replaced: original had ...80fd5b..., patched has ...80005b...
	if !strings.Contains(result, "80005b") {
		t.Errorf("expected patched bytecode to contain 80005b, got %s", result)
	}
	if strings.Contains(result, "80fd5b") {
		t.Errorf("patched bytecode still contains 80fd5b: %s", result)
	}
}

func TestPatchBytecodeHex_NoPrefix(t *testing.T) {
	hexcode := "6080604052348015600080fd5b50"
	result, err := PatchBytecodeHex(hexcode)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(result, "0x") {
		t.Errorf("expected 0x prefix, got %s", result)
	}
}

func TestPatchBytecodeHex_OddLength(t *testing.T) {
	_, err := PatchBytecodeHex("0xabc")
	if err == nil {
		t.Error("expected error for odd-length hex")
	}
}

func TestCountRevertOpcodes(t *testing.T) {
	input := []byte{0xfd, 0x5b, 0xfd, 0x00}
	count := CountRevertOpcodes(input)
	if count != 2 {
		t.Errorf("expected 2, got %d", count)
	}
}
