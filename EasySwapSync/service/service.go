/**
 * service.go - EasySwapSync 服务管理器
 *
 * 功能：
 *   - 统一管理所有子服务的初始化和生命周期
 *   - 协调各组件之间的依赖关系
 *   - 提供服务启动入口
 *
 * 组件依赖关系：
 *   Config → Redis → DB → CollectionFilter → OrderManager → OrderBookIndexer
 *
 * 使用方式：
 *   service, err := service.New(ctx, cfg)
 *   service.Start()
 */
package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/ProjectsTask/EasySwapBase/chain"
	"github.com/ProjectsTask/EasySwapBase/chain/chainclient"
	"github.com/ProjectsTask/EasySwapBase/ordermanager"
	"github.com/ProjectsTask/EasySwapBase/stores/xkv"
	"github.com/pkg/errors"
	"github.com/zeromicro/go-zero/core/stores/cache"
	"github.com/zeromicro/go-zero/core/stores/kv"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"gorm.io/gorm"

	"github.com/ProjectsTask/EasySwapSync/service/orderbookindexer"

	"github.com/ProjectsTask/EasySwapSync/model"
	"github.com/ProjectsTask/EasySwapSync/service/collectionfilter"
	"github.com/ProjectsTask/EasySwapSync/service/config"
)

// Service 是 EasySwapSync 的核心服务管理器
// 负责协调所有子组件的工作
type Service struct {
	ctx              context.Context            // 全局上下文，用于控制服务生命周期
	config           *config.Config             // 配置信息
	kvStore          *xkv.Store                 // Redis 缓存，用于存储订单状态和临时数据
	db               *gorm.DB                   // 数据库连接，用于持久化订单数据
	wg               *sync.WaitGroup            // 等待组，用于优雅关闭
	collectionFilter *collectionfilter.Filter   // NFT 集合过滤器，维护需要追踪的集合白名单
	orderbookIndexer *orderbookindexer.Service  // 订单簿索引器，核心组件，负责同步链上事件
	orderManager     *ordermanager.OrderManager // 订单管理器，来自 EasySwapBase，管理订单生命周期
}

// New 创建并初始化 Service 实例
// 初始化顺序很重要：Redis → DB → CollectionFilter → ChainClient → OrderBookIndexer
//
// @param ctx: 上下文，用于控制服务生命周期
// @param cfg: 配置对象，包含所有必要的配置信息
// @return: Service 实例和可能的错误
func New(ctx context.Context, cfg *config.Config) (*Service, error) {
	// ========== 1. 初始化 Redis 缓存 ==========
	// 将配置转换为 go-zero 的 KvConf 格式
	var kvConf kv.KvConf
	for _, con := range cfg.Kv.Redis {
		kvConf = append(kvConf, cache.NodeConf{
			RedisConf: redis.RedisConf{
				Host: con.Host, // Redis 地址，如 "localhost:6379"
				Type: con.Type, // 连接类型，"node" 或 "cluster"
				Pass: con.Pass, // 密码（可选）
			},
			Weight: 10, // 节点权重，用于负载均衡
		})
	}
	// 创建 Redis KV 存储实例
	kvStore := xkv.NewStore(kvConf)

	// ========== 2. 初始化数据库连接 ==========
	var err error
	db := model.NewDB(cfg.DB)

	// ========== 3. 初始化集合过滤器 ==========
	// 用于过滤需要追踪的 NFT 集合
	// 只有在白名单中的集合才会被同步
	collectionFilter := collectionfilter.New(ctx, db, cfg.ChainCfg.Name, cfg.ProjectCfg.Name)

	// ========== 4. 初始化订单管理器 ==========
	// 来自 EasySwapBase 共享库，负责订单的增删改查和状态管理
	orderManager := ordermanager.New(ctx, db, kvStore, cfg.ChainCfg.Name, cfg.ProjectCfg.Name)

	// ========== 5. 初始化区块链客户端 ==========
	var orderbookSyncer *orderbookindexer.Service
	var chainClient chainclient.ChainClient

	// 打印 RPC URL 用于调试
	fmt.Println("chainClient url:" + cfg.AnkrCfg.HttpsUrl + cfg.AnkrCfg.ApiKey)

	// 创建 EVM 链客户端，用于与区块链交互
	// 支持的链：ETH (1), Optimism (10), Sepolia (11155111)
	chainClient, err = chainclient.New(int(cfg.ChainCfg.ID), cfg.AnkrCfg.HttpsUrl+cfg.AnkrCfg.ApiKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed on create evm client")
	}

	// ========== 6. 根据链 ID 创建订单簿索引器 ==========
	// 不同的链可能有不同的索引逻辑（但目前都使用相同的实现）
	switch cfg.ChainCfg.ID {
	case chain.EthChainID, chain.OptimismChainID, chain.SepoliaChainID:
		// 创建订单簿索引器，这是核心组件
		// 负责监听链上事件（LogMake, LogCancel, LogMatch）并同步到数据库
		orderbookSyncer = orderbookindexer.New(
			ctx,
			cfg,
			db,
			kvStore,
			chainClient,
			cfg.ChainCfg.ID,
			cfg.ChainCfg.Name,
			orderManager,
		)
	}

	// 检查错误（这里的 err 理论上不会被设置，因为上面的 switch 没有返回错误）
	if err != nil {
		return nil, errors.Wrap(err, "failed on create trade info server")
	}

	// ========== 7. 组装 Service ==========
	manager := Service{
		ctx:              ctx,
		config:           cfg,
		db:               db,
		kvStore:          kvStore,
		collectionFilter: collectionFilter,
		orderbookIndexer: orderbookSyncer,
		orderManager:     orderManager,
		wg:               &sync.WaitGroup{},
	}

	return &manager, nil
}

// Start 启动所有子服务
// 启动顺序很重要：
//  1. 先预加载 NFT 集合白名单（必须在同步之前完成）
//  2. 启动订单簿索引器（开始同步链上事件）
//  3. 启动订单管理器（开始处理订单状态）
//
// @return: 启动过程中的错误
func (s *Service) Start() error {
	// ========== 1. 预加载 NFT 集合白名单 ==========
	// 注意：不要移动这个调用的位置！
	// 必须在启动索引器之前完成，否则可能会遗漏事件
	if err := s.collectionFilter.PreloadCollections(); err != nil {
		return errors.Wrap(err, "failed on preload collection to filter")
	}

	// ========== 2. 启动订单簿索引器 ==========
	// 这会启动后台 goroutine，持续监听链上事件
	// 包括：SyncOrderBookEventLoop（事件同步）和 UpKeepingCollectionFloorChangeLoop（地板价更新）
	s.orderbookIndexer.Start()

	// ========== 3. 启动订单管理器 ==========
	// 处理订单状态变更、过期检查等
	s.orderManager.Start()

	return nil
}
