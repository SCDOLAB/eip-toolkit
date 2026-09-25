// Deploy bridge contracts to SCDO.
// Usage: npx hardhat run scripts/deploy-bridge.js --network scdo
const hre = require("hardhat");

async function main() {
  const [deployer] = await hre.ethers.getSigners();
  console.log("Deploying bridge from:", deployer.address);

  // 1. BridgeLock (Ethereum-side lock contract) — also deployed on SCDO for testing
  const Lock = await hre.ethers.getContractFactory("BridgeLock");
  const lock = await Lock.deploy(deployer.address, deployer.address);
  await lock.waitForDeployment();
  console.log("BridgeLock deployed to:", await lock.getAddress());

  // 2. BridgeMint (pegged ERC20)
  const Mint = await hre.ethers.getContractFactory("BridgeMint");
  const mint = await Mint.deploy("Peg USDC", "pUSDC", deployer.address, deployer.address);
  await mint.waitForDeployment();
  console.log("BridgeMint deployed to:", await mint.getAddress());

  console.log("\nNext:");
  console.log("  1. Put BridgeLock address in Ethereum relayer config");
  console.log("  2. Put BridgeMint address in SCDO relayer config");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
