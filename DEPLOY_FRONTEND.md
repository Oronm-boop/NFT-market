# EasySwap 前端本地部署指南 (Windows)

## 一、环境要求

| 依赖 | 版本要求 | 说明 |
|------|---------|------|
| **Node.js** | >= 20 | [下载地址](https://nodejs.org/) |
| **pnpm** | >= 8 | 推荐包管理器 |

> 验证安装：
> ```powershell
> node -v     # 应输出 v20+
> pnpm -v     # 应输出 8+
> ```
>
> 安装 pnpm（如未安装）：
> ```powershell
> npm install -g pnpm
> ```

---

## 二、技术栈

| 技术 | 说明 |
|------|------|
| **Next.js 16** | React 全栈框架（App Router） |
| **React 19** | UI 库 |
| **TypeScript** | 类型安全 |
| **TailwindCSS 3** | 样式框架 |
| **RainbowKit + wagmi** | 钱包连接（MetaMask 等） |
| **viem / ethers** | 以太坊交互 |
| **axios** | HTTP 请求 |
| **i18next** | 国际化 |
| **recharts** | 图表 |

---

## 三、安装依赖

```powershell
cd nft-market-fe
pnpm install
```

---

## 四、关键配置

### 4.1 后端 API 代理

文件：`next.config.ts`

```typescript
async rewrites() {
  return [
    {
      source: "/api/v1/:path*",
      destination: "http://222.186.34.36:9988/api/v1/:path*", // ← 改成你的后端地址
    },
  ];
},
```

**本地部署时**，改为你本地的 Backend 地址：

```typescript
destination: "http://localhost:9000/api/v1/:path*",  // Backend 端口
```

> 前端所有 `/api/v1/*` 请求会被 Next.js 反向代理到 Backend。

### 4.2 钱包连接 & 链配置

文件：`config/wagmi.ts`

```typescript
export const config = getDefaultConfig({
  appName: 'RainbowKit App',
  projectId: '94066ab3be2718981f226c7407038ba4',  // WalletConnect Project ID
  chains: [mainnet, sepolia],                      // 支持的链
  transports: {
    [mainnet.id]: http('https://mainnet.infura.io/v3/YOUR_KEY'),
    [sepolia.id]: http('https://sepolia.infura.io/v3/YOUR_KEY'),  // ← RPC 节点
  },
  ssr: true,
});
```

| 配置项 | 说明 |
|--------|------|
| `projectId` | WalletConnect Cloud 的 Project ID，去 [cloud.walletconnect.com](https://cloud.walletconnect.com) 免费申请 |
| `chains` | 前端支持连接的链，当前为 mainnet + sepolia |
| `transports` | 各链的 RPC 节点地址 |

---

## 五、启动开发服务器

```powershell
pnpm dev
```

启动后访问：**http://localhost:3000**

> 开发模式使用 Turbopack（`next dev --turbopack`），热更新速度极快。

---

## 六、构建生产版本

```powershell
# 构建
pnpm build

# 启动生产服务器
pnpm start
```

> 构建产物使用 `output: "standalone"` 模式，生成独立可运行的 Node.js 服务。

---

## 七、项目目录结构

```
nft-market-fe/
├── app/              # Next.js App Router 页面
├── api/              # API 请求封装（axios）
├── components/       # 通用 UI 组件
├── config/           # wagmi 钱包配置
├── constants/        # 常量定义
├── contracts/        # 合约 ABI 和地址
├── hooks/            # 自定义 React Hooks
├── lib/              # 工具函数
├── public/           # 静态资源
├── scripts/          # 脚本
├── next.config.ts    # Next.js 配置（API 代理、构建选项）
├── tailwind.config.ts # TailwindCSS 配置
└── package.json      # 依赖和脚本
```

---

## 八、与后端的连接关系

```
┌──────────────────┐     反向代理      ┌──────────────────┐
│  前端 (Next.js)  │  ───────────────→ │  Backend API     │
│  localhost:3000   │  /api/v1/*        │  localhost:9000   │
└──────────────────┘                   └────────┬─────────┘
                                                │
                                       ┌────────▼─────────┐
                                       │  MySQL + Redis   │
                                       └──────────────────┘
```

1. 前端通过 `next.config.ts` 的 `rewrites` 将 `/api/v1/*` 代理到 Backend
2. 前端直接通过 wagmi/viem 与区块链交互（钱包签名、合约调用）
3. 后端负责提供 NFT 数据查询、订单信息等 API

---

## 九、常见问题

### Q1: `pnpm install` 很慢

设置国内镜像：
```powershell
pnpm config set registry https://registry.npmmirror.com
pnpm install
```

### Q2: 钱包连不上

- 确保浏览器安装了 MetaMask 扩展
- 检查 `config/wagmi.ts` 中的 `projectId` 是否有效
- 确保 RPC 节点可访问

### Q3: API 请求返回 502/504

- 确保 Backend 已启动并监听正确端口
- 检查 `next.config.ts` 中 `destination` 地址是否正确

### Q4: 构建报类型错误

项目已配置 `typescript.ignoreBuildErrors: true`，应该不会因类型错误阻断构建。如仍有问题，检查 Node.js 版本是否 >= 20。
