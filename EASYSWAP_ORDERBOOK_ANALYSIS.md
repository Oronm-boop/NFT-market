# EasySwapOrderBook 合约解析

> 本文档对 `EasySwapOrderBook.sol` 核心交易合约进行详细解析，包括合约结构、核心功能、业务流程等可视化说明。

---

## 📊 合约概览

`EasySwapOrderBook` 是 NFT 订单簿交易系统的**核心合约**，负责订单的创建、取消、编辑和撮合成交。

### 合约继承结构

```mermaid
graph TB
    subgraph "OpenZeppelin 可升级合约"
        INIT[Initializable]
        CTX[ContextUpgradeable]
        OWN[OwnableUpgradeable]
        REENT[ReentrancyGuardUpgradeable]
        PAUSE[PausableUpgradeable]
    end
    
    subgraph "自定义模块"
        STORAGE[OrderStorage<br/>订单存储]
        PROTOCOL[ProtocolManager<br/>协议费管理]
        VALIDATOR[OrderValidator<br/>EIP-712 验证]
    end
    
    ORDERBOOK[EasySwapOrderBook<br/>核心交易合约]
    
    INIT --> ORDERBOOK
    CTX --> ORDERBOOK
    OWN --> ORDERBOOK
    REENT --> ORDERBOOK
    PAUSE --> ORDERBOOK
    STORAGE --> ORDERBOOK
    PROTOCOL --> ORDERBOOK
    VALIDATOR --> ORDERBOOK
    
    style ORDERBOOK fill:#ff9800,color:#fff
    style STORAGE fill:#e3f2fd
    style PROTOCOL fill:#e8f5e9
    style VALIDATOR fill:#fff3e0
```

---

## 🏗️ 核心组件

| 组件 | 职责 | 关键功能 |
|:---|:---|:---|
| **OrderStorage** | 订单存储 | 红黑树价格排序、链表时间优先 |
| **ProtocolManager** | 协议费管理 | 设置/计算手续费比例 |
| **OrderValidator** | 签名验证 | EIP-712 签名校验、订单状态验证 |
| **EasySwapVault** | 资产托管 | NFT 和 ETH 存取管理 |

### 组件交互关系

```mermaid
flowchart LR
    subgraph "EasySwapOrderBook"
        MAKE[makeOrders<br/>创建订单]
        CANCEL[cancelOrders<br/>取消订单]
        EDIT[editOrders<br/>编辑订单]
        MATCH[matchOrder<br/>撮合成交]
    end
    
    subgraph "依赖组件"
        STORAGE[(OrderStorage)]
        VAULT[(EasySwapVault)]
        VALIDATOR[OrderValidator]
        PROTOCOL[ProtocolManager]
    end
    
    MAKE --> STORAGE
    MAKE --> VAULT
    CANCEL --> STORAGE
    CANCEL --> VAULT
    EDIT --> STORAGE
    EDIT --> VAULT
    MATCH --> STORAGE
    MATCH --> VAULT
    MATCH --> VALIDATOR
    MATCH --> PROTOCOL
```

---

## 📋 核心函数一览

```mermaid
mindmap
  root((EasySwapOrderBook))
    订单管理
      makeOrders 批量创建订单
      cancelOrders 批量取消订单
      editOrders 批量编辑订单
    订单撮合
      matchOrder 单笔撮合
      matchOrders 批量撮合
      matchOrderWithoutPayback 内部撮合
    管理功能
      setVault 设置金库
      setProtocolShare 设置费率
      withdrawETH 提取手续费
      pause/unpause 暂停/恢复
```

---

## 🔄 核心业务流程

### 1️⃣ 创建订单 (makeOrders)

