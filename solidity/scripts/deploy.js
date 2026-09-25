// Deploy MerkleNFTAllowlist.
// Usage:
//   npx hardhat run scripts/deploy.js              // local hardhat network
//   npx hardhat run scripts/deploy.js --network sepolia
const hre = require("hardhat");

async function main() {
  const [deployer] = await hre.ethers.getSigners();
  console.log("Deploying from:", deployer.address);

  const NFT = await hre.ethers.getContractFactory("MerkleNFTAllowlist");
  const nft = await NFT.deploy(deployer.address);
  await nft.waitForDeployment();

  console.log("MerkleNFTAllowlist deployed to:", await nft.getAddress());
  console.log("Save this address and put it in web/config.js");
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
