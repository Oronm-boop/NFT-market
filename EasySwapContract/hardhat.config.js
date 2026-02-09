require("@nomicfoundation/hardhat-toolbox")
require("@nomiclabs/hardhat-ethers")
require('hardhat-contract-sizer')
require('@openzeppelin/hardhat-upgrades')
require('solidity-coverage')

// config
const { config: dotenvConfig } = require("dotenv")
const { resolve } = require("path")
dotenvConfig({ path: resolve(__dirname, "./.env") })

const SEPOLIA_PK_ONE = process.env.SEPOLIA_PK_ONE
const SEPOLIA_PK_TWO = process.env.SEPOLIA_PK_TWO
if (!SEPOLIA_PK_ONE) {
  throw new Error("Please set at least one private key in a .env file")
}

const MAINNET_PK = process.env.MAINNET_PK
const MAINNET_ALCHEMY_AK = process.env.MAINNET_ALCHEMY_AK

const SEPOLIA_RPC_URL = process.env.SEPOLIA_RPC_URL
const SEPOLIA_ALCHEMY_AK = process.env.SEPOLIA_ALCHEMY_AK

// Sepolia RPC: 优先使用自定义 RPC URL，否则使用 Alchemy
const SEPOLIA_RPC = SEPOLIA_RPC_URL || `https://eth-sepolia.g.alchemy.com/v2/${SEPOLIA_ALCHEMY_AK}`

/** @type import('hardhat/config').HardhatUserConfig */
module.exports = {
  solidity: {
    version: '0.8.20',
    settings: {
      optimizer: {
        enabled: true,
        runs: 50,
      },
      viaIR: true,
    },
    metadata: {
      bytecodeHash: 'none',
    }
  },
  networks: {
    // mainnet 只在配置了私钥时启用
    ...(MAINNET_PK ? {
      mainnet: {
        url: `https://eth-mainnet.g.alchemy.com/v2/${MAINNET_ALCHEMY_AK}`,
        accounts: [MAINNET_PK],
        saveDeployments: true,
        chainId: 1,
      }
    } : {}),
    sepolia: {
      url: SEPOLIA_RPC,
      accounts: [SEPOLIA_PK_ONE, SEPOLIA_PK_TWO].filter(Boolean),
      chainId: 11155111,
    },
    // optimism: {
    //   url: `https://rpc.ankr.com/optimism`,
    //   accounts: [`${MAINNET_PK}`],
    // },
  },
  gasReporter: {
    currency: "USD",
    enabled: process.env.REPORT_GAS ? true : false,
    excludeContracts: [],
    src: "./contracts",
  },
  paths: {
    artifacts: "./artifacts",
    cache: "./cache",
    sources: "./contracts",
    tests: "./test",
  },
}
