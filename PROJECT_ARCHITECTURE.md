# NFT Marketplace 项目架构文档

> 本文档详细介绍 NFT Marketplace 项目的整体架构设计，包含系统架构、数据流程、交易逻辑等核心内容。

---

## 目录

- [1. 项目概述](#1-项目概述)
- [2. 系统架构图](#2-系统架构图)
- [3. 模块详解](#3-模块详解)
- [4. 数据模型](#4-数据模型)
- [5. 核心业务流程](#5-核心业务流程)
- [6. 智能合约架构](#6-智能合约架构)
- [7. 数据同步流程](#7-数据同步流程)
- [8. 技术栈总览](#8-技术栈总览)

---

## 1. 项目概述

这是一个 **全栈去中心化 NFT 交易市场**，采用 **链下订单簿 + 链上结算** 架构，类似于 OpenSea 或 LooksRare 的设计模式。

### 核心特点

- 🔗 **链上链下结合**：订单签名在链下，资产结算在链上
- 📚 **订单簿模式**：非 AMM，支持限价单交易
- 🏗️ **微服务架构**：API 服务、数据同步服务分离
- ⚡ **高性能查询**：链下数据库支撑高效查询

---

## 2. 系统架构图

### 2.1 整体架构

```mermaid
graph TB
    subgraph "🖥️ 用户层 Frontend"
        USER[用户]
        WALLET[MetaMask 钱包]
        FE["nft-market-fe<br/>Next.js + TailwindCSS"]
    end
    
    subgraph "🌐 服务层 Backend Services"
        API["EasySwapBackend<br/>Go API 服务<br/>━━━━━━━━━━<br/>• Collection API<br/>• Item API<br/>• Order API<br/>• Activity API"]
        SYNC["EasySwapSync<br/>Go 数据同步服务<br/>━━━━━━━━━━<br/>• 区块监听<br/>• 事件解析<br/>• 数据入库"]
    end
    
    subgraph "💾 数据层 Data Layer"
        DB[(MySQL<br/>持久化存储)]
        REDIS[(Redis<br/>缓存/队列)]
    end
    
    subgraph "🧰 基础设施层 Infrastructure"
        BASE["EasySwapBase<br/>Go 公共库<br/>━━━━━━━━━━<br/>• chain 链交互<br/>• evm 编解码<br/>• logger 日志<br/>• stores 存储<br/>• errcode 错误码"]
    end
    
    subgraph "⛓️ 区块链层 Blockchain"
        CONTRACT["EasySwapContract<br/>Solidity 智能合约<br/>━━━━━━━━━━<br/>• OrderBookExchange<br/>• OrderVault"]
        CHAIN["Ethereum / Sepolia"]
    end
    
    USER --> WALLET
    WALLET --> FE
    FE <-->|API 请求| API
    FE <-->|合约交互| CHAIN
    
    API --> DB
    API --> REDIS
    API -.->|依赖| BASE
    
    SYNC -->|监听事件| CHAIN
    SYNC -->|写入数据| DB
    SYNC -.->|依赖| BASE
    
    CONTRACT -->|部署| CHAIN

    style USER fill:#e1f5fe
    style FE fill:#fff3e0
    style API fill:#e8f5e9
    style SYNC fill:#e8f5e9
    style DB fill:#fce4ec
    style REDIS fill:#fce4ec
    style BASE fill:#f3e5f5
    style CONTRACT fill:#fff8e1
    style CHAIN fill:#fff8e1
```

### 2.2 模块依赖关系

```mermaid
graph LR
    subgraph "应用层"
        FE[nft-market-fe]
        API[EasySwapBackend]
        SYNC[EasySwapSync]
    end
    
    subgraph "基础层"
        BASE[EasySwapBase]
        CONTRACT[EasySwapContract]
    end
    
    FE -->|调用 API| API
    FE -->|调用合约| CONTRACT
    API -->|依赖| BASE
    SYNC -->|依赖| BASE
    SYNC -->|监听| CONTRACT
    
    style FE fill:#42a5f5
    style API fill:#66bb6a
    style SYNC fill:#66bb6a
    style BASE fill:#ab47bc
    style CONTRACT fill:#ffa726
```

---

## 3. 模块详解

### 3.1 模块总览表

| 目录 | 角色 | 技术栈 | 核心职责 |
|:---|:---|:---|:---|
| `EasySwapContract` | 💎 核心逻辑 | Solidity, Hardhat | 链上订单簿交易撮合 |
| `EasySwapSync` | 🔄 数据索引器 | Go | 监听链上事件，同步到数据库 |
| `EasySwapBackend` | 🌐 API 服务 | Go | 为前端提供高性能查询接口 |
| `EasySwapBase` | 🧰 基础设施 | Go | 公共工具库（日志、链交互、错误码等） |
| `nft-market-fe` | 🖥️ 前端界面 | Next.js, TS, Tailwind | 用户交互界面 |

### 3.2 EasySwapBase 公共库结构

```mermaid
graph TB
    BASE[EasySwapBase]
    
    BASE --> CHAIN[chain<br/>链交互封装]
    BASE --> EVM[evm<br/>EVM 编解码]
    BASE --> LOGGER[logger<br/>日志工具]
    BASE --> STORES[stores<br/>存储层抽象]
    BASE --> ERRCODE[errcode<br/>错误码定义]
    BASE --> KIT[kit<br/>通用工具集]
    BASE --> XHTTP[xhttp<br/>HTTP 工具]
    BASE --> RETRY[retry<br/>重试机制]
    BASE --> ORDER[ordermanager<br/>订单管理]
    
    style BASE fill:#ab47bc,color:#fff
    style CHAIN fill:#e1bee7
    style EVM fill:#e1bee7
    style LOGGER fill:#e1bee7
    style STORES fill:#e1bee7
    style ERRCODE fill:#e1bee7
    style KIT fill:#e1bee7
    style XHTTP fill:#e1bee7
    style RETRY fill:#e1bee7
    style ORDER fill:#e1bee7
```

### 3.3 前端模块结构

```mermaid
graph TB
    FE[nft-market-fe]
    
    FE --> APP[app<br/>页面路由]
    FE --> COMP[components<br/>UI 组件]
    FE --> HOOKS[hooks<br/>React Hooks<br/>钱包连接]
    FE --> API_DIR[api<br/>后端接口]
    FE --> CONTRACTS[contracts<br/>合约 ABI]
    FE --> CONFIG[config<br/>配置文件]
    FE --> LIB[lib<br/>工具函数]
    
    style FE fill:#42a5f5,color:#fff
    style APP fill:#bbdefb
    style COMP fill:#bbdefb
    style HOOKS fill:#bbdefb
    style API_DIR fill:#bbdefb
    style CONTRACTS fill:#bbdefb
    style CONFIG fill:#bbdefb
    style LIB fill:#bbdefb
```

---

## 4. 数据模型

### 4.1 核心实体关系

```mermaid
erDiagram
    COLLECTION ||--o{ ITEM : "包含"
    ITEM ||--o{ ORDER : "关联"
    ITEM ||--o{ ACTIVITY : "产生"
    WALLET ||--o{ ITEM : "拥有"
    WALLET ||--o{ ORDER : "创建"
    ORDER ||--o{ ACTIVITY : "触发"
    
    COLLECTION {
        bigint id PK
        varchar address "合约地址"
        varchar name "集合名称"
        varchar symbol "标识符"
        varchar creator "创建者"
        bigint item_amount "NFT 总量"
        bigint owner_amount "持有人数"
        decimal floor_price "地板价"
        decimal volume_total "总交易量"
    }
    
    ITEM {
        bigint id PK
        varchar collection_address FK
        varchar token_id "Token ID"
        varchar name "NFT 名称"
        varchar owner "当前拥有者"
        varchar creator "创建者"
        decimal list_price "挂单价格"
        decimal sale_price "成交价格"
    }
    
    ORDER {
        bigint id PK
        varchar order_id "订单 Hash"
        tinyint order_type "类型: listing/offer/bid"
        tinyint order_status "状态"
        varchar collection_address FK
        varchar token_id
        varchar maker "挂单者"
        varchar taker "吃单者"
        decimal price "价格"
        bigint expire_time "过期时间"
    }
    
    ACTIVITY {
        bigint id PK
        tinyint activity_type "类型: mint/transfer/buy/sell"
        varchar collection_address FK
        varchar token_id
        varchar maker "发起方"
        varchar taker "接收方"
        decimal price "价格"
        varchar tx_hash "交易哈希"
        bigint block_number "区块号"
    }
    
    WALLET {
        varchar address PK "钱包地址"
    }
```

### 4.2 订单类型说明

```mermaid
graph LR
    subgraph "📋 订单类型 Order Types"
        O1["1️⃣ Listing<br/>卖家挂单出售"]
        O2["2️⃣ Offer<br/>买家报价"]
        O3["3️⃣ Collection Bid<br/>集合出价"]
        O4["4️⃣ Item Bid<br/>单品出价"]
    end
    
    subgraph "📊 Activity 类型"
        A1["1 Buy 购买"]
        A2["2 Mint 铸造"]
        A3["3 List 挂单"]
        A4["4 Cancel Listing"]
        A5["5 Cancel Offer"]
        A6["6 Make Offer"]
        A7["7 Sell 出售"]
        A8["8 Transfer 转移"]
        A9["9 Collection Bid"]
        A10["10 Item Bid"]
    end
```

---

## 5. 核心业务流程

### 5.1 NFT 挂单出售流程 (Listing Flow)

```mermaid
sequenceDiagram
    autonumber
    participant 卖家 as 🧑 卖家
    participant 前端 as 🖥️ 前端
    participant 钱包 as 🦊 MetaMask
    participant 后端 as 🌐 Backend
    participant 合约 as 📜 OrderBookExchange
    participant 链 as ⛓️ Blockchain
    
    卖家->>前端: 选择 NFT，设置价格
    前端->>钱包: 请求 EIP-712 签名
    
    Note over 钱包: 构造订单结构:<br/>- collection<br/>- tokenId<br/>- price<br/>- expireTime<br/>- salt
    
    钱包->>钱包: 用户确认签名
    钱包-->>前端: 返回签名 signature
    
    前端->>后端: 提交订单 + 签名
    后端->>后端: 验证签名有效性
    后端->>后端: 存储订单到数据库
    后端-->>前端: 订单创建成功
    
    Note over 后端: 订单存储在链下<br/>节省 Gas 费用
    
    前端-->>卖家: 显示挂单成功
```

### 5.2 NFT 购买流程 (Buy Flow)

```mermaid
sequenceDiagram
    autonumber
    participant 买家 as 🧑 买家
    participant 前端 as 🖥️ 前端
    participant 后端 as 🌐 Backend
    participant 钱包 as 🦊 MetaMask
    participant 合约 as 📜 OrderBookExchange
    participant Vault as 🏦 OrderVault
    participant 同步 as 🔄 Sync Service
    participant 链 as ⛓️ Blockchain
    
    买家->>前端: 点击购买 NFT
    前端->>后端: 获取订单详情 + 签名
    后端-->>前端: 返回完整订单数据
    
    前端->>钱包: 构造交易请求
    
    Note over 钱包: 调用合约方法:<br/>fulfillOrder(order, signature)
    
    钱包->>钱包: 用户确认交易
    钱包->>合约: 发送交易 + ETH
    
    合约->>合约: 验证签名
    合约->>合约: 验证订单有效性
    合约->>Vault: 转移 NFT 给买家
    合约->>合约: 转移 ETH 给卖家
    合约->>链: 发出 OrderFulfilled 事件
    
    链-->>同步: 监听到事件
    同步->>同步: 解析事件数据
    同步->>后端: 更新订单状态
    同步->>后端: 更新 Item Owner
    同步->>后端: 创建 Activity 记录
    
    钱包-->>前端: 交易确认
    前端-->>买家: 购买成功！
```

### 5.3 订单簿交易完整流程

```mermaid
flowchart TB
    START((开始)) --> CHECK_TYPE{用户操作类型?}
    
    CHECK_TYPE -->|挂单 Listing| LISTING
    CHECK_TYPE -->|购买 Buy| BUY
    CHECK_TYPE -->|出价 Offer| OFFER
    CHECK_TYPE -->|接受出价 Accept| ACCEPT
    
    subgraph LISTING [📤 挂单流程]
        L1[选择 NFT] --> L2[设置价格和过期时间]
        L2 --> L3[EIP-712 签名]
        L3 --> L4[提交到后端存储]
        L4 --> L5[订单上架成功]
    end
    
    subgraph BUY [🛒 购买流程]
        B1[浏览市场] --> B2[选择心仪 NFT]
        B2 --> B3[获取订单签名]
        B3 --> B4[调用合约 fulfillOrder]
        B4 --> B5[链上验证 & 结算]
        B5 --> B6[NFT 转移完成]
    end
    
    subgraph OFFER [💰 出价流程]
        O1[选择 NFT] --> O2[设置出价金额]
        O2 --> O3[存入保证金到 Vault]
        O3 --> O4[创建 Offer 订单]
        O4 --> O5[等待卖家接受]
    end
    
    subgraph ACCEPT [✅ 接受出价]
        A1[查看收到的出价] --> A2[选择接受]
        A2 --> A3[调用合约成交]
        A3 --> A4[NFT & ETH 互换]
    end
    
    L5 --> END((结束))
    B6 --> END
    O5 --> END
    A4 --> END
```

### 5.4 订单状态流转

```mermaid
stateDiagram-v2
    [*] --> Created: 创建订单
    
    Created --> Active: 签名验证通过
    Created --> Invalid: 签名验证失败
    
    Active --> Fulfilled: 被成交
    Active --> Cancelled: 用户取消
    Active --> Expired: 订单过期
    
    Fulfilled --> [*]
    Cancelled --> [*]
    Expired --> [*]
    Invalid --> [*]
    
    note right of Active
        订单在链下存储
        等待 Taker 吃单
    end note
    
    note right of Fulfilled
        链上成交
        产生 Activity
    end note
```

---

## 6. 智能合约架构

### 6.1 合约组件结构

```mermaid
graph TB
    subgraph "📜 EasySwapContract"
        EXCHANGE["OrderBookExchange<br/>━━━━━━━━━━━━<br/>核心交易合约<br/>处理订单撮合逻辑"]
        
        STORAGE["OrderStorage<br/>━━━━━━━━━━━━<br/>订单存储模块<br/>管理链上订单状态"]
        
        VALIDATOR["OrderValidator<br/>━━━━━━━━━━━━<br/>订单验证模块<br/>签名验证 & 条件检查"]
        
        PROTOCOL["ProtocolManager<br/>━━━━━━━━━━━━<br/>协议费管理<br/>手续费收取 & 分配"]
        
        VAULT["OrderVault<br/>━━━━━━━━━━━━<br/>资产托管模块<br/>NFT & ETH 托管"]
    end
    
    EXCHANGE --> STORAGE
    EXCHANGE --> VALIDATOR
    EXCHANGE --> PROTOCOL
    EXCHANGE --> VAULT
    
    style EXCHANGE fill:#ff9800,color:#fff
    style STORAGE fill:#ffe0b2
    style VALIDATOR fill:#ffe0b2
    style PROTOCOL fill:#ffe0b2
    style VAULT fill:#fff3e0
```

### 6.2 合约交互流程

```mermaid
sequenceDiagram
    participant User as 用户
    participant Exchange as OrderBookExchange
    participant Validator as OrderValidator
    participant Storage as OrderStorage
    participant Vault as OrderVault
    participant Protocol as ProtocolManager
    
    User->>Exchange: fulfillOrder(order, signature)
    
    Exchange->>Validator: validateOrder(order, signature)
    Validator->>Validator: 验证 EIP-712 签名
    Validator->>Validator: 检查订单未过期
    Validator->>Storage: 检查订单未被使用
    Validator-->>Exchange: 验证通过 ✓
    
    Exchange->>Vault: transferNFT(seller, buyer, tokenId)
    Vault-->>Exchange: NFT 转移完成 ✓
    
    Exchange->>Protocol: calculateFee(price)
    Protocol-->>Exchange: 返回手续费金额
    
    Exchange->>Exchange: 分配资金
    Note over Exchange: seller: price - fee<br/>protocol: fee
    
    Exchange->>Storage: markOrderFulfilled(orderId)
    
    Exchange-->>User: 交易成功 🎉
```

### 6.3 EIP-712 签名验证

```mermaid
flowchart LR
    subgraph "📝 签名生成 (链下)"
        A1[构造订单结构] --> A2[计算 structHash]
        A2 --> A3[计算 domainSeparator]
        A3 --> A4[计算 digest]
        A4 --> A5[私钥签名]
        A5 --> A6[得到 v,r,s]
    end
    
    subgraph "🔐 签名验证 (链上)"
        B1[接收 order + signature] --> B2[重构 structHash]
        B2 --> B3[重构 digest]
        B3 --> B4[ecrecover 恢复地址]
        B4 --> B5{地址匹配?}
        B5 -->|是| B6[验证通过 ✓]
        B5 -->|否| B7[验证失败 ✗]
    end
    
    A6 -.->|传输| B1
```

---

## 7. 数据同步流程

### 7.1 EasySwapSync 工作流程

```mermaid
sequenceDiagram
    participant Chain as ⛓️ 区块链 RPC
    participant Sync as 🔄 EasySwapSync
    participant Parser as 📋 事件解析器
    participant DB as 💾 MySQL
    participant Cache as 📦 Redis
    
    loop 持续监听
        Sync->>Chain: eth_getLogs(fromBlock, toBlock)
        Chain-->>Sync: 返回事件列表
        
        loop 处理每个事件
            Sync->>Parser: 解析事件类型
            
            alt Transfer 事件
                Parser->>DB: 更新 Item.owner
                Parser->>DB: 创建 Activity(Transfer)
                Parser->>DB: 更新 Collection 统计
            else OrderFulfilled 事件
                Parser->>DB: 标记 Order 已完成
                Parser->>DB: 创建 Activity(Buy/Sell)
                Parser->>DB: 更新 Item.sale_price
            else Mint 事件
                Parser->>DB: 创建新 Item
                Parser->>DB: 创建 Activity(Mint)
                Parser->>DB: 更新 Collection.item_amount
            end
            
            Parser->>Cache: 更新缓存数据
        end
        
        Sync->>DB: 记录最新同步区块
    end
```

### 7.2 区块回滚处理

```mermaid
flowchart TB
    START[监听新区块] --> CHECK{区块高度连续?}
    
    CHECK -->|是| PROCESS[正常处理事件]
    CHECK -->|否| REORG[检测到回滚!]
    
    REORG --> FIND[找到分叉点]
    FIND --> DELETE[删除无效区块数据]
    DELETE --> RESYNC[从分叉点重新同步]
    RESYNC --> PROCESS
    
    PROCESS --> UPDATE[更新数据库]
    UPDATE --> NEXT[继续监听]
    NEXT --> START
    
    style REORG fill:#ff5252,color:#fff
    style DELETE fill:#ff8a80
    style RESYNC fill:#ff8a80
```

### 7.3 事件类型映射

```mermaid
graph LR
    subgraph "链上事件"
        E1[Transfer 事件]
        E2[OrderCreated]
        E3[OrderFulfilled]
        E4[OrderCancelled]
    end
    
    subgraph "数据库操作"
        D1[更新 Item Owner<br/>创建 Transfer Activity]
        D2[创建 Order 记录]
        D3[更新 Order 状态<br/>创建 Buy/Sell Activity<br/>更新统计数据]
        D4[标记 Order 已取消<br/>创建 Cancel Activity]
    end
    
    E1 --> D1
    E2 --> D2
    E3 --> D3
    E4 --> D4
```

---

## 8. 技术栈总览

### 8.1 技术选型

```mermaid
mindmap
  root((NFT Marketplace))
    前端
      Next.js
      TypeScript
      TailwindCSS
      Wagmi 钱包连接
      ethers.js
    后端
      Go
      Gin Web 框架
      GORM ORM
      go-ethereum
    数据库
      MySQL 主存储
      Redis 缓存
    区块链
      Solidity
      Hardhat
      EIP-712 签名
      Sepolia 测试网
    基础设施
      Docker
      GitHub Actions
```

### 8.2 开发环境要求

| 组件 | 版本要求 |
|:---|:---|
| Node.js | >= 18.x |
| Go | >= 1.18 |
| MySQL | >= 8.0 |
| Redis | >= 6.0 |
| Hardhat | Latest |

### 8.3 项目启动流程

```mermaid
flowchart LR
    A[1. 部署合约] --> B[2. 配置数据库]
    B --> C[3. 启动 Sync 服务]
    C --> D[4. 启动 Backend]
    D --> E[5. 启动前端]
    
    A -.->|Hardhat| CHAIN[(Sepolia)]
    C -.->|监听| CHAIN
    D -.->|读写| DB[(MySQL)]
    E -.->|API| D
    E -.->|合约调用| CHAIN
```

---

## 附录：学习路线建议

```mermaid
flowchart TB
    subgraph P1["🔷 第一阶段: 智能合约"]
        A1["⭐⭐⭐⭐⭐ 理解订单簿模型 (必学)"]
        A2["⭐⭐⭐⭐⭐ 学习 EIP-712 签名 (必学)"]
        A3["⭐⭐⭐⭐ 运行合约测试 (建议)"]
    end
    
    subgraph P2["🔷 第二阶段: 数据同步"]
        B1["⭐⭐⭐⭐ 理解事件监听 (必学)"]
        B2["⭐⭐⭐⭐⭐ 理解数据模型 (必学)"]
        B3["⭐⭐⭐ 学习回滚处理 (进阶)"]
    end
    
    subgraph P3["🔷 第三阶段: 后端 API"]
        C1["⭐⭐⭐⭐ 理解接口设计 (必学)"]
        C2["⭐⭐⭐ 学习数据聚合 (建议)"]
    end
    
    subgraph P4["🔷 第四阶段: 前端"]
        D1["⭐⭐⭐⭐ 钱包连接 (必学)"]
        D2["⭐⭐⭐⭐⭐ 合约交互 (必学)"]
        D3["⭐⭐⭐⭐⭐ 完整流程串联 (必学)"]
    end
    
    P1 --> P2 --> P3 --> P4
```

---

> 📝 **文档版本**: v1.0  
> 📅 **更新日期**: 2026-02-07  
> 🔗 **项目地址**: [GitHub Repository](https://github.com/MetaNodeAcademy/ProjectBreakdown-NFTMarket)
