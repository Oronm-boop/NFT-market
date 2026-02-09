# EasySwapVault 合约解析

> 本文档对 `EasySwapVault.sol` 资产托管合约进行详细解析，包括合约结构、核心功能、资产流转等可视化说明。

---

## 📊 合约概览

`EasySwapVault` 是 NFT 订单簿交易系统的**资产托管合约（金库）**，负责在撮合前/撮合中安全托管用户的 ETH 与 NFT 资产。

### 核心职责

| 职责 | 说明 |
|:---|:---|
| 🔐 **权限隔离** | 仅允许 OrderBook 合约调用存取款 |
| 💰 **ETH 托管** | 按订单维度记录 ETH 余额（Bid 出价锁定） |
| 🖼️ **NFT 托管** | 按订单维度记录 NFT（List 挂单锁定） |
| ✏️ **订单编辑** | 支持改价时资产迁移（editETH/editNFT） |
| 🔄 **资产转移** | 支持直接 NFT 转账和批量转账 |

---

## 🏗️ 合约结构

```mermaid
graph TB
    subgraph "EasySwapVault"
        STATE["状态变量"]
        ETH_OPS["ETH 操作"]
        NFT_OPS["NFT 操作"]
        EDIT_OPS["编辑操作"]
        TRANSFER["转账操作"]
    end
    
    STATE --> |"orderBook"| ORDERBOOK[OrderBook 地址]
    STATE --> |"ETHBalance"| ETH_MAP["mapping: OrderKey → ETH"]
    STATE --> |"NFTBalance"| NFT_MAP["mapping: OrderKey → tokenId"]
    
    ETH_OPS --> DEPOSIT_ETH[depositETH]
    ETH_OPS --> WITHDRAW_ETH[withdrawETH]
    
    NFT_OPS --> DEPOSIT_NFT[depositNFT]
    NFT_OPS --> WITHDRAW_NFT[withdrawNFT]
    
    EDIT_OPS --> EDIT_ETH[editETH]
    EDIT_OPS --> EDIT_NFT[editNFT]
    
    TRANSFER --> TRANSFER_721[transferERC721]
    TRANSFER --> BATCH_721[batchTransferERC721]
    
    style STATE fill:#e3f2fd
    style ETH_OPS fill:#e8f5e9
    style NFT_OPS fill:#fff3e0
    style EDIT_OPS fill:#fce4ec
    style TRANSFER fill:#f3e5f5
```

---

## 🔐 权限控制

```mermaid
flowchart LR
    subgraph "调用者"
        USER[用户]
        ORDERBOOK[OrderBook 合约]
        OWNER[合约 Owner]
    end
    
    subgraph "Vault 函数"
        ADMIN["setOrderBook<br/>onlyOwner"]
        PROTECTED["depositETH<br/>withdrawETH<br/>depositNFT<br/>withdrawNFT<br/>editETH<br/>editNFT<br/>transferERC721<br/>onlyEasySwapOrderBook"]
        PUBLIC["batchTransferERC721<br/>balanceOf<br/>公开"]
    end
    
    OWNER --> ADMIN
    ORDERBOOK --> PROTECTED
    USER --> PUBLIC
    
    style ADMIN fill:#ffcdd2
    style PROTECTED fill:#c8e6c9
    style PUBLIC fill:#bbdefb
```

### 权限修饰符

```solidity
modifier onlyEasySwapOrderBook() {
    require(msg.sender == orderBook, "HV: only EasySwap OrderBook");
    _;
}
```

> ⚠️ 只有 OrderBook 合约可以操作托管资产，防止资产被任意转出。

---

## 💰 ETH 操作

### 数据结构

```solidity
// 按订单维度托管的 ETH 数量
mapping(OrderKey => uint256) public ETHBalance;
```

### 存取流程

```mermaid
sequenceDiagram
    participant 用户 as 🧑 用户
    participant OB as 📜 OrderBook
    participant Vault as 🏦 Vault
    
    Note over 用户,Vault: == 存入 ETH (Bid 出价时) ==
    用户->>OB: makeOrders + ETH
    OB->>Vault: depositETH(orderKey, ETHAmount)
    Vault->>Vault: ETHBalance[orderKey] += msg.value
    
    Note over 用户,Vault: == 提取 ETH (成交/取消时) ==
    用户->>OB: matchOrder / cancelOrders
    OB->>Vault: withdrawETH(orderKey, amount, to)
    Vault->>Vault: ETHBalance[orderKey] -= amount
    Vault->>用户: 转账 ETH
```

### depositETH

