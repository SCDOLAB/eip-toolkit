package main

import (
	"testing"
)

func TestMerkleProofVerify(t *testing.T) {
	leaf1 := Keccak256([]byte("leaf1"))
	leaf2 := Keccak256([]byte("leaf2"))
	leaf3 := Keccak256([]byte("leaf3"))
	leaves := [][32]byte{leaf1, leaf2, leaf3}

	root := BuildMerkleTree(leaves)

	for _, l := range leaves {
		proof, ok := GetProof(leaves, l)
		if !ok {
			t.Fatalf("cannot get proof for leaf %x", l)
		}
		if !VerifyMerkleProof(proof, root, l) {
			t.Errorf("proof should be valid for leaf %x", l)
		}
	}
}

func TestMerkleProof_RejectsFakeLeaf(t *testing.T) {
	leaf1 := Keccak256([]byte("leaf1"))
	leaf2 := Keccak256([]byte("leaf2"))
	leaves := [][32]byte{leaf1, leaf2}
	root := BuildMerkleTree(leaves)

	proof, _ := GetProof(leaves, leaf1)
	fake := Keccak256([]byte("fake-leaf"))
	if VerifyMerkleProof(proof, root, fake) {
		t.Error("fake leaf must not verify")
	}
}

func TestGetProof_MissingLeaf(t *testing.T) {
	leaf1 := Keccak256([]byte("a"))
	leaves := [][32]byte{leaf1}
	missing := Keccak256([]byte("b"))
	if _, ok := GetProof(leaves, missing); ok {
		t.Error("expected not-found for missing leaf")
	}
}

func TestHashPair_IsOrderIndependent(t *testing.T) {
	a := Keccak256([]byte("a"))
	b := Keccak256([]byte("b"))
	ab := HashPair(a, b)
	ba := HashPair(b, a)
	if ab != ba {
		t.Error("HashPair must be order independent (sortPairs)")
	}
}
