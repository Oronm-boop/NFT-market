# EasySwap 后端本地部署指南 (Windows)

## 一、环境要求

| 依赖 | 版本要求 | 说明 |
|------|---------|------|
| **Go** | >= 1.21 | [下载地址](https://go.dev/dl/) |
| **MySQL** | >= 5.7 | 推荐使用 [MySQL Installer](https://dev.mysql.com/downloads/installer/) |
| **Redis** | >= 6.0 | Windows 可使用 [Memurai](https://www.memurai.com/) 或 Docker 版 Redis |
| **Git** | 任意 | 用于拉取代码 |

> 验证安装：
> ```powershell
> go version        # 应输出 go1.21+
> mysql --version   # 应输出 MySQL 版本
> redis-cli ping    # 应输出 PONG
> ```

---

## 二、项目结构

```
ProjectBreakdown-NFTMarket/
├── EasySwapBase/       # 基础库 (被 Backend 和 Sync 依赖)
├── EasySwapBackend/    # API 服务 (本文档部署目标)
├── EasySwapSync/       # 链上事件索引器
└── EasySwapContract/   # 智能合约
```

> `go.mod` 中使用了 `replace` 指令引用本地的 `EasySwapBase`，因此**三个项目必须在同一父目录下**。

---

## 三、数据库初始化

### 3.1 创建数据库

```sql
CREATE DATABASE easyswap CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER 'easyuser'@'localhost' IDENTIFIED BY 'easypasswd';
GRANT ALL PRIVILEGES ON easyswap.* TO 'easyuser'@'localhost';
FLUSH PRIVILEGES;
```

### 3.2 建表

如果项目中有 migration 脚本，执行即可。否则表会由 GORM AutoMigrate 自动创建（取决于代码实现）。

---

## 四、Redis 配置

Windows 上推荐方案（任选其一）：

**方案 A：Memurai（Windows 原生 Redis 兼容）**
```powershell
# 安装后默认监听 127.0.0.1:6379，无密码
# 下载地址: https://www.memurai.com/get-memurai
```

**方案 B：Docker Desktop 运行 Redis**
```powershell
docker run -d --name redis -p 6379:6379 redis:7
```

---

## 五、配置文件

### 5.1 复制配置模板

```powershell
cd EasySwapBackend
copy config\config.toml.example config\config.toml
```

### 5.2 修改 `config/config.toml`

根据本地环境修改以下关键配置：

```toml
[project_cfg]
name = "EasySwap"

[api]
port = ":9000"          # API 监听端口
max_num = 500

[log]
level = "info"
mode = "console"        # 开发环境用 console，生产用 file
path = "logs/backend"
service_name = "easyswap-backend"

# ========== Redis ==========
[[kv.redis]]
pass = ""               # Redis 密码，无密码留空
host = "127.0.0.1:6379"
type = "node"

# ========== MySQL ==========
[db]
host = "127.0.0.1"
port = 3306
user = "easyuser"       # ← 改成你的用户名
password = "easypasswd" # ← 改成你的密码
database = "easyswap"   # ← 改成你的数据库名
max_open_conns = 100
max_idle_conns = 10
max_conn_max_lifetime = 300
log_level = "info"

# ========== 链配置 ==========
[[chain_supported]]
name = "sepolia"
chain_id = 11155111
endpoint = "https://rpc.ankr.com/eth_sepolia"  # ← 替换为你的 RPC 节点

# ========== 元数据解析 ==========
[metadata_parse]
name_tags = ["name", "title"]
image_tags = ["image", "image_url", "animation_url", "media_url"]
attributes_tags = ["attributes", "properties", "attribute"]
trait_name_tags = ["trait_type"]
trait_value_tags = ["value"]

# ========== COS (可选) ==========
[cos]
secret_id = "YOUR_SECRET_ID"
secret_key = "YOUR_SECRET_KEY"
bucket = "nft-assets"
region = "ap-beijing"
app_id = "1234567890"

# ========== MetaNode (可选) ==========
[metanode]
owner_private_key = "YOUR_PRIVATE_KEY"
gas_limit = 300000
gas_price = "20000000000"

[metanode.contract_addresses]
"11155111" = "0xYOUR_CONTRACT_ADDRESS"

[metanode.rpc_endpoints]
"11155111" = "https://sepolia.infura.io/v3/YOUR_PROJECT_ID"
```

---

## 六、构建与运行

### 6.1 下载依赖

```powershell
cd EasySwapBackend
go mod tidy
```

### 6.2 方式一：直接运行（开发模式）

```powershell
cd src
go run main.go -conf ../config/config.toml
```

### 6.3 方式二：编译后运行（推荐）

```powershell
# 编译
go build -ldflags="-s -w" -o bin/easyswap-backend.exe ./src/main.go

# 运行
.\bin\easyswap-backend.exe -conf .\config\config.toml
```

### 6.4 验证服务

```powershell
# 检查服务是否启动
curl http://localhost:9000/api/v1/collections/ranking

# 或用浏览器访问
# http://localhost:9000/api/v1/collections/ranking
```

---

## 七、同时部署 EasySwapSync（索引器）

如果需要从链上同步数据，还需部署 `EasySwapSync`：

```powershell
cd EasySwapSync

# 复制并修改配置
# copy config\config.toml.example config\config.toml

# 运行
go run main.go daemon -conf config/config.toml
```

> EasySwapSync 负责写入数据，EasySwapBackend 负责读取数据。两者通过 MySQL + Redis 解耦。

---

## 八、常见问题

### Q1: `go mod tidy` 报错找不到 EasySwapBase

确保目录结构正确，`EasySwapBase` 和 `EasySwapBackend` 在同一父目录下：
```
ProjectBreakdown-NFTMarket/
├── EasySwapBase/     ← 必须存在
└── EasySwapBackend/
```

### Q2: 连接 MySQL 失败

检查 MySQL 是否启动：
```powershell
# 检查 MySQL 服务状态
Get-Service -Name "MySQL*"

# 启动 MySQL
Start-Service -Name "MySQL80"
```

### Q3: Redis 连接被拒绝

Windows 上 Redis 需要额外安装。检查 Redis 是否运行：
```powershell
redis-cli ping
# 应返回 PONG
```

### Q4: 端口被占用

修改 `config.toml` 中的 `[api] port` 字段，例如改为 `:8080`。

### Q5: RPC 节点限速

免费 RPC 节点有请求频率限制，建议：
- 使用 [Alchemy](https://www.alchemy.com/) 或 [Infura](https://infura.io/) 的免费 API Key
- 或自建节点

---

## 九、服务架构一览

```
                    ┌──────────────┐
                    │   Frontend   │
                    └──────┬───────┘
                           │ HTTP
                    ┌──────▼───────┐
                    │  Backend API │ ← 本文档部署目标
                    │  :9000       │
                    └──┬───────┬───┘
                       │       │
                 ┌─────▼──┐ ┌──▼─────┐
                 │ MySQL  │ │ Redis  │
                 │ :3306  │ │ :6379  │
                 └─────▲──┘ └──▲─────┘
                       │       │
                    ┌──┴───────┴───┐
                    │  Sync 索引器  │ ← 可选部署
                    └──────┬───────┘
                           │ RPC
                    ┌──────▼───────┐
                    │   区块链节点   │
                    └──────────────┘
```