```solidity
function depositETH(OrderKey orderKey, uint256 ETHAmount) external payable onlyEasySwapOrderBook {
    require(msg.value >= ETHAmount, "HV: not match ETHAmount");
    ETHBalance[orderKey] += msg.value;
}
```

| 参数 | 说明 |
|:---|:---|
| `orderKey` | 订单唯一标识 |
| `ETHAmount` | 预期存入金额 |
| `msg.value` | 实际发送的 ETH |

### withdrawETH

```solidity
function withdrawETH(OrderKey orderKey, uint256 ETHAmount, address to) external onlyEasySwapOrderBook {
    ETHBalance[orderKey] -= ETHAmount;
    to.safeTransferETH(ETHAmount);
}
```

| 场景 | to 地址 |
|:---|:---|
| Bid 成交 → 卖家收款 | 卖家地址 |
| Bid 取消 → 退还买家 | 买家地址 |
| Bid 成交 → 协议费 | OrderBook 合约 |

---

## 🖼️ NFT 操作

### 数据结构

```solidity
// 按订单维度托管的 NFT tokenId
mapping(OrderKey => uint256) public NFTBalance;
```

### 存取流程

```mermaid
sequenceDiagram
    participant 卖家 as 🧑 卖家
    participant OB as 📜 OrderBook
    participant Vault as 🏦 Vault
    participant 买家 as 🧑 买家
    
    Note over 卖家,买家: == 存入 NFT (List 挂单时) ==
    卖家->>OB: makeOrders (List)
    OB->>Vault: depositNFT(orderKey, from, collection, tokenId)
    Vault->>卖家: transferFrom NFT
    Vault->>Vault: NFTBalance[orderKey] = tokenId
    
    Note over 卖家,买家: == 提取 NFT (成交/取消时) ==
    买家->>OB: matchOrder
    OB->>Vault: withdrawNFT(orderKey, to, collection, tokenId)
    Vault->>Vault: delete NFTBalance[orderKey]
    Vault->>买家: 转移 NFT
```

### depositNFT

```solidity
function depositNFT(
    OrderKey orderKey,
    address from,
    address collection,
    uint256 tokenId
) external onlyEasySwapOrderBook {
    IERC721(collection).safeTransferNFT(from, address(this), tokenId);
    NFTBalance[orderKey] = tokenId;
}
```

> 📌 前提：卖家需要先 `approve` Vault 合约

### withdrawNFT

```solidity
function withdrawNFT(
    OrderKey orderKey,
    address to,
    address collection,
    uint256 tokenId
) external onlyEasySwapOrderBook {
    require(NFTBalance[orderKey] == tokenId, "HV: not match tokenId");
    delete NFTBalance[orderKey];
    IERC721(collection).safeTransferNFT(address(this), to, tokenId);
}
```

| 场景 | to 地址 |
|:---|:---|
| List 成交 → 买家获得 NFT | 买家地址 |
| List 取消 → 退还卖家 NFT | 卖家地址 |

---

## ✏️ 编辑操作

### editETH - ETH 迁移

订单编辑时，将 ETH 从旧订单迁移到新订单：

```mermaid
flowchart TB
    START["editETH 调用"] --> CLEAR["清空旧订单: ETHBalance[old] = 0"]
    CLEAR --> COMPARE{新旧金额比较}
    
    COMPARE -->|"新价更低<br/>oldAmount > newAmount"| REFUND
    COMPARE -->|"新价更高<br/>oldAmount < newAmount"| ADD
    COMPARE -->|"金额相等"| SAME
    
    subgraph REFUND ["退款流程"]
        R1["ETHBalance[new] = newAmount"]
        R2["退回差额给用户"]
        R1 --> R2
    end
    
    subgraph ADD ["补款流程"]
        A1["验证 msg.value >= 差额"]
        A2["ETHBalance[new] = msg.value + oldAmount"]
        A1 --> A2
    end
    
    subgraph SAME ["金额相等"]
        S1["ETHBalance[new] = oldAmount"]
    end
    
    REFUND --> DONE([完成])
    ADD --> DONE
    SAME --> DONE
```

### 示例

| 场景 | 旧价格 | 新价格 | 操作 |
|:---|:---|:---|:---|
| 提高出价 | 1 ETH | 1.5 ETH | 用户补充 0.5 ETH |
| 降低出价 | 1 ETH | 0.8 ETH | 退还用户 0.2 ETH |
| 价格不变 | 1 ETH | 1 ETH | 仅迁移记录 |

### editNFT - NFT 迁移

