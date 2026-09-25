// Generates a Merkle root and proof for an allowlist.
// Usage: node scripts/merkle-gen.js
const { MerkleTree } = require("merkletreejs");
const { keccak256, AbiCoder } = require("ethers");

const allowlist = [
  "0x1111111111111111111111111111111111111111",
  "0x2222222222222222222222222222222222222222",
  "0x3333333333333333333333333333333333333333",
];
const MAX_PER_WL = 2;

const leaves = allowlist.map((addr) =>
  keccak256(AbiCoder.defaultAbiCoder().encode(["address", "uint256"], [addr, MAX_PER_WL]))
);
const tree = new MerkleTree(leaves, keccak256, { sortPairs: true });

console.log("merkle root:", tree.getHexRoot());

const target = allowlist[0];
const targetLeaf = keccak256(
  AbiCoder.defaultAbiCoder().encode(["address", "uint256"], [target, MAX_PER_WL])
);
console.log("proof for", target, ":", tree.getHexProof(targetLeaf));
