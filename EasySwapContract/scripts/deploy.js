/**
 * EasySwap 合约部署脚本
 * 
 * 功能：部署 EasySwapVault 和 EasySwapOrderBook 可升级合约，并完成初始配置
 * 
 * 部署流程：
 *   1. 部署 EasySwapVault (金库合约) - 负责托管 NFT 和 ETH
 *   2. 部署 EasySwapOrderBook (订单簿合约) - 负责订单管理和撮合
 *   3. 配置 Vault.setOrderBook() - 关联订单簿合约地址
 * 
 * 使用方法：
 *   npx hardhat run --network sepolia scripts/deploy.js
 */

const { ethers, upgrades } = require("hardhat")

/**
 * 历史部署记录
 * 
 * 2026/02/09 Sepolia 测试网部署:
 *   esVault Proxy: 0x38FfF9035b68452507566612445BFf218e83D2d1
 *   esVault Implementation: 0x4a3dcf4905cBC596270a339Ce6625f567da0A80E
 *   esDex Proxy: 0xDf4c2715AeB20bAe0490b0e4642C7C838c2E0090
 *   esDex Implementation: 0x14F6e788dAb429EeE3474d307Eda1B03650822ab
 *   ProxyAdmin: 0xEe72CA76455dAdf967306da7d214B1F1520F1a40
 * 
 * 2025/02/15 Sepolia 测试网部署:
 *   esVault Proxy: 0xaD65f3dEac0Fa9Af4eeDC96E95574AEaba6A2834
 *   esVault Implementation: 0x5D034EA7F15429Bcb9dFCBE08Ee493F001063AF0
 *   esDex Proxy: 0xcEE5AA84032D4a53a0F9d2c33F36701c3eAD5895
 *   esDex Implementation: 0x17B2d83BFE9089cd1D676dE8aebaDCA561f55c96
 *   ProxyAdmin: 0xe839419C14188F7b79a0E4C09cFaF612398e7795
 */

async function main() {
  // ========== 1. 获取部署账户 ==========
  const [deployer] = await ethers.getSigners()
  console.log("deployer: ", deployer.address)

  // ========== 2. 部署 EasySwapVault (金库合约) ==========
  // 金库合约负责托管用户的 NFT 和 ETH 资产
  // - List 订单：NFT 存入金库
  // - Bid 订单：ETH 存入金库
  let esVault = await ethers.getContractFactory("EasySwapVault")
  esVault = await upgrades.deployProxy(esVault, {
    initializer: 'initialize'  // 调用 initialize() 初始化函数
  });
  await esVault.deployed()

  // 打印 Vault 合约地址
  console.log("esVault contract deployed to:", esVault.address)
  console.log(await upgrades.erc1967.getImplementationAddress(esVault.address), " esVault getImplementationAddress")
  console.log(await upgrades.erc1967.getAdminAddress(esVault.address), " esVault getAdminAddress")

  // ========== 3. 部署 EasySwapOrderBook (订单簿合约) ==========
  // 订单簿合约负责订单的创建、取消、编辑和撮合

  // 初始化参数
  const newProtocolShare = 200;              // 协议费比例：200/10000 = 2%
  const newESVault = esVault.address;        // 金库合约地址
  const EIP712Name = "EasySwapOrderBook";    // EIP-712 签名域名（用于链下签名验证）
  const EIP712Version = "1";                 // EIP-712 签名版本

  let esDex = await ethers.getContractFactory("EasySwapOrderBook")
  esDex = await upgrades.deployProxy(
    esDex,
    [newProtocolShare, newESVault, EIP712Name, EIP712Version],  // 初始化参数
    {
      initializer: 'initialize',
      // 允许使用 immutable 变量（self 变量用于 delegatecall 检查）
      unsafeAllow: ['state-variable-immutable', 'state-variable-assignment']
    }
  );
  await esDex.deployed()

  // 打印 OrderBook 合约地址
  console.log("esDex contract deployed to:", esDex.address)
  console.log(await upgrades.erc1967.getImplementationAddress(esDex.address), " esDex getImplementationAddress")
  console.log(await upgrades.erc1967.getAdminAddress(esDex.address), " esDex getAdminAddress")

  // ========== 4. 配置 Vault → OrderBook 关联 ==========
  // 设置 Vault 的 orderBook 地址，只有 OrderBook 合约才能操作 Vault 中的资产
  const esDexAddress = esDex.address
  const esVaultAddress = esVault.address

  // 获取 Vault 合约实例
  const esVault_ = await (
    await ethers.getContractFactory("EasySwapVault")
  ).attach(esVaultAddress)

  // 调用 setOrderBook 设置关联
  const tx = await esVault_.setOrderBook(esDexAddress)
  await tx.wait()  // 等待交易确认
  console.log("esVault setOrderBook tx:", tx.hash)

  console.log("\n========== 部署完成 ==========")
  console.log("Vault Proxy:", esVaultAddress)
  console.log("OrderBook Proxy:", esDexAddress)
}

// 执行部署脚本
// 使用 async/await 模式处理异步操作和错误
main()
  .then(() => process.exit(0))  // 成功退出
  .catch((error) => {
    console.error(error)        // 打印错误信息
    process.exit(1)             // 失败退出
  })