```mermaid
flowchart TB
    START([用户调用 makeOrders]) --> CHECK_TYPE{订单类型?}
    
    CHECK_TYPE -->|List 挂单| LIST_FLOW
    CHECK_TYPE -->|Bid 出价| BID_FLOW
    
    subgraph LIST_FLOW [List 挂单流程]
        L1[验证: maker = msg.sender]
        L2[验证: price != 0]
        L3[验证: salt != 0]
        L4[验证: expiry 有效]
        L5[验证: amount = 1]
        L6[Vault.depositNFT 存入 NFT]
        L7[_addOrder 写入订单存储]
        L1 --> L2 --> L3 --> L4 --> L5 --> L6 --> L7
    end
    
    subgraph BID_FLOW [Bid 出价流程]
        B1[验证: maker = msg.sender]
        B2[验证: price != 0]
        B3[验证: salt != 0]
        B4[验证: expiry 有效]
        B5[验证: amount > 0]
        B6[计算 ETH = price × amount]
        B7[Vault.depositETH 存入 ETH]
        B8[_addOrder 写入订单存储]
        B1 --> B2 --> B3 --> B4 --> B5 --> B6 --> B7 --> B8
    end
    
    LIST_FLOW --> EMIT[发出 LogMake 事件]
    BID_FLOW --> EMIT
    EMIT --> RETURN[返回 orderKey]
    
    style START fill:#4caf50,color:#fff
    style RETURN fill:#2196f3,color:#fff
```

### 验证规则

| 条件 | 说明 |
|:---|:---|
| `order.maker == msg.sender` | 只能为自己创建订单 |
| `order.price != 0` | 价格不能为零 |
| `order.salt != 0` | 随机数防重放 |
| `order.expiry > block.timestamp` 或 `== 0` | 过期时间有效或永不过期 |
| `filledAmount[orderKey] == 0` | 订单未被取消或成交过 |

---

### 2️⃣ 取消订单 (cancelOrders)

```mermaid
flowchart TB
    START([用户调用 cancelOrders]) --> LOAD[加载订单: orders[orderKey]]
    LOAD --> CHECK{验证条件}
    
    CHECK -->|失败| SKIP[发出 LogSkipOrder 事件]
    CHECK -->|通过| TYPE{订单类型?}
    
    TYPE -->|List| LIST_CANCEL
    TYPE -->|Bid| BID_CANCEL
    
    subgraph LIST_CANCEL [List 取消流程]
        LC1[_removeOrder 从存储移除]
        LC2[Vault.withdrawNFT 提取 NFT]
        LC3[_cancelOrder 标记取消]
        LC1 --> LC2 --> LC3
    end
    
    subgraph BID_CANCEL [Bid 取消流程]
        BC1[计算未成交数量]
        BC2[_removeOrder 从存储移除]
        BC3[Vault.withdrawETH 提取 ETH]
        BC4[_cancelOrder 标记取消]
        BC1 --> BC2 --> BC3 --> BC4
    end
    
    LIST_CANCEL --> EMIT[发出 LogCancel 事件]
    BID_CANCEL --> EMIT
    EMIT --> DONE([返回 success])
    SKIP --> FAIL([返回 false])
    
    style START fill:#f44336,color:#fff
    style DONE fill:#4caf50,color:#fff
    style FAIL fill:#9e9e9e,color:#fff
```

### 取消条件

```solidity
// 只有满足以下条件才能取消
order.maker == _msgSender() &&           // 只有创建者可以取消
filledAmount[orderKey] < order.nft.amount // 订单未完全成交
```

---

### 3️⃣ 编辑订单 (editOrders)

```mermaid
flowchart TB
    START([用户调用 editOrders]) --> LOAD[加载旧订单]
    LOAD --> VALIDATE{编辑限制检查}
    
    VALIDATE -->|失败| SKIP[发出 LogSkipOrder]
    VALIDATE -->|通过| CANCEL[取消旧订单]
    
    CANCEL --> CREATE[创建新订单]
    CREATE --> ASSET{资产处理}
    
    ASSET -->|List| EDIT_NFT[Vault.editNFT<br/>更新 NFT 关联]
    ASSET -->|Bid| CALC_DIFF{价格差额?}
    
    CALC_DIFF -->|新价更高| ADD_ETH[补充差额 ETH]
    CALC_DIFF -->|新价更低| REFUND[退回多余 ETH]
    
    ADD_ETH --> EDIT_ETH[Vault.editETH]
    REFUND --> EDIT_ETH
    
    EDIT_NFT --> EMIT[发出 LogCancel + LogMake]
    EDIT_ETH --> EMIT
    EMIT --> RETURN([返回新 orderKey])
    
    style START fill:#ff9800,color:#fff
    style RETURN fill:#2196f3,color:#fff
```

