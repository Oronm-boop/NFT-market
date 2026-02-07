# NFT Marketplace 项目学习指南

这是一个典型的 **全栈去中心化 NFT 交易市场** 项目，采用 **链下订单簿 + 链上结算** 架构。类似于 OpenSea 或 LooksRare 的早期架构，它不仅包含智能合约，还包含完整的后端服务来处理数据索引、API 聚合和前端展示。

## 1. 项目架构概览

本项目是一个 **Monorepo（单体仓库）**，包含 5 个主要模块：

| 目录 | 角色 | 技术栈 | 说明 |
| :--- | :--- | :--- | :--- |
| **`EasySwapContract`** | **核心逻辑** | Solidity, Hardhat | 智能合约。处理 NFT 的挂单、买卖、撮合逻辑。这是项目的"心脏"。 |
| **`EasySwapSync`** | **数据索引** | Go | 监听链上事件（Mint, Transfer, Sale），将数据同步到数据库。这是"爬虫/索引器"。 |
| **`EasySwapBackend`** | **API 服务** | Go | 为前端提供高性能的查询接口（如：获取某个集合的地板价、交易历史）。 |
| **`EasySwapBase`** | **基础设施** | Go | 公共库。包含后端和同步服务共用的工具（如日志、链交互封装、错误码）。 |
| **`nft-market-fe`** | **用户界面** | Next.js, TS, Tailwind | 前端页面。用户连接钱包、查看 NFT、挂单买卖的地方。 |

## 2. 核心业务逻辑（面试/学习重点）

这个项目最核心的设计是 **订单簿（OrderBook）模型**，这与 Uniswap 这种 AMM 模型不同。

*   **挂单（Maker）**：用户签名一个订单（包含价格、过期时间等），**不上链**（省 Gas），只存在后端数据库中。
*   **吃单（Taker）**：买家看到订单后，调用智能合约，传入卖家的签名和订单信息，**链上成交**。

## 3. 推荐学习路线

建议按照 **数据流向** 从底层到上层进行学习：

### 第一阶段：智能合约 (EasySwapContract)
*这是资产安全的基石，也是 Web3 项目的起点。*
1.  **阅读 `contracts/` 目录**：重点看 `OrderBookExchange`（交易核心）和 `OrderVault`（资产托管/授权）。
2.  **理解 EIP-712 签名**：这是链下挂单的核心，重点理解合约如何验证用户的链下签名。
3.  **运行测试**：在 `test/` 目录下运行测试脚本，理解完整的交易流程（Approve -> List -> Buy）。

### 第二阶段：数据同步 (EasySwapSync)
*理解链上数据如何变为链下数据。*
1.  看它如何监听区块链事件（Event Logs）。
2.  看 `model/` 目录下的数据库表结构（Collection, Item, Order, Activity），理解业务实体关系。
3.  **难点**：理解如何处理 **区块回滚（Reorg）** 的情况（这是索引器最难的部分）。

### 第三阶段：后端 API (EasySwapBackend)
*理解如何高效提供数据。*
1.  看接口定义：如何聚合数据（例如：计算 Collection 的地板价、总交易量）。
2.  学习它是如何与数据库交互的。

### 第四阶段：前端交互 (nft-market-fe)
*将所有东西串联起来。*
1.  看 `hooks/`：如何连接钱包（MetaMask/Wagmi）。
2.  看 `components/`：如何调用后端 API 展示数据，以及如何调用合约进行 `buy` 操作。

## 4. 快速启动建议

如果你想先跑起来看看：

1.  **环境准备**：你需要 Node.js, Go (1.18+), MySQL, Redis。
2.  **合约部署**：进入 `EasySwapContract`，编译并部署到测试网（如 Sepolia 或本地 Hardhat Network）。
3.  **数据库初始化**：根据 `README.md` 或 `model` 目录下的 SQL 建表。
4.  **启动后端**：配置好数据库连接，先启动 `EasySwapSync` 开始同步数据，再启动 `EasySwapBackend` 提供接口。
5.  **启动前端**：进入 `nft-market-fe`，运行 `npm run dev` 启动前端，连接你的本地后端。
