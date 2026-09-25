require("@nomicfoundation/hardhat-toolbox");
require("dotenv").config();

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    version: "0.8.26",
    settings: {
      evmVersion: "cancun",
    },
  },
  paths: {
    sources: "./contracts",
    tests: "./test",
    cache: "./cache",
    artifacts: "./artifacts",
  },
  networks: {
    sepolia: {
      url: process.env.SEPOLIA_RPC_URL || "https://ethereum-sepolia-rpc.publicnode.com",
      chainId: 11155111,
      accounts: process.env.DEPLOYER_PRIVATE_KEY ? [process.env.DEPLOYER_PRIVATE_KEY] : [],
    },
    scdo: {
      url: process.env.SCDO_RPC_URL || "http://192.168.50.50:8037",
      chainId: Number(process.env.SCDO_CHAIN_ID || 0),
      accounts: process.env.SCDO_DEPLOYER_PRIVATE_KEY ? [process.env.SCDO_DEPLOYER_PRIVATE_KEY] : [],
    },
  },
};
