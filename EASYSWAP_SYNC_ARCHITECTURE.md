# EasySwapSync 架构解析

> 链上事件同步服务：将智能合约事件实时同步到数据库，支持订单状态管理和地板价追踪。

---

## 📊 整体架构图

```mermaid
flowchart TB
    subgraph Blockchain ["⛓️ 区块链 (Ethereum/Sepolia)"]
        CONTRACT["EasySwapOrderBook<br/>智能合约"]
        EVENTS["链上事件<br/>LogMake/LogCancel/LogMatch"]
    end
    
    subgraph EasySwapSync ["🔄 EasySwapSync 服务"]
        DAEMON["daemon.go<br/>服务入口"]
        SERVICE["service.go<br/>服务管理器"]
        
        subgraph Indexers ["索引器"]
            OBI["OrderBookIndexer<br/>订单簿索引器"]
            CF["CollectionFilter<br/>NFT集合过滤器"]
        end
        
        subgraph External ["外部依赖"]
            OM["OrderManager<br/>订单管理器"]
        end
    end
    
    subgraph Storage ["💾 存储层"]
        DB[(MySQL/PostgreSQL)]
        REDIS[(Redis)]
    end
    
    CONTRACT --> EVENTS
    EVENTS -->|"RPC 轮询"| OBI
    OBI -->|"写入订单"| DB
    OBI -->|"缓存状态"| REDIS
    OBI --> OM
    CF -->|"过滤集合"| DB
    SERVICE --> OBI
    SERVICE --> CF
    SERVICE --> OM
    DAEMON --> SERVICE
    
    style DAEMON fill:#4caf50,color:#fff
    style OBI fill:#2196f3,color:#fff
    style CONTRACT fill:#ff9800,color:#fff
```

---

## 🏗️ 目录结构

```
EasySwapSync/
├── main.go                    # 程序入口
├── cmd/
│   ├── root.go               # Cobra 根命令
│   └── daemon.go             # daemon 子命令（主服务）
├── config/
│   └── config.toml           # 配置文件
├── service/
│   ├── service.go            # 服务管理器（核心）
│   ├── config/
│   │   └── config.go         # 配置结构定义
│   ├── orderbookindexer/
│   │   └── service.go        # 订单簿索引器（1300+行，核心逻辑）
│   ├── collectionfilter/
│   │   └── filter.go         # NFT 集合过滤器
│   └── comm/
│       ├── types.go          # 公共类型定义
│       └── util/             # 工具函数
├── model/
│   └── db.go                 # 数据库初始化
└── db/
    └── migrations/           # 数据库迁移
```

---

## 🔧 核心组件

### 1️⃣ Service (服务管理器)

```go
type Service struct {
    ctx              context.Context
    config           *config.Config
    kvStore          *xkv.Store           // Redis 缓存
    db               *gorm.DB             // 数据库
    collectionFilter *collectionfilter.Filter  // 集合过滤器
    orderbookIndexer *orderbookindexer.Service // 订单簿索引器
    orderManager     *ordermanager.OrderManager // 订单管理器
}
```

| 组件 | 职责 |
|:---|:---|
| **kvStore** | Redis 缓存，存储订单状态和地板价 |
| **db** | 数据库连接，持久化订单数据 |
| **collectionFilter** | 过滤需要追踪的 NFT 集合 |
| **orderbookIndexer** | 监听链上事件，同步订单数据 |
| **orderManager** | 管理订单生命周期 |

---

### 2️⃣ OrderBookIndexer (订单簿索引器)

**核心职责**：监听链上事件，解析并同步到数据库

```mermaid
flowchart LR
    subgraph Events ["链上事件"]
        MAKE["LogMake<br/>创建订单"]
        CANCEL["LogCancel<br/>取消订单"]
        MATCH["LogMatch<br/>订单成交"]
        APPROVAL["Approval<br/>NFT 授权"]
    end
    
    subgraph Handlers ["事件处理器"]
        H1["handleMakeEvent"]
        H2["handleCancelEvent"]
        H3["handleMatchEvent"]
        H4["handleApprovalEvent"]
    end
    
    subgraph Actions ["数据操作"]
        A1["创建订单记录"]
        A2["标记订单取消"]
        A3["更新成交状态"]
        A4["更新授权状态"]
    end
    
    MAKE --> H1 --> A1
    CANCEL --> H2 --> A2
    MATCH --> H3 --> A3
    APPROVAL --> H4 --> A4
```

#### 事件 Topic

```go
const (
    LogMakeTopic        = "0xfc37f2ff..."  // 创建订单
    LogCancelTopic      = "0x5152abd..."   // 取消订单
    LogMatchTopic       = "0xf629aec..."   // 订单成交
    ERC721ApprovalTopic = "0x8c5be1e..."   // NFT 授权
)
```

---

### 3️⃣ CollectionFilter (集合过滤器)

**职责**：维护需要追踪的 NFT 集合白名单

```go
type Filter struct {
    ctx     context.Context
    db      *gorm.DB
    chain   string
    set     map[string]bool  // 集合地址 -> 是否追踪
    lock    *sync.RWMutex    // 读写锁（并发安全）
}
```

| 方法 | 功能 |
|:---|:---|
| `Add(address)` | 添加集合到白名单 |
| `Remove(address)` | 从白名单移除 |
| `Contains(address)` | 检查是否在白名单 |
| `PreloadCollections()` | 从数据库预加载白名单 |

---

## 🔄 同步流程

