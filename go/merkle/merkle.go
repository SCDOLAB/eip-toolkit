// Package main implements a Merkle tree and proof verifier compatible with
// OpenZeppelin's MerkleProof.sol: leaves are hashed with keccak256, sibling
// pairs are sorted before being hashed together (sortPairs=true).
package main

import (
	"fmt"

	"golang.org/x/crypto/sha3"
)

// Keccak256 returns the Ethereum keccak256 digest of data.
func Keccak256(data []byte) [32]byte {
	h := sha3.NewLegacyKeccak256()
	h.Write(data)
	var res [32]byte
	copy(res[:], h.Sum(nil))
	return res
}

// HashPair hashes two child nodes, sorting them first to match the
// OpenZeppelin "sortPairs" behaviour.
func HashPair(a, b [32]byte) [32]byte {
	if string(a[:]) > string(b[:]) {
		a, b = b, a
	}
	buf := append(a[:], b[:]...)
	return Keccak256(buf)
}

// BuildMerkleTree returns the root of a Merkle tree built from leaves.
// Odd trailing leaves are paired with themselves (hashed twice), matching
// merkletreejs defaults.
func BuildMerkleTree(leaves [][32]byte) [32]byte {
	tree := make([][32]byte, len(leaves))
	copy(tree, leaves)
	for len(tree) > 1 {
		var next [][32]byte
		for i := 0; i < len(tree); i += 2 {
			if i+1 < len(tree) {
				next = append(next, HashPair(tree[i], tree[i+1]))
			} else {
				next = append(next, HashPair(tree[i], tree[i]))
			}
		}
		tree = next
	}
	return tree[0]
}

// GetProof returns the Merkle proof for targetLeaf. The boolean is false if
// the leaf is not present.
func GetProof(leaves [][32]byte, targetLeaf [32]byte) ([][32]byte, bool) {
	idx := -1
	for i, l := range leaves {
		if l == targetLeaf {
			idx = i
			break
		}
	}
	if idx == -1 {
		return nil, false
	}
	tree := make([][32]byte, len(leaves))
	copy(tree, leaves)
	curIdx := idx
	var proof [][32]byte
	for len(tree) > 1 {
		var next [][32]byte
		for i := 0; i < len(tree); i += 2 {
			left := tree[i]
			right := tree[i]
			if i+1 < len(tree) {
				right = tree[i+1]
			}
			parent := HashPair(left, right)
			next = append(next, parent)
			if curIdx == i {
				proof = append(proof, right)
			} else if curIdx == i+1 {
				proof = append(proof, left)
			}
		}
		curIdx /= 2
		tree = next
	}
	return proof, true
}

// VerifyMerkleProof is equivalent to OpenZeppelin's MerkleProof.verify.
func VerifyMerkleProof(proof [][32]byte, root, leaf [32]byte) bool {
	current := leaf
	for _, p := range proof {
		current = HashPair(current, p)
	}
	return current == root
}

func main() {
	leaf1 := Keccak256([]byte("leaf1"))
	leaf2 := Keccak256([]byte("leaf2"))
	leaves := [][32]byte{leaf1, leaf2}
	root := BuildMerkleTree(leaves)
	proof, _ := GetProof(leaves, leaf1)
	fmt.Printf("root=%x valid=%v\n", root, VerifyMerkleProof(proof, root, leaf1))
}
