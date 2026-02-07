# NFT Marketplace 完整架构解析

本文档详细解释前端、后端、智能合约三层各自承担的角色，以及它们之间的交互方式。

---

## 目录

1. [架构总览](#1-架构总览)
2. [智能合约层 (EasySwapContract)](#2-智能合约层-easyswapcontract)
3. [后端服务层 (EasySwapBackend + EasySwapSync)](#3-后端服务层-easyswapbackend--easyswapsync)
4. [前端应用层 (nft-market-fe)](#4-前端应用层-nft-market-fe)
5. [核心业务流程详解](#5-核心业务流程详解)
6. [数据流向图](#6-数据流向图)

---

## 1. 架构总览

这是一个典型的 **链上订单簿（On-chain OrderBook）** NFT 交易市场，采用 **三层架构**：

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              用户 (浏览器)                                   │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │
                    ┌──────────────┴──────────────┐
                    │                             │
                    ▼                             ▼
┌───────────────────────────────┐   ┌───────────────────────────────────────┐
│     前端 (nft-market-fe)      │   │      钱包 (MetaMask/RainbowKit)        │
│  • 展示 NFT 数据              │   │  • 签名交易                            │
│  • 调用后端 API 查询          │   │  • 发送交易到链上                      │
│  • 构造合约交易               │   │                                       │
└───────────────┬───────────────┘   └───────────────┬───────────────────────┘
                │ HTTP API                          │ JSON-RPC
                ▼                                   ▼
┌───────────────────────────────┐   ┌───────────────────────────────────────┐
│   后端 API (EasySwapBackend)  │   │        区块链 (Ethereum/Sepolia)       │
│  • 提供高性能查询接口          │   │  ┌─────────────────────────────────┐   │
│  • 聚合链上数据               │   │  │   EasySwapOrderBook 合约        │   │
│  • 计算地板价、排行榜等        │   │  │   • 创建/取消/匹配订单          │   │
└───────────────▲───────────────┘   │  │   • 发出 LogMake/Match/Cancel   │   │
                │ 读取               │  └─────────────────────────────────┘   │
                │                    │  ┌─────────────────────────────────┐   │
┌───────────────┴───────────────┐   │  │   EasySwapVault 合约            │   │
│         MySQL + Redis         │   │  │   • 托管 NFT 和 ETH             │   │
│  • Collection / Item 表       │   │  └─────────────────────────────────┘   │
│  • Order / Activity 表        │   └───────────────────────────────────────┘
└───────────────▲───────────────┘                   │
                │ 写入                              │ 监听事件
                │                                   │
┌───────────────┴───────────────────────────────────┴───────────────────────┐
│                        索引服务 (EasySwapSync)                             │
│  • 轮询区块链获取最新区块                                                   │
│  • 解析 LogMake/LogMatch/LogCancel 事件                                    │
│  • 将链上数据结构化存入数据库                                               │
│  • 维护聚合数据（地板价、交易量）                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### 各层核心职责速览

| 层级 | 模块 | 核心职责 | 技术栈 |
|:---|:---|:---|:---|
| **合约层** | EasySwapOrderBook | 订单创建、匹配、取消的核心逻辑 | Solidity, Hardhat |
| | EasySwapVault | 托管用户的 NFT 和 ETH | Solidity |
| **后端层** | EasySwapSync | 监听链上事件，同步数据到数据库 | Go, GORM |
| | EasySwapBackend | 为前端提供 REST API | Go, Gin |
| | EasySwapBase | 公共库（日志、链交互封装等） | Go |
| **前端层** | nft-market-fe | 用户界面、钱包交互、合约调用 | Next.js, wagmi, ethers.js |

---

## 2. 智能合约层 (EasySwapContract)

### 2.1 合约层的角色

**合约层是整个系统的"心脏"和"信任锚点"**。它负责：
- **资产安全**：托管用户的 NFT 和 ETH
- **交易逻辑**：验证订单、匹配买卖双方、结算资金
- **事件广播**：发出事件供链下服务索引

> **关键特性**：所有涉及资产转移的操作都必须通过合约完成，后端和前端无法直接操作用户资产。

### 2.2 合约组成

```
EasySwapContract/
├── contracts/
│   ├── EasySwapOrderBook.sol   # 核心：订单簿交易逻辑
│   ├── EasySwapVault.sol       # 金库：资产托管
│   ├── OrderStorage.sol        # 订单存储（红黑树）
│   ├── OrderValidator.sol      # 订单验证
│   ├── ProtocolManager.sol     # 协议费管理
│   └── libraries/
│       ├── LibOrder.sol        # 订单数据结构
│       ├── LibPayInfo.sol      # 支付信息
│       └── RedBlackTreeLibrary.sol  # 红黑树（价格排序）
```

### 2.3 核心合约详解

#### 2.3.1 EasySwapOrderBook（订单簿合约）

**职责**：实现完整的订单簿交易逻辑。

**支持的订单类型**：

| 订单类型 | Side | SaleKind | 说明 |
|:---|:---|:---|:---|
| **Listing（挂单）** | List (0) | FixedPriceForItem (1) | 卖家挂出 NFT，等待买家购买 |
| **Item Bid（单品出价）** | Bid (1) | FixedPriceForItem (1) | 买家对特定 NFT 出价 |
| **Collection Bid（集合出价）** | Bid (1) | FixedPriceForCollection (0) | 买家对整个集合出价，任意 NFT 可接受 |

**核心函数**：

```solidity
// 创建订单（挂单或出价）
function makeOrders(LibOrder.Order[] calldata newOrders) 
    external payable returns (OrderKey[] memory newOrderKeys);

// 取消订单
function cancelOrders(OrderKey[] calldata orderKeys) 
    external returns (bool[] memory successes);

// 编辑订单（修改价格）
function editOrders(LibOrder.EditDetail[] calldata editDetails) 
    external payable returns (OrderKey[] memory newOrderKeys);

// 匹配订单（撮合交易）
function matchOrder(LibOrder.Order calldata sellOrder, LibOrder.Order calldata buyOrder) 
    external payable;

// 批量匹配订单
function matchOrders(LibOrder.MatchDetail[] calldata matchDetails) 
    external payable returns (bool[] memory successes);
```

**发出的事件**：

```solidity
// 订单创建事件 - 后端索引挂单信息
event LogMake(
    OrderKey orderKey,           // 订单唯一标识（hash）
    LibOrder.Side indexed side,  // List 或 Bid
    LibOrder.SaleKind indexed saleKind,
    address indexed maker,       // 订单创建者
    LibOrder.Asset nft,          // NFT 信息（collection, tokenId, amount）
    Price price,                 // 价格
    uint64 expiry,               // 过期时间
    uint64 salt                  // 随机盐值
);

// 订单取消事件
event LogCancel(OrderKey indexed orderKey, address indexed maker);

// 订单匹配事件 - 后端索引成交信息
event LogMatch(
    OrderKey indexed makeOrderKey,
    OrderKey indexed takeOrderKey,
    LibOrder.Order makeOrder,
    LibOrder.Order takeOrder,
    uint128 fillPrice            // 成交价格
);
```

#### 2.3.2 EasySwapVault（金库合约）

**职责**：安全托管用户的 NFT 和 ETH 资产。

**为什么需要金库？**
- **卖家挂单**：NFT 需要锁定在金库中，防止卖家在挂单后转移 NFT
- **买家出价**：ETH 需要锁定在金库中，确保买家有足够资金

**核心函数**：

```solidity
// ETH 操作
function depositETH(OrderKey orderKey, uint256 ETHAmount) external payable;
function withdrawETH(OrderKey orderKey, uint256 ETHAmount, address to) external;

// NFT 操作
function depositNFT(OrderKey orderKey, address from, address collection, uint256 tokenId) external;
function withdrawNFT(OrderKey orderKey, address to, address collection, uint256 tokenId) external;

// 订单编辑时的资产迁移
function editETH(...) external payable;
function editNFT(...) external;
```

**安全机制**：
```solidity
modifier onlyEasySwapOrderBook() {
    require(msg.sender == orderBook, "HV: only EasySwap OrderBook");
    _;
}
```
只有 OrderBook 合约可以调用金库的存取函数，防止资产被任意转出。

### 2.4 订单生命周期

```
                    ┌─────────────┐
                    │   用户创建   │
                    │    订单     │
                    └──────┬──────┘
                           │
                           ▼
              ┌────────────────────────┐
              │  makeOrders() 调用     │
              │  1. 验证订单参数        │
              │  2. 资产转入金库        │
              │     - List: NFT → Vault │
              │     - Bid: ETH → Vault  │
              │  3. 存储订单到红黑树    │
              │  4. 发出 LogMake 事件   │
              └──────────┬─────────────┘
                         │
          ┌──────────────┼──────────────┐
          │              │              │
          ▼              ▼              ▼
    ┌──────────┐   ┌──────────┐   ┌──────────┐
    │  被匹配   │   │  被取消   │   │   过期    │
    │ (Match)  │   │ (Cancel) │   │ (Expire) │
    └────┬─────┘   └────┬─────┘   └────┬─────┘
         │              │              │
         ▼              ▼              ▼
┌─────────────┐  ┌─────────────┐  ┌─────────────┐
│matchOrder() │  │cancelOrders()│ │ (链下处理)  │
│1. 验证匹配  │  │1. 验证权限   │  │后端标记过期 │
│2. 资产流转  │  │2. 资产退回   │  │             │
│  NFT→买家   │  │  NFT→卖家    │  │             │
│  ETH→卖家   │  │  ETH→买家    │  │             │
│3. 扣协议费  │  │3. LogCancel │  │             │
│4. LogMatch  │  │              │  │             │
└─────────────┘  └─────────────┘  └─────────────┘
```

---

## 3. 后端服务层 (EasySwapBackend + EasySwapSync)

### 3.1 后端层的角色

**后端层是"链上世界"和"用户界面"之间的桥梁**。它的存在是因为：

1. **区块链查询低效**：直接从链上查询"某个集合的所有 NFT"需要遍历所有区块，非常慢
2. **聚合计算复杂**：地板价、交易量、排行榜等需要复杂计算，链上无法高效完成
3. **用户体验**：前端需要毫秒级响应，而链上查询可能需要几秒

> **重要**：后端**只读取**链上数据，**不执行**任何链上写操作。所有交易都是前端直接与合约交互。

### 3.2 EasySwapSync（索引服务）

**职责**：监听区块链事件，将链上数据同步到数据库。

```
EasySwapSync/
├── cmd/                    # 命令入口
├── service/
│   ├── orderbookindexer/   # 核心：订单簿事件索引
│   │   └── service.go      # 事件监听和处理逻辑
│   ├── collectionfilter/   # Collection 过滤器
│   └── config/             # 配置
├── model/                  # 数据模型
└── main.go
```

#### 3.2.1 事件监听循环

```go
// 核心同步循环 (简化版)
func (s *Service) SyncOrderBookEventLoop() {
    for {
        currentBlockNum := s.chainClient.BlockNumber()
        
        // 等待 8 个区块确认（防止分叉）
        if lastSyncBlock > currentBlockNum - 8 {
            time.Sleep(10 * time.Second)
            continue
        }
        
        // 批量获取事件日志
        logs := s.chainClient.FilterLogs(FilterQuery{
            FromBlock: lastSyncBlock,
            ToBlock:   lastSyncBlock + 100,
            Addresses: []string{dexContractAddress},
        })
        
        // 根据事件类型分发处理
        for _, log := range logs {
            switch log.Topics[0] {
            case LogMakeTopic:
                s.handleMakeEvent(log)    // 处理挂单事件
            case LogCancelTopic:
                s.handleCancelEvent(log)  // 处理取消事件
            case LogMatchTopic:
                s.handleMatchEvent(log)   // 处理成交事件
            }
        }
        
        lastSyncBlock = endBlock + 1
    }
}
```

#### 3.2.2 事件处理逻辑

**handleMakeEvent（挂单事件）**：
```go
func (s *Service) handleMakeEvent(log ethereumTypes.Log) {
    // 1. 解析事件数据
    event := parseLogMakeEvent(log)
    
    // 2. 写入订单表 (ob_order_sepolia)
    newOrder := Order{
        OrderID:           event.OrderKey,
        OrderType:         determineOrderType(event.Side, event.SaleKind),
        OrderStatus:       Active,
        Price:             event.Price,
        Maker:             event.Maker,
        CollectionAddress: event.Nft.CollectionAddr,
        TokenId:           event.Nft.TokenId,
        ExpireTime:        event.Expiry,
    }
    db.Create(&newOrder)
    
    // 3. 写入/更新 Item 表 (ob_item_sepolia)
    db.UpdateOrCreate(&Item{
        CollectionAddress: event.Nft.CollectionAddr,
        TokenId:           event.Nft.TokenId,
        ListPrice:         event.Price,
        ListTime:          time.Now().Unix(),
    })
    
    // 4. 写入活动表 (ob_activity_sepolia)
    db.Create(&Activity{
        ActivityType:      Listing,
        Maker:             event.Maker,
        CollectionAddress: event.Nft.CollectionAddr,
        TokenId:           event.Nft.TokenId,
        Price:             event.Price,
        TxHash:            log.TxHash,
        BlockNumber:       log.BlockNumber,
    })
    
    // 5. 更新 Collection 的地板价
    s.updateCollectionFloorPrice(event.Nft.CollectionAddr)
}
```

**handleMatchEvent（成交事件）**：
```go
func (s *Service) handleMatchEvent(log ethereumTypes.Log) {
    event := parseLogMatchEvent(log)
    
    // 1. 更新订单状态为已成交
    db.Update(&Order{OrderID: event.MakeOrderKey}, map[string]interface{}{
        "order_status": Filled,
        "taker":        event.TakeOrder.Maker,
    })
    
    // 2. 更新 NFT 所有者
    db.Update(&Item{
        CollectionAddress: event.Collection,
        TokenId:           event.TokenId,
    }, map[string]interface{}{
        "owner": newOwner,
    })
    
    // 3. 记录成交活动
    db.Create(&Activity{
        ActivityType: Sale,
        Maker:        seller,
        Taker:        buyer,
        Price:        event.FillPrice,
        TxHash:       log.TxHash,
    })
    
    // 4. 更新 Collection 交易量
    s.updateCollectionVolume(event.Collection, event.FillPrice)
}
```

#### 3.2.3 数据库表结构

| 表名 | 用途 | 主要字段 |
|:---|:---|:---|
| `ob_collection_sepolia` | NFT 集合信息 | address, name, floor_price, volume_total, item_amount |
| `ob_item_sepolia` | 单个 NFT 信息 | collection_address, token_id, owner, list_price |
| `ob_order_sepolia` | 订单信息 | order_id, order_type, order_status, price, maker |
| `ob_activity_sepolia` | 交易历史 | activity_type, maker, taker, price, tx_hash |
| `ob_item_external_sepolia` | NFT 元数据 | meta_data_uri, image_uri |

### 3.3 EasySwapBackend（API 服务）

**职责**：为前端提供 REST API 接口。

```
EasySwapBackend/
├── src/
│   ├── api/
│   │   ├── router/         # 路由定义
│   │   │   └── v1.go       # API 路由
│   │   ├── v1/             # 接口处理函数
│   │   │   ├── collection.go
│   │   │   ├── order.go
│   │   │   └── activity.go
│   │   └── middleware/     # 中间件
│   │       ├── auth.go     # 认证
│   │       └── cacheapi.go # 缓存
│   ├── service/            # 业务逻辑
│   ├── dao/                # 数据访问
│   └── types/              # 类型定义
```

#### 3.3.1 核心 API 接口

| 模块 | 接口 | 说明 |
|:---|:---|:---|
| **Collection** | `GET /collections/ranking` | 热门集合排行榜（缓存 60s） |
| | `GET /collections/:address` | 集合详情 |
| | `GET /collections/:address/items` | 集合下的 NFT 列表 |
| | `GET /collections/:address/bids` | 集合的出价列表 |
| **Item** | `GET /collections/:address/:token_id` | NFT 详情 |
| | `GET /collections/:address/:token_id/bids` | NFT 的出价列表 |
| | `GET /collections/:address/:token_id/listing` | NFT 的挂单信息 |
| **Activity** | `GET /activities` | 交易历史 |
| **Portfolio** | `GET /portfolio/items` | 用户持有的 NFT |
| | `GET /portfolio/listings` | 用户的挂单 |
| | `GET /portfolio/bids` | 用户的出价 |
| **Order** | `GET /bid-orders` | 订单列表 |

#### 3.3.2 API 响应示例

```json
// GET /collections/0x1234.../items
{
  "code": 0,
  "data": {
    "items": [
      {
        "token_id": "1",
        "name": "CryptoPunk #1",
        "owner": "0xabcd...",
        "list_price": "1000000000000000000",  // 1 ETH in wei
        "image_uri": "https://..."
      }
    ],
    "total": 100,
    "page": 1
  }
}
```

---

## 4. 前端应用层 (nft-market-fe)

### 4.1 前端层的角色

**前端是用户与系统交互的唯一入口**。它负责：
- **展示数据**：从后端 API 获取并渲染 NFT、订单、交易历史
- **钱包交互**：连接钱包、获取用户地址和余额
- **构造交易**：根据用户操作构造合约调用参数
- **发送交易**：通过钱包将交易发送到区块链

### 4.2 技术栈

```json
{
  "dependencies": {
    "@rainbow-me/rainbowkit": "^2.2.10",  // 钱包连接 UI
    "wagmi": "^2.19.5",                   // React Hooks for Ethereum
    "viem": "^2.41.2",                    // 底层 Ethereum 交互
    "ethers": "^6.16.0",                  // 合约交互
    "@tanstack/react-query": "^5.90.12",  // 数据获取和缓存
    "axios": "^1.13.2",                   // HTTP 请求
    "next": "16.0.10",                    // React 框架
    "tailwindcss": "^3.4.18"              // UI 样式
  }
}
```

### 4.3 项目结构

```
nft-market-fe/
├── app/                    # Next.js App Router 页面
├── api/                    # 后端 API 封装
│   ├── request.ts          # Axios 实例
│   ├── collections.ts      # Collection API
│   ├── activity.ts         # Activity API
│   └── portfolio.ts        # Portfolio API
├── contracts/
│   ├── abis/               # 合约 ABI 文件
│   │   ├── EasySwapOrderBook.sol/
│   │   └── EasySwapVault.sol/
│   └── service/
│       └── orderBookContract.ts  # 合约交互封装
├── hooks/
│   ├── useEthersSigner.ts  # wagmi → ethers signer 转换
│   └── useGlobalState.ts   # 全局状态
├── components/             # UI 组件
└── config/                 # 配置（链、合约地址等）
```

### 4.4 核心交互模式

#### 4.4.1 数据读取：调用后端 API

```typescript
// api/collections.ts
import { request } from './request';

// 获取集合排行榜
function GetCollections(params: { limit: number; range: string }) {
    return request.get('/collections/ranking', { params });
}

// 获取集合详情
function GetCollectionDetail(params: { address: string; chain_id: number }) {
    return request.get(`/collections/${params.address}`, { params });
}

// 获取 NFT 列表
function GetCollectionItems(params: { address: string; filters: {...} }) {
    return request.get(`/collections/${params.address}/items`, {
        params: { filters: JSON.stringify(params.filters) }
    });
}
```

#### 4.4.2 钱包连接：RainbowKit + wagmi

```typescript
// hooks/useEthersSigner.ts
import { useWalletClient } from 'wagmi';
import { BrowserProvider, JsonRpcSigner } from 'ethers';

export function useEthersSigner({ chainId }: { chainId?: number } = {}) {
    const { data: walletClient } = useWalletClient({ chainId });
    
    return useMemo(() => {
        if (!walletClient) return undefined;
        
        const { account, chain, transport } = walletClient;
        const provider = new BrowserProvider(transport, {
            chainId: chain.id,
            name: chain.name,
        });
        return new JsonRpcSigner(provider, account.address);
    }, [walletClient]);
}
```

#### 4.4.3 合约交互：挂单流程

```typescript
// contracts/service/orderBookContract.ts

// 1. 授权 NFT 给 Vault 合约
export async function approveNFT(
    signer: ethers.Signer,
    nftContractAddress: string,
    tokenId: string | number
) {
    const nftContract = new ethers.Contract(
        nftContractAddress,
        ['function approve(address to, uint256 tokenId) external'],
        signer
    );
    
    const tx = await nftContract.approve(VAULT_CONTRACT_ADDRESS, tokenId);
    await tx.wait();
    return tx.hash;
}

// 2. 创建挂单
export async function makeOrders(
    signer: ethers.Signer,
    orders: Order[],
    options: { autoApprove?: boolean }
) {
    const contract = getOrderBookContract(signer);
    
    // 如果需要自动授权
    if (options.autoApprove) {
        for (const order of orders.filter(o => o.side === Side.List)) {
            const isApproved = await checkNFTApproval(signer, order.nft.collection, order.maker, order.nft.tokenId);
            if (!isApproved) {
                await approveNFT(signer, order.nft.collection, order.nft.tokenId);
            }
        }
    }
    
    // 调用合约创建订单
    const tx = await contract.makeOrders(orders);
    const receipt = await tx.wait();
    
    // 从事件中提取订单 ID
    const orderKeys = receipt.logs
        .filter(log => contract.interface.parseLog(log)?.name === 'LogMake')
        .map(log => contract.interface.parseLog(log)?.args.orderKey);
    
    return { orderKeys, transactionHash: receipt.hash };
}
```

### 4.5 数据来源对比

| 数据类型 | 来源 | 原因 |
|:---|:---|:---|
| NFT 列表、详情 | **后端 API** | 需要分页、过滤、排序，链上查询太慢 |
| 交易历史 | **后端 API** | 需要聚合多个事件，链上查询复杂 |
| 地板价、排行榜 | **后端 API** | 需要实时计算，链上无法高效完成 |
| 用户 NFT 余额 | **后端 API** 或 **链上** | 后端更快，链上更准确 |
| 挂单、出价、购买 | **直接调用合约** | 涉及资产转移，必须链上执行 |
| 用户 ETH 余额 | **链上** | 实时性要求高，且查询简单 |

---

## 5. 核心业务流程详解

### 5.1 挂单（List）流程

```
用户操作：将 NFT #123 以 1 ETH 价格挂单出售

┌──────────────┐
│    用户      │
│  点击 List   │
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────────────────┐
│                    前端                          │
│  1. 检查 NFT 是否已授权给 Vault                  │
│  2. 如未授权，调用 NFT.approve(Vault, tokenId)   │
│  3. 构造 Order 结构体                            │
│     {                                            │
│       side: List (0),                            │
│       saleKind: FixedPriceForItem (1),           │
│       maker: 用户地址,                            │
│       nft: { tokenId: 123, collection: 0x..., amount: 1 },│
│       price: 1 ETH,                              │
│       expiry: 7天后,                              │
│       salt: 随机数                                │
│     }                                            │
│  4. 调用 OrderBook.makeOrders([order])           │
└──────────────────────┬───────────────────────────┘
                       │ 交易发送到链上
                       ▼
┌──────────────────────────────────────────────────┐
│               EasySwapOrderBook 合约             │
│  1. 验证订单参数（价格>0, 未过期, salt≠0）        │
│  2. 调用 Vault.depositNFT() 将 NFT 转入金库       │
│  3. 将订单存入红黑树（按价格排序）                │
│  4. 发出 LogMake 事件                            │
└──────────────────────┬───────────────────────────┘
                       │ 事件被广播
                       ▼
┌──────────────────────────────────────────────────┐
│                 EasySwapSync                     │
│  1. 监听到 LogMake 事件                          │
│  2. 解析事件数据                                 │
│  3. 写入 ob_order_sepolia 表                     │
│  4. 更新 ob_item_sepolia 表的 list_price         │
│  5. 写入 ob_activity_sepolia 表                  │
│  6. 更新 ob_collection_sepolia 的 floor_price    │
└──────────────────────────────────────────────────┘
```

### 5.2 购买（Buy）流程

```
用户操作：购买挂单中的 NFT #123（价格 1 ETH）

┌──────────────┐
│    用户      │
│  点击 Buy    │
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────────────────┐
│                    前端                          │
│  1. 从后端 API 获取 sellOrder 信息               │
│  2. 构造匹配的 buyOrder                          │
│     {                                            │
│       side: Bid (1),                             │
│       saleKind: FixedPriceForItem (1),           │
│       maker: 买家地址,                            │
│       nft: { tokenId: 123, collection: 0x..., amount: 1 },│
│       price: 1 ETH,                              │
│       expiry: 当前时间+1分钟,                     │
│       salt: 随机数                                │
│     }                                            │
│  3. 调用 OrderBook.matchOrder(sellOrder, buyOrder)│
│     并附带 1 ETH (msg.value)                     │
└──────────────────────┬───────────────────────────┘
                       │ 交易发送到链上
                       ▼
┌──────────────────────────────────────────────────┐
│               EasySwapOrderBook 合约             │
│  1. 验证订单匹配性                               │
│     - sellOrder.side == List                     │
│     - buyOrder.side == Bid                       │
│     - NFT 信息匹配                               │
│     - 价格满足                                   │
│  2. 计算协议费（如 2.5%）                         │
│  3. 从 Vault 提取 NFT 给买家                     │
│  4. 将 ETH 转给卖家（扣除协议费）                │
│  5. 发出 LogMatch 事件                           │
└──────────────────────┬───────────────────────────┘
                       │ 事件被广播
                       ▼
┌──────────────────────────────────────────────────┐
│                 EasySwapSync                     │
│  1. 监听到 LogMatch 事件                         │
│  2. 更新 ob_order_sepolia 状态为 Filled          │
│  3. 更新 ob_item_sepolia 的 owner                │
│  4. 写入 ob_activity_sepolia (Sale)              │
│  5. 更新 ob_collection_sepolia 的 volume_total   │
└──────────────────────────────────────────────────┘
```

### 5.3 出价（Bid）流程

```
用户操作：对 NFT #123 出价 0.8 ETH

┌──────────────┐
│    用户      │
│ 点击 Make    │
│   Offer     │
└──────┬───────┘
       │
       ▼
┌──────────────────────────────────────────────────┐
│                    前端                          │
│  1. 构造 Bid Order                               │
│     {                                            │
│       side: Bid (1),                             │
│       saleKind: FixedPriceForItem (1),           │
│       maker: 买家地址,                            │
│       nft: { tokenId: 123, collection: 0x..., amount: 1 },│
│       price: 0.8 ETH,                            │
│       expiry: 7天后,                              │
│       salt: 随机数                                │
│     }                                            │
│  2. 调用 OrderBook.makeOrders([order])           │
│     并附带 0.8 ETH (msg.value)                   │
└──────────────────────┬───────────────────────────┘
                       │
                       ▼
┌──────────────────────────────────────────────────┐
│               EasySwapOrderBook 合约             │
│  1. 验证订单参数                                 │
│  2. 调用 Vault.depositETH() 将 ETH 锁入金库      │
│  3. 将订单存入红黑树                             │
│  4. 发出 LogMake 事件                            │
└──────────────────────────────────────────────────┘

后续：NFT 所有者可以选择接受出价（Accept Offer）
      这会触发 matchOrder()，执行类似购买的流程
```

---

## 6. 数据流向图

### 6.1 读取操作（查询）

```
┌──────────┐         ┌──────────────┐         ┌──────────┐
│   前端   │ ──────▶ │  后端 API    │ ──────▶ │  MySQL   │
│          │  HTTP   │              │  SQL    │          │
│          │ ◀────── │              │ ◀────── │          │
└──────────┘  JSON   └──────────────┘  Data   └──────────┘

示例：获取热门集合排行榜
1. 前端调用 GET /api/v1/collections/ranking
2. 后端从 MySQL 查询 ob_collection_sepolia 表
3. 按 volume_total 排序，计算涨跌幅
4. 返回 JSON 给前端
```

### 6.2 写入操作（交易）

```
┌──────────┐         ┌──────────────┐         ┌──────────┐
│   前端   │ ──────▶ │    钱包      │ ──────▶ │  区块链  │
│          │ 构造Tx  │  (MetaMask)  │ 签名&发送│          │
└──────────┘         └──────────────┘         └────┬─────┘
                                                   │ 事件
                                                   ▼
┌──────────┐         ┌──────────────┐         ┌──────────┐
│   前端   │ ◀────── │  后端 API    │ ◀────── │ 索引服务 │
│ (刷新后) │  查询   │              │  同步   │          │
└──────────┘         └──────────────┘         └──────────┘

示例：挂单操作
1. 前端构造 makeOrders() 交易
2. 钱包签名并发送到链上
3. 合约执行，发出 LogMake 事件
4. 索引服务监听到事件，写入数据库
5. 前端刷新页面，从后端 API 获取最新数据
```

### 6.3 完整数据流

```
                                 ┌─────────────────┐
                                 │     用户        │
                                 └────────┬────────┘
                                          │
                    ┌─────────────────────┼─────────────────────┐
                    │                     │                     │
                    ▼                     ▼                     ▼
           ┌────────────────┐    ┌────────────────┐    ┌────────────────┐
           │  浏览 NFT      │    │  连接钱包      │    │  交易操作      │
           │  (只读)        │    │                │    │  (写入)        │
           └───────┬────────┘    └────────────────┘    └───────┬────────┘
                   │                                           │
                   ▼                                           ▼
           ┌────────────────┐                          ┌────────────────┐
           │  后端 API      │                          │  智能合约      │
           │  /collections  │                          │  OrderBook     │
           │  /items        │                          │  Vault         │
           └───────┬────────┘                          └───────┬────────┘
                   │                                           │
                   ▼                                           │ 事件
           ┌────────────────┐                                  │
           │    MySQL       │ ◀────────────────────────────────┘
           │  (索引数据)    │         EasySwapSync 同步
           └────────────────┘
```

---

## 总结

| 层级 | 核心职责 | 不做的事情 |
|:---|:---|:---|
| **合约层** | 资产托管、订单匹配、交易结算、发出事件 | 不存储历史数据、不计算聚合指标 |
| **后端层** | 索引链上事件、存储结构化数据、提供查询 API | 不执行任何链上交易、不托管资产 |
| **前端层** | 展示数据、构造交易、与钱包交互 | 不存储持久化数据、不直接操作资产 |

这种分层架构确保了：
- **安全性**：资产操作只在合约层，受智能合约保护
- **性能**：复杂查询在后端完成，毫秒级响应
- **去中心化**：后端是可选的，用户可直接与合约交互
- **可扩展**：各层独立开发和部署