### 编辑限制

| 可修改 | 不可修改 |
|:---|:---|
| ✅ price 价格 | ❌ saleKind 销售类型 |
| ✅ amount 数量 | ❌ side 订单方向 |
| ✅ expiry 过期时间 | ❌ maker 创建者 |
| ✅ salt 随机数 | ❌ collection 合约地址 |
| | ❌ tokenId |

---

### 4️⃣ 撮合成交 (matchOrder)

```mermaid
flowchart TB
    START([用户调用 matchOrder]) --> CHECK[_isMatchAvailable<br/>检查匹配条件]
    CHECK --> WHO{谁发起的?}
    
    WHO -->|sellOrder.maker| SELLER_ACCEPT
    WHO -->|buyOrder.maker| BUYER_ACCEPT
    
    subgraph SELLER_ACCEPT [卖家接受出价]
        S1[验证 sellOrder]
        S2[验证 buyOrder 存在]
        S3[fillPrice = buyOrder.price]
        S4[Vault.withdrawETH 提取买家 ETH]
        S5[计算协议费]
        S6[转 ETH 给卖家]
        S7[转 NFT 给买家]
        S1 --> S2 --> S3 --> S4 --> S5 --> S6 --> S7
    end
    
    subgraph BUYER_ACCEPT [买家接受挂单]
        B1[验证 sellOrder 存在]
        B2[验证 buyOrder]
        B3[fillPrice = sellOrder.price]
        B4[验证 msg.value >= fillPrice]
        B5[计算协议费]
        B6[转 ETH 给卖家]
        B7[Vault.withdrawNFT 提取 NFT 给买家]
        B1 --> B2 --> B3 --> B4 --> B5 --> B6 --> B7
    end
    
    SELLER_ACCEPT --> EMIT[发出 LogMatch 事件]
    BUYER_ACCEPT --> EMIT
    EMIT --> DONE([成交完成])
    
    style START fill:#4caf50,color:#fff
    style DONE fill:#2196f3,color:#fff
```

### 匹配条件 (_isMatchAvailable)

```solidity
sellOrderKey != buyOrderKey        // 不能是同一订单
sellOrder.side == Side.List        // 卖单必须是 List
buyOrder.side == Side.Bid          // 买单必须是 Bid
sellOrder.maker != buyOrder.maker  // 买卖双方不能是同一人
// 资产匹配：Collection Bid 或 tokenId 相同
buyOrder.saleKind == FixedPriceForCollection || 
    (collection 和 tokenId 相同)
// 订单未完全成交
filledAmount[sellOrderKey] < sellOrder.nft.amount
filledAmount[buyOrderKey] < buyOrder.nft.amount
```

---

## 💰 资金流转

### 买家购买 NFT (List → Buy)

```mermaid
sequenceDiagram
    participant 买家 as 🧑 买家
    participant 合约 as 📜 OrderBook
    participant 金库 as 🏦 Vault
    participant 卖家 as 🧑 卖家
    
    Note over 买家,卖家: 前置：卖家已挂单，NFT 在金库中
    
    买家->>合约: matchOrder + ETH
    合约->>合约: 验证订单匹配
    合约->>合约: 计算协议费 (fillPrice × protocolShare)
    合约->>卖家: 转账 (fillPrice - 协议费) ETH
    合约->>金库: withdrawNFT
    金库->>买家: 转移 NFT
    合约->>合约: 保留协议费
```

### 卖家接受出价 (Bid → Accept)

