const { expect } = require("chai");
const { ethers } = require("hardhat");
const { MerkleTree } = require("merkletreejs");
const { keccak256, AbiCoder } = require("ethers");

describe("MerkleNFTAllowlist", function () {
  let nft;
  let owner, wlUser1, wlUser2, outsider;
  let merkleTree, merkleRoot;
  const WL_PRICE = 1000000000000000n;
  const PUBLIC_PRICE = 2000000000000000n;
  const MAX_PER_WL = 2;
  const MAX_PER_PUBLIC = 5;

  beforeEach(async function () {
    [owner, wlUser1, wlUser2, outsider] = await ethers.getSigners();
    const allowlist = [wlUser1.address, wlUser2.address];
    const leaves = allowlist.map((addr) =>
      keccak256(AbiCoder.defaultAbiCoder().encode(["address", "uint256"], [addr, MAX_PER_WL]))
    );
    merkleTree = new MerkleTree(leaves, keccak256, { sortPairs: true });
    merkleRoot = merkleTree.getHexRoot();

    const NFT = await ethers.getContractFactory("MerkleNFTAllowlist");
    nft = await NFT.deploy(owner.address);
    await nft.waitForDeployment();
    await nft.setMerkleRoot(merkleRoot);
  });

  function proofFor(addr) {
    const leaf = keccak256(AbiCoder.defaultAbiCoder().encode(["address", "uint256"], [addr, MAX_PER_WL]));
    return merkleTree.getHexProof(leaf);
  }

  it("owner can toggle wl mint", async function () {
    await nft.toggleWlMint();
    expect(await nft.wlMintActive()).to.equal(true);
  });

  it("wl user can mint within limit", async function () {
    await nft.toggleWlMint();
    const proof = proofFor(wlUser1.address);
    await nft.connect(wlUser1).wlMint(2, proof, { value: WL_PRICE * 2n });
    expect(await nft.wlMintedCount(wlUser1.address)).to.equal(2);
    expect(await nft.totalSupply()).to.equal(2n);
  });

  it("revert when exceeding per-wallet wl limit", async function () {
    await nft.toggleWlMint();
    const proof = proofFor(wlUser1.address);
    await nft.connect(wlUser1).wlMint(2, proof, { value: WL_PRICE * 2n });
    await expect(
      nft.connect(wlUser1).wlMint(1, proof, { value: WL_PRICE })
    ).to.be.revertedWith("exceed wl limit");
  });

  it("revert outsider wl mint", async function () {
    await nft.toggleWlMint();
    const proof = proofFor(outsider.address);
    await expect(
      nft.connect(outsider).wlMint(1, proof, { value: WL_PRICE })
    ).to.be.revertedWith("not in allowlist");
  });

  it("revert wl mint when closed", async function () {
    const proof = proofFor(wlUser1.address);
    await expect(
      nft.connect(wlUser1).wlMint(1, proof, { value: WL_PRICE })
    ).to.be.revertedWith("WL mint closed");
  });

  it("revert wl mint with wrong eth", async function () {
    await nft.toggleWlMint();
    const proof = proofFor(wlUser1.address);
    await expect(
      nft.connect(wlUser1).wlMint(1, proof, { value: WL_PRICE / 2n })
    ).to.be.revertedWith("incorrect eth amount");
  });

  it("owner can toggle public mint", async function () {
    await nft.togglePublicMint();
    expect(await nft.publicMintActive()).to.equal(true);
  });

  it("public mint success within limit", async function () {
    await nft.togglePublicMint();
    await nft.connect(outsider).publicMint(3, { value: PUBLIC_PRICE * 3n });
    expect(await nft.publicMintedCount(outsider.address)).to.equal(3);
  });

  it("revert public mint exceeding per-wallet limit", async function () {
    await nft.togglePublicMint();
    await nft.connect(outsider).publicMint(5, { value: PUBLIC_PRICE * 5n });
    await expect(
      nft.connect(outsider).publicMint(1, { value: PUBLIC_PRICE })
    ).to.be.revertedWith("exceed public mint limit");
  });

  it("revert public mint when closed", async function () {
    await expect(
      nft.connect(outsider).publicMint(1, { value: PUBLIC_PRICE })
    ).to.be.revertedWith("Public mint closed");
  });

  it("revert public mint with wrong eth", async function () {
    await nft.togglePublicMint();
    await expect(
      nft.connect(outsider).publicMint(1, { value: PUBLIC_PRICE / 2n })
    ).to.be.revertedWith("incorrect eth for public");
  });

  it("revert wl mint when max supply reached", async function () {
    await nft.toggleWlMint();
    const proof = proofFor(wlUser1.address);
    // Fill 1000 tokens via wlUser1 (max 2 per call, repeat).
    for (let i = 0; i < 500; i++) {
      await nft.connect(wlUser1).wlMint(2, proof, { value: WL_PRICE * 2n });
    }
    // Reset wlMintedCount by using wlUser2 (still in allowlist).
    const proof2 = proofFor(wlUser2.address);
    await expect(
      nft.connect(wlUser2).wlMint(1, proof2, { value: WL_PRICE })
    ).to.be.revertedWith("max supply reached");
  });

  it("revert public mint when max supply reached", async function () {
    await nft.togglePublicMint();
    for (let i = 0; i < 200; i++) {
      await nft.connect(outsider).publicMint(5, { value: PUBLIC_PRICE * 5n });
    }
    await expect(
      nft.connect(outsider).publicMint(1, { value: PUBLIC_PRICE })
    ).to.be.revertedWith("max supply reached");
  });
});