```mermaid
sequenceDiagram
    participant D as Daemon
    participant S as Service
    participant I as OrderBookIndexer
    participant RPC as 区块链 RPC
    participant DB as 数据库
    
    D->>S: New(ctx, config)
    S->>S: 初始化 Redis, DB
    S->>S: 创建 OrderBookIndexer
    D->>S: Start()
    
    loop 事件同步循环
        I->>RPC: eth_getLogs(fromBlock, toBlock)
        RPC-->>I: 返回事件日志
        
        alt LogMake 事件
            I->>I: handleMakeEvent
            I->>DB: 插入订单记录
        else LogCancel 事件
            I->>I: handleCancelEvent
            I->>DB: 更新订单状态为取消
        else LogMatch 事件
            I->>I: handleMatchEvent
            I->>DB: 更新订单状态为成交
        end
        
        I->>I: 更新同步区块高度
        I->>I: Sleep(10s)
    end
```

---

## ⚙️ 配置结构

```toml
[chain_cfg]
name = "sepolia"
id = 11155111

[contract_cfg]
dex_address = "0xDf4c2715..."    # OrderBook 合约
vault_address = "0x38FfF903..."  # Vault 合约

[ankr_cfg]
https_url = "https://sepolia.infura.io/v3/"
api_key = "your_api_key"

[db]
host = "localhost"
port = 3306
database = "easyswap"

[kv.redis]
host = "localhost:6379"
```

---

## 📊 数据流向

```mermaid
flowchart LR
    subgraph Input ["输入"]
        CHAIN["区块链事件"]
    end
    
    subgraph Process ["处理"]
        PARSE["解析事件数据"]
        VALIDATE["验证数据"]
        TRANSFORM["转换数据格式"]
    end
    
    subgraph Output ["输出"]
        ORDERS["订单表<br/>ob_order_{chain}"]
        ITEMS["NFT 表<br/>ob_item_{chain}"]
        TRADES["成交表<br/>ob_trade_{chain}"]
        FLOOR["地板价表<br/>ob_floor_change"]
    end
    
    CHAIN --> PARSE --> VALIDATE --> TRANSFORM
    TRANSFORM --> ORDERS
    TRANSFORM --> ITEMS
    TRANSFORM --> TRADES
    TRANSFORM --> FLOOR
```

---

## 🔑 关键函数

| 函数 | 文件 | 功能 |
|:---|:---|:---|
| `SyncOrderBookEventLoop` | orderbookindexer/service.go | 主同步循环 |
| `handleMakeEvent` | orderbookindexer/service.go | 处理创建订单事件 |
| `handleCancelEvent` | orderbookindexer/service.go | 处理取消订单事件 |
| `handleMatchEvent` | orderbookindexer/service.go | 处理成交事件 |
| `handleApprovalEvent` | orderbookindexer/service.go | 处理 NFT 授权事件 |
| `checkAndHandleFork` | orderbookindexer/service.go | 处理区块分叉 |
| `UpKeepingCollectionFloorChangeLoop` | orderbookindexer/service.go | 更新地板价 |

---

## 🚀 启动流程

```mermaid
flowchart TB
    START([启动]) --> CONFIG[读取配置文件]
    CONFIG --> LOG[初始化日志]
    LOG --> SERVICE[创建 Service]
    SERVICE --> REDIS[连接 Redis]
    SERVICE --> DB[连接数据库]
    SERVICE --> RPC[创建 RPC 客户端]
    SERVICE --> INDEXER[创建 OrderBookIndexer]
    SERVICE --> FILTER[创建 CollectionFilter]
    FILTER --> PRELOAD[预加载 NFT 集合]
    PRELOAD --> LOOP[启动同步循环]
    LOOP --> RUNNING([运行中])
    
    style START fill:#4caf50,color:#fff
    style RUNNING fill:#2196f3,color:#fff
```

---

## 📋 依赖关系

```mermaid
graph TB
    subgraph EasySwapSync
        SYNC[EasySwapSync]
    end
    
    subgraph EasySwapBase ["EasySwapBase (共享库)"]
        CHAIN[chain/chainclient]
        STORES[stores/xkv, gdb]
        ORDER[ordermanager]
        LOGGER[logger/xzap]
    end
    
    subgraph External ["第三方库"]
        GORM[gorm.io/gorm]
        ZERO[go-zero]
        COBRA[spf13/cobra]
        VIPER[spf13/viper]
    end
    
    SYNC --> CHAIN
    SYNC --> STORES
    SYNC --> ORDER
    SYNC --> LOGGER
    SYNC --> GORM
    SYNC --> ZERO
    SYNC --> COBRA
    SYNC --> VIPER
```

---

## 🔗 与其他服务的关系

```mermaid
flowchart LR
    subgraph OnChain ["链上"]
        CONTRACT["EasySwapOrderBook<br/>智能合约"]
    end
    
    subgraph Backend ["后端服务"]
        SYNC["EasySwapSync<br/>事件同步"]
        API["EasySwapApi<br/>API 服务"]
    end
    
    subgraph Frontend ["前端"]
        WEB["Web 应用"]
    end
    
    CONTRACT -->|"事件同步"| SYNC
    SYNC -->|"写入数据库"| API
    API -->|"提供 API"| WEB
    WEB -->|"发送交易"| CONTRACT
    
    style SYNC fill:#2196f3,color:#fff
```

---

> 📝 **文档版本**: v1.0  
> 📅 **更新日期**: 2026-02-09