```mermaid
sequenceDiagram
    participant 卖家 as 🧑 卖家
    participant 合约 as 📜 OrderBook
    participant 金库 as 🏦 Vault
    participant 买家 as 🧑 买家
    
    Note over 卖家,买家: 前置：买家已出价，ETH 在金库中
    
    卖家->>合约: matchOrder (无需 ETH)
    合约->>合约: 验证订单匹配
    合约->>金库: withdrawETH
    金库->>合约: 转入买家 ETH
    合约->>合约: 计算协议费
    合约->>卖家: 转账 (fillPrice - 协议费) ETH
    合约->>金库: withdrawNFT
    金库->>买家: 转移 NFT
```

---

## 🔐 安全机制

### 1. 重入保护

```solidity
modifier nonReentrant {
    // OpenZeppelin ReentrancyGuard
    // 防止在函数执行期间重复调用
}
```

### 2. 暂停机制

```solidity
modifier whenNotPaused {
    // 合约可被 Owner 暂停
    // 紧急情况下停止所有交易
}
```

### 3. DelegateCall 限制

```solidity
modifier onlyDelegateCall {
    require(address(this) != self);
    // 只允许通过 delegatecall 调用
    // 用于批量撮合的原子性
}
```

### 4. 订单验证

```mermaid
flowchart LR
    VALIDATE[订单验证] --> CHECK1[maker 身份验证]
    VALIDATE --> CHECK2[价格非零]
    VALIDATE --> CHECK3[salt 非零]
    VALIDATE --> CHECK4[过期时间有效]
    VALIDATE --> CHECK5[订单未被使用]
    VALIDATE --> CHECK6[EIP-712 签名验证]
```

---

## 📊 事件 (Events)

| 事件 | 触发时机 | 关键参数 |
|:---|:---|:---|
| `LogMake` | 订单创建成功 | orderKey, side, maker, price, nft |
| `LogCancel` | 订单取消成功 | orderKey, maker |
| `LogMatch` | 订单撮合成功 | sellOrderKey, buyOrderKey, fillPrice |
| `LogSkipOrder` | 订单操作跳过 | orderKey, salt |
| `BatchMatchInnerError` | 批量撮合错误 | offset, msg |
| `LogWithdrawETH` | 提取 ETH | recipient, amount |

---

## 🔧 管理功能

| 函数 | 权限 | 功能 |
|:---|:---|:---|
| `setVault` | onlyOwner | 设置金库合约地址 |
| `setProtocolShare` | onlyOwner | 设置协议费比例 |
| `withdrawETH` | onlyOwner | 提取协议手续费 |
| `pause` | onlyOwner | 暂停合约交易 |
| `unpause` | onlyOwner | 恢复合约交易 |

---

## 📈 Gas 优化

1. **批量操作**：`makeOrders`, `cancelOrders`, `editOrders`, `matchOrders` 支持批量处理
2. **Try 模式**：单个订单失败不影响批量中其他订单
3. **DelegateCall 批量撮合**：`matchOrders` 使用 delegatecall 避免多次退款
4. **存储间隙**：预留 50 个 slot 用于未来升级

---

## 🔗 合约依赖

```mermaid
graph LR
    ORDERBOOK[EasySwapOrderBook] --> VAULT[EasySwapVault]
    ORDERBOOK --> STORAGE[OrderStorage]
    ORDERBOOK --> VALIDATOR[OrderValidator]
    ORDERBOOK --> PROTOCOL[ProtocolManager]
    
    STORAGE --> RBTREE[RedBlackTreeLibrary]
    STORAGE --> LIBORDER[LibOrder]
    PROTOCOL --> LIBPAY[LibPayInfo]
    VALIDATOR --> EIP712[EIP712Upgradeable]
    
    style ORDERBOOK fill:#ff9800,color:#fff
    style VAULT fill:#4caf50,color:#fff
```

---

> 📝 **文档版本**: v1.0  
> 📅 **更新日期**: 2026-02-08  
> 📁 **源文件**: [EasySwapOrderBook.sol](./EasySwapContract/contracts/EasySwapOrderBook.sol)
