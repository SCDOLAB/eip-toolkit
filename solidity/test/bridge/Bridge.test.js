const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("BridgeLock", function () {
  let lock, token, owner, admin, user, relayer;
  const FEE_BPS = 10n; // 0.1%

  beforeEach(async function () {
    [owner, admin, user, relayer] = await ethers.getSigners();

    const ERC20 = await ethers.getContractFactory("ERC20Mock");
    token = await ERC20.deploy("Test", "TST", ethers.parseEther("1000000"));
    await token.transfer(user.address, ethers.parseEther("1000"));

    const Lock = await ethers.getContractFactory("BridgeLock");
    lock = await Lock.deploy(owner.address, admin.address);

    await token.connect(user).approve(await lock.getAddress(), ethers.parseEther("1000"));
  });

  it("locks tokens and emits Deposited with fee deducted", async function () {
    const amount = ethers.parseEther("100");
    await expect(lock.connect(user).deposit(await token.getAddress(), amount, 11155111))
      .to.emit(lock, "Deposited")
      .withArgs(await token.getAddress(), user.address, amount - (amount * FEE_BPS) / 10000n, 0n, 11155111);
  });

  it("rejects deposit of zero", async function () {
    await expect(lock.connect(user).deposit(await token.getAddress(), 0, 11155111)).to.be.revertedWith("amount zero");
  });

  it("withdraws with valid admin signature", async function () {
    const amount = ethers.parseEther("50");
    await lock.connect(user).deposit(await token.getAddress(), amount, 11155111);

    // Admin signs (token, to, amount, nonce, lock)
    const nonce = 0n;
    const digest = ethers.solidityPackedKeccak256(
      ["address", "address", "uint256", "uint256", "address"],
      [await token.getAddress(), user.address, amount, nonce, await lock.getAddress()]
    );
    const sig = await admin.signMessage(ethers.getBytes(digest));

    await expect(lock.withdraw(await token.getAddress(), user.address, amount, nonce, sig))
      .to.emit(lock, "Withdrawn");
  });

  it("rejects withdraw with bad signature", async function () {
    const nonce = 0n;
    const digest = ethers.solidityPackedKeccak256(
      ["address", "address", "uint256", "uint256", "address"],
      [await token.getAddress(), user.address, ethers.parseEther("1"), nonce, await lock.getAddress()]
    );
    const sig = await user.signMessage(ethers.getBytes(digest)); // wrong signer
    await expect(
      lock.withdraw(await token.getAddress(), user.address, ethers.parseEther("1"), nonce, sig)
    ).to.be.revertedWith("bad signature");
  });
});

describe("BridgeMint", function () {
  let mint, owner, relayer, user;

  beforeEach(async function () {
    [owner, relayer, user] = await ethers.getSigners();
    const Mint = await ethers.getContractFactory("BridgeMint");
    mint = await Mint.deploy("Peg USDC", "pUSDC", owner.address, relayer.address);
  });

  it("mints only by relayer and marks nonce used", async function () {
    await expect(mint.connect(relayer).mint(user.address, ethers.parseEther("100"), 42n))
      .to.emit(mint, "Minted");
    expect(await mint.balanceOf(user.address)).to.equal(ethers.parseEther("100"));
    await expect(
      mint.connect(relayer).mint(user.address, ethers.parseEther("1"), 42n)
    ).to.be.revertedWith("nonce already used");
  });

  it("rejects mint by non-relayer", async function () {
    await expect(mint.connect(user).mint(user.address, 1, 1n)).to.be.revertedWith("not minter");
  });

  it("burn emits Burned for relayer to watch", async function () {
    await mint.connect(relayer).mint(user.address, ethers.parseEther("10"), 1n);
    await expect(mint.connect(user).burn(ethers.parseEther("3"), 99n)).to.emit(mint, "Burned");
  });
});