订单编辑时，将 NFT 记录从旧订单迁移到新订单（NFT 本身不移动）：

```solidity
function editNFT(OrderKey oldOrderKey, OrderKey newOrderKey) external onlyEasySwapOrderBook {
    NFTBalance[newOrderKey] = NFTBalance[oldOrderKey];
    delete NFTBalance[oldOrderKey];
}
```

```
旧订单                    新订单
┌─────────────────┐      ┌─────────────────┐
│ oldOrderKey     │ ──▶  │ newOrderKey     │
│ tokenId: 42     │      │ tokenId: 42     │
└─────────────────┘      └─────────────────┘
      ❌ 删除                  ✅ 新增
      
      NFT 本身位置不变，只是关联到新的 orderKey
```

---

## 🔄 转账操作

### transferERC721 - 单笔转账

OrderBook 发起的 NFT 直接转账（如卖家接受 Bid 时，NFT 不在 Vault 中）：

```solidity
function transferERC721(address from, address to, LibOrder.Asset calldata assets) 
    external onlyEasySwapOrderBook {
    IERC721(assets.collection).safeTransferNFT(from, to, assets.tokenId);
}
```

### batchTransferERC721 - 批量转账

用户批量转移 NFT（公开函数，任何人可调用）：

```solidity
function batchTransferERC721(address to, LibOrder.NFTInfo[] calldata assets) external {
    for (uint256 i = 0; i < assets.length; ++i) {
        IERC721(assets[i].collection).safeTransferNFT(_msgSender(), to, assets[i].tokenId);
    }
}
```

> 💡 常用于批量上架时一次性将多个 NFT 转入 Vault

---

## 📊 完整资产流转

### List 挂单 → 成交

```mermaid
flowchart LR
    subgraph "挂单阶段"
        S1[卖家] -->|"NFT"| V1[Vault]
    end
    
    subgraph "成交阶段"
        V1 -->|"NFT"| B1[买家]
        B1 -->|"ETH"| OB[OrderBook]
        OB -->|"ETH - 手续费"| S1
    end
    
    style V1 fill:#4caf50,color:#fff
    style OB fill:#ff9800,color:#fff
```

### Bid 出价 → 接受

```mermaid
flowchart LR
    subgraph "出价阶段"
        B2[买家] -->|"ETH"| V2[Vault]
    end
    
    subgraph "接受阶段"
        V2 -->|"ETH"| OB2[OrderBook]
        OB2 -->|"ETH - 手续费"| S2[卖家]
        S2 -->|"NFT"| B2
    end
    
    style V2 fill:#4caf50,color:#fff
    style OB2 fill:#ff9800,color:#fff
```

---

## 🔧 其他功能

### onERC721Received

```solidity
function onERC721Received(address, address, uint256, bytes memory) public virtual returns (bytes4) {
    return this.onERC721Received.selector;
}
```

> 实现 ERC721 接收接口，使 Vault 能接收 `safeTransferFrom` 的 NFT

### receive

```solidity
receive() external payable {}
```

> 允许合约直接接收 ETH

### __gap

```solidity
uint256[50] private __gap;
```

> 可升级合约的存储间隙，为未来升级预留空间

---

## 📋 函数一览表

| 函数 | 权限 | 功能 |
|:---|:---|:---|
| `setOrderBook` | onlyOwner | 设置 OrderBook 地址 |
| `balanceOf` | 公开 | 查询订单托管余额 |
| `depositETH` | onlyOrderBook | 存入 ETH |
| `withdrawETH` | onlyOrderBook | 提取 ETH |
| `depositNFT` | onlyOrderBook | 存入 NFT |
| `withdrawNFT` | onlyOrderBook | 提取 NFT |
| `editETH` | onlyOrderBook | 编辑订单时迁移 ETH |
| `editNFT` | onlyOrderBook | 编辑订单时迁移 NFT |
| `transferERC721` | onlyOrderBook | 单笔 NFT 转账 |
| `batchTransferERC721` | 公开 | 批量 NFT 转账 |

---

## 🔐 安全设计

| 设计 | 说明 |
|:---|:---|
| **权限隔离** | 只有 OrderBook 可操作托管资产 |
| **按订单隔离** | 每个订单的资产独立记录，互不影响 |
| **安全转账** | 使用 safeTransferETH 和 safeTransferNFT |
| **可升级** | 预留 50 个存储槽位 |

---

> 📝 **文档版本**: v1.0  
> 📅 **更新日期**: 2026-02-09  
> 📁 **源文件**: [EasySwapVault.sol](./EasySwapContract/contracts/EasySwapVault.sol)
