/**
 * orderbookindexer 包 - 订单簿索引服务
 *
 * 功能：
 *   - 监听链上 EasySwapOrderBook 合约事件（Make, Match, Cancel）
 *   - 将链上事件同步到数据库（Orders, Activities, Items）
 *   - 维护 NFT 集合地板价
 *   - 处理区块链分叉（Reorg）
 */
package orderbookindexer

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/ProjectsTask/EasySwapBase/chain/chainclient"
	"github.com/ProjectsTask/EasySwapBase/chain/types"
	"github.com/ProjectsTask/EasySwapBase/logger/xzap"
	"github.com/ProjectsTask/EasySwapBase/ordermanager"
	"github.com/ProjectsTask/EasySwapBase/stores/gdb"
	"github.com/ProjectsTask/EasySwapBase/stores/gdb/orderbookmodel/base"
	"github.com/ProjectsTask/EasySwapBase/stores/gdb/orderbookmodel/multi"
	"github.com/ProjectsTask/EasySwapBase/stores/xkv"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethereumTypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	"github.com/shopspring/decimal"
	"github.com/zeromicro/go-zero/core/threading"
	"go.uber.org/zap"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/ProjectsTask/EasySwapSync/service/comm"
	"github.com/ProjectsTask/EasySwapSync/service/config"
)

const (
	EventIndexType  = 6   // 索引类型：6 代表订单簿事件
	SleepInterval   = 10  // 轮询间隔：10秒
	SyncBlockPeriod = 100 // 同步步长：每次请求 100 个区块（提高同步效率）

	// 链上事件 Topic 签名（Keccak256）
	// LogMake: 创建订单
	LogMakeTopic = "0xfc37f2ff950f95913eb7182357ba3c14df60ef354bc7d6ab1ba2815f249fffe6"
	// LogCancel: 取消订单
	LogCancelTopic = "0x0ac8bb53fac566d7afc05d8b4df11d7690a7b27bdc40b54e4060f9b21fb849bd"
	// LogMatch: 订单成交
	LogMatchTopic = "0xf629aecab94607bc43ce4aebd564bf6e61c7327226a797b002de724b9944b20e"
	// ERC721 Approval: 授权事件
	ERC721ApprovalTopic = "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"
	contractAbi         = `[{"inputs":[],"name":"CannotFindNextEmptyKey","type":"error"},{"inputs":[],"name":"CannotFindPrevEmptyKey","type":"error"},{"inputs":[{"internalType":"OrderKey","name":"orderKey","type":"bytes32"}],"name":"CannotInsertDuplicateOrder","type":"error"},{"inputs":[],"name":"CannotInsertEmptyKey","type":"error"},{"inputs":[],"name":"CannotInsertExistingKey","type":"error"},{"inputs":[],"name":"CannotRemoveEmptyKey","type":"error"},{"inputs":[],"name":"CannotRemoveMissingKey","type":"error"},{"inputs":[],"name":"EnforcedPause","type":"error"},{"inputs":[],"name":"ExpectedPause","type":"error"},{"inputs":[],"name":"InvalidInitialization","type":"error"},{"inputs":[],"name":"NotInitializing","type":"error"},{"inputs":[{"internalType":"address","name":"owner","type":"address"}],"name":"OwnableInvalidOwner","type":"error"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"OwnableUnauthorizedAccount","type":"error"},{"inputs":[],"name":"ReentrancyGuardReentrantCall","type":"error"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint256","name":"offset","type":"uint256"},{"indexed":false,"internalType":"bytes","name":"msg","type":"bytes"}],"name":"BatchMatchInnerError","type":"event"},{"anonymous":false,"inputs":[],"name":"EIP712DomainChanged","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"uint64","name":"version","type":"uint64"}],"name":"Initialized","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"OrderKey","name":"orderKey","type":"bytes32"},{"indexed":true,"internalType":"address","name":"maker","type":"address"}],"name":"LogCancel","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"OrderKey","name":"orderKey","type":"bytes32"},{"indexed":true,"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"indexed":true,"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"indexed":true,"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"indexed":false,"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"indexed":false,"internalType":"Price","name":"price","type":"uint128"},{"indexed":false,"internalType":"uint64","name":"expiry","type":"uint64"},{"indexed":false,"internalType":"uint64","name":"salt","type":"uint64"}],"name":"LogMake","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"OrderKey","name":"makeOrderKey","type":"bytes32"},{"indexed":true,"internalType":"OrderKey","name":"takeOrderKey","type":"bytes32"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"indexed":false,"internalType":"structLibOrder.Order","name":"makeOrder","type":"tuple"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"indexed":false,"internalType":"structLibOrder.Order","name":"takeOrder","type":"tuple"},{"indexed":false,"internalType":"uint128","name":"fillPrice","type":"uint128"}],"name":"LogMatch","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"OrderKey","name":"orderKey","type":"bytes32"},{"indexed":false,"internalType":"uint64","name":"salt","type":"uint64"}],"name":"LogSkipOrder","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"uint128","name":"newProtocolShare","type":"uint128"}],"name":"LogUpdatedProtocolShare","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"recipient","type":"address"},{"indexed":false,"internalType":"uint256","name":"amount","type":"uint256"}],"name":"LogWithdrawETH","type":"event"},{"anonymous":false,"inputs":[{"indexed":true,"internalType":"address","name":"previousOwner","type":"address"},{"indexed":true,"internalType":"address","name":"newOwner","type":"address"}],"name":"OwnershipTransferred","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"account","type":"address"}],"name":"Paused","type":"event"},{"anonymous":false,"inputs":[{"indexed":false,"internalType":"address","name":"account","type":"address"}],"name":"Unpaused","type":"event"},{"inputs":[{"internalType":"OrderKey[]","name":"orderKeys","type":"bytes32[]"}],"name":"cancelOrders","outputs":[{"internalType":"bool[]","name":"successes","type":"bool[]"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"components":[{"internalType":"OrderKey","name":"oldOrderKey","type":"bytes32"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"newOrder","type":"tuple"}],"internalType":"structLibOrder.EditDetail[]","name":"editDetails","type":"tuple[]"}],"name":"editOrders","outputs":[{"internalType":"OrderKey[]","name":"newOrderKeys","type":"bytes32[]"}],"stateMutability":"payable","type":"function"},{"inputs":[],"name":"eip712Domain","outputs":[{"internalType":"bytes1","name":"fields","type":"bytes1"},{"internalType":"string","name":"name","type":"string"},{"internalType":"string","name":"version","type":"string"},{"internalType":"uint256","name":"chainId","type":"uint256"},{"internalType":"address","name":"verifyingContract","type":"address"},{"internalType":"bytes32","name":"salt","type":"bytes32"},{"internalType":"uint256[]","name":"extensions","type":"uint256[]"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"OrderKey","name":"","type":"bytes32"}],"name":"filledAmount","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"}],"name":"getBestOrder","outputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"orderResult","type":"tuple"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"collection","type":"address"},{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"}],"name":"getBestPrice","outputs":[{"internalType":"Price","name":"price","type":"uint128"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"collection","type":"address"},{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"Price","name":"price","type":"uint128"}],"name":"getNextBestPrice","outputs":[{"internalType":"Price","name":"nextBestPrice","type":"uint128"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"uint256","name":"count","type":"uint256"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"OrderKey","name":"firstOrderKey","type":"bytes32"}],"name":"getOrders","outputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order[]","name":"resultOrders","type":"tuple[]"},{"internalType":"OrderKey","name":"nextOrderKey","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"uint128","name":"newProtocolShare","type":"uint128"},{"internalType":"address","name":"newVault","type":"address"},{"internalType":"string","name":"EIP712Name","type":"string"},{"internalType":"string","name":"EIP712Version","type":"string"}],"name":"initialize","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order[]","name":"newOrders","type":"tuple[]"}],"name":"makeOrders","outputs":[{"internalType":"OrderKey[]","name":"newOrderKeys","type":"bytes32[]"}],"stateMutability":"payable","type":"function"},{"inputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"sellOrder","type":"tuple"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"buyOrder","type":"tuple"}],"name":"matchOrder","outputs":[],"stateMutability":"payable","type":"function"},{"inputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"sellOrder","type":"tuple"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"buyOrder","type":"tuple"},{"internalType":"uint256","name":"msgValue","type":"uint256"}],"name":"matchOrderWithoutPayback","outputs":[{"internalType":"uint128","name":"costValue","type":"uint128"}],"stateMutability":"payable","type":"function"},{"inputs":[{"components":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"sellOrder","type":"tuple"},{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"buyOrder","type":"tuple"}],"internalType":"structLibOrder.MatchDetail[]","name":"matchDetails","type":"tuple[]"}],"name":"matchOrders","outputs":[{"internalType":"bool[]","name":"successes","type":"bool[]"}],"stateMutability":"payable","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"enumLibOrder.Side","name":"","type":"uint8"},{"internalType":"Price","name":"","type":"uint128"}],"name":"orderQueues","outputs":[{"internalType":"OrderKey","name":"head","type":"bytes32"},{"internalType":"OrderKey","name":"tail","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"OrderKey","name":"","type":"bytes32"}],"name":"orders","outputs":[{"components":[{"internalType":"enumLibOrder.Side","name":"side","type":"uint8"},{"internalType":"enumLibOrder.SaleKind","name":"saleKind","type":"uint8"},{"internalType":"address","name":"maker","type":"address"},{"components":[{"internalType":"uint256","name":"tokenId","type":"uint256"},{"internalType":"address","name":"collection","type":"address"},{"internalType":"uint96","name":"amount","type":"uint96"}],"internalType":"structLibOrder.Asset","name":"nft","type":"tuple"},{"internalType":"Price","name":"price","type":"uint128"},{"internalType":"uint64","name":"expiry","type":"uint64"},{"internalType":"uint64","name":"salt","type":"uint64"}],"internalType":"structLibOrder.Order","name":"order","type":"tuple"},{"internalType":"OrderKey","name":"next","type":"bytes32"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"owner","outputs":[{"internalType":"address","name":"","type":"address"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"pause","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"paused","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"view","type":"function"},{"inputs":[{"internalType":"address","name":"","type":"address"},{"internalType":"enumLibOrder.Side","name":"","type":"uint8"}],"name":"priceTrees","outputs":[{"internalType":"Price","name":"root","type":"uint128"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"protocolShare","outputs":[{"internalType":"uint128","name":"","type":"uint128"}],"stateMutability":"view","type":"function"},{"inputs":[],"name":"renounceOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"uint128","name":"newProtocolShare","type":"uint128"}],"name":"setProtocolShare","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newVault","type":"address"}],"name":"setVault","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"newOwner","type":"address"}],"name":"transferOwnership","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[],"name":"unpause","outputs":[],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"recipient","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"withdrawETH","outputs":[],"stateMutability":"nonpayable","type":"function"},{"stateMutability":"payable","type":"receive"}]`
	FixForCollection    = 0
	FixForItem          = 1
	List                = 0
	Bid                 = 1

	HexPrefix   = "0x"
	ZeroAddress = "0x0000000000000000000000000000000000000000"
)

type Order struct {
	Side     uint8
	SaleKind uint8
	Maker    common.Address
	Nft      struct {
		TokenId        *big.Int
		CollectionAddr common.Address
		Amount         *big.Int
	}
	Price  *big.Int
	Expiry uint64
	Salt   uint64
}

type Service struct {
	ctx          context.Context
	cfg          *config.Config
	db           *gorm.DB
	kv           *xkv.Store
	orderManager *ordermanager.OrderManager
	chainClient  chainclient.ChainClient
	chainId      int64
	chain        string
	parsedAbi    abi.ABI
	vaultAddress string
}

var MultiChainMaxBlockDifference = map[string]uint64{
	"eth":      8,
	"optimism": 8,

	"base":    8,
	"sepolia": 8, // Sepolia 测试网可能需要更多确认

}

func New(ctx context.Context, cfg *config.Config, db *gorm.DB, xkv *xkv.Store, chainClient chainclient.ChainClient, chainId int64, chain string, orderManager *ordermanager.OrderManager) *Service {
	parsedAbi, _ := abi.JSON(strings.NewReader(contractAbi)) // 通过ABI实例化
	return &Service{
		ctx:          ctx,
		cfg:          cfg,
		db:           db,
		kv:           xkv,
		chainClient:  chainClient,
		orderManager: orderManager,
		chain:        chain,
		chainId:      chainId,
		parsedAbi:    parsedAbi,
		vaultAddress: cfg.ContractCfg.VaultAddress,
	}
}

func (s *Service) Start() {
	threading.GoSafe(s.SyncOrderBookEventLoop)
	threading.GoSafe(s.UpKeepingCollectionFloorChangeLoop)
}

// SyncOrderBookEventLoop 订单簿事件同步主循环
// 持续监听链上事件并同步到数据库
func (s *Service) SyncOrderBookEventLoop() {
	var indexedStatus base.IndexedStatus
	// 1. 获取上次同步进度
	// 从 indexed_status 表中读取最后一次成功同步的区块高度
	// 如果服务重启，将从这个高度继续，防止重复或遗漏
	if err := s.db.WithContext(s.ctx).Table(base.IndexedStatusTableName()).
		Where("chain_id = ? and index_type = ?", s.chainId, EventIndexType).
		First(&indexedStatus).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed on get listing index status",
			zap.Error(err))
		return
	}

	lastSyncBlock := uint64(indexedStatus.LastIndexedBlock)
	for {
		select {
		case <-s.ctx.Done():
			xzap.WithContext(s.ctx).Info("SyncOrderBookEventLoop stopped due to context cancellation")
			return
		default:
		}

		// 2. 获取当前链上最新区块高度
		currentBlockNum, err := s.chainClient.BlockNumber() // 以轮询的方式获取当前区块高度
		if err != nil {
			xzap.WithContext(s.ctx).Error("failed on get current block number", zap.Error(err))
			time.Sleep(SleepInterval * time.Second)
			continue
		}

		// 3. 检查是否需要等待（防止超过当前高度）
		// MultiChainMaxBlockDifference 用于防止同步到未确认的区块（特别是 Reorg 风险）
		// 如果落后于最新区块不足一定数量（如 ETH 是 8 个区块），则等待
		if lastSyncBlock > currentBlockNum-MultiChainMaxBlockDifference[s.chain] { // 如果上次同步的区块高度大于当前区块高度，等待一段时间后再次轮询
			time.Sleep(SleepInterval * time.Second)
			continue
		}

		// 4. 计算本次同步的区块范围 [startBlock, endBlock]
		startBlock := lastSyncBlock
		endBlock := startBlock + SyncBlockPeriod
		if endBlock > currentBlockNum-MultiChainMaxBlockDifference[s.chain] { // 如果结束区块高度大于当前区块高度，将结束区块高度设置为当前区块高度
			endBlock = currentBlockNum - MultiChainMaxBlockDifference[s.chain]
		}

		query := types.FilterQuery{
			FromBlock: new(big.Int).SetUint64(startBlock),
			ToBlock:   new(big.Int).SetUint64(endBlock),
			Addresses: []string{s.cfg.ContractCfg.DexAddress},
		}

		// 5. 调用 RPC 获取日志
		// FilterLogs: 根据查询条件向节点请求日志数据
		// 这里会获取 [startBlock, endBlock] 范围内的所有符合条件（Contract Address）的日志
		logs, err := s.chainClient.FilterLogs(s.ctx, query) //同时获取多个（SyncBlockPeriod）区块的日志
		if err != nil {
			xzap.WithContext(s.ctx).Error("failed on get log",
				zap.Error(err),
				zap.Uint64("start_block", startBlock),
				zap.Uint64("end_block", endBlock),
				zap.Uint64("block_range", endBlock-startBlock+1))

			// 如果是因为请求范围太大导致的错误，尝试减少批量大小
			if endBlock-startBlock > 1 {
				xzap.WithContext(s.ctx).Warn("reducing block range due to RPC error",
					zap.Uint64("original_range", endBlock-startBlock+1),
					zap.Uint64("new_range", 1))
				// 只处理单个区块
				endBlock = startBlock
				query.ToBlock = new(big.Int).SetUint64(endBlock)

				// 重试单个区块
				logs, err = s.chainClient.FilterLogs(s.ctx, query)
				if err != nil {
					xzap.WithContext(s.ctx).Error("failed on get log even with single block",
						zap.Error(err),
						zap.Uint64("block", startBlock))
					time.Sleep(SleepInterval * time.Second)
					continue
				}
			} else {
				time.Sleep(SleepInterval * time.Second)
				continue
			}
		}

		// 6. 遍历并处理日志
		// 对于每个获取到的日志，根据其 Topic[0] (事件签名) 分发给不同的处理函数
		for _, log := range logs { // 遍历日志，根据不同的topic处理不同的事件
			ethLog := log.(ethereumTypes.Log)
			switch ethLog.Topics[0].String() {
			case LogMakeTopic:
				s.handleMakeEvent(ethLog) // 处理挂单 (Listing/Bid)
			case LogCancelTopic:
				s.handleCancelEvent(ethLog) // 处理取消
			case LogMatchTopic:
				s.handleMatchEvent(ethLog) // 处理成交
			case ERC721ApprovalTopic:
				s.handleApprovalEvent(ethLog) // 处理授权
			default:
				// 忽略其他未关注的事件
			}
		}

		// 7. 更新同步进度到数据库
		// 处理完一批区块后，更新 indexed_status 表，标记这批区块已处理完成
		// 下次循环将从 endBlock + 1 开始
		lastSyncBlock = endBlock + 1 // 更新最后同步的区块高度
		if err := s.db.WithContext(s.ctx).Table(base.IndexedStatusTableName()).
			Where("chain_id = ? and index_type = ?", s.chainId, EventIndexType).
			Update("last_indexed_block", lastSyncBlock).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed on update orderbook event sync block number",
				zap.Error(err))
			return
		}

		xzap.WithContext(s.ctx).Info("sync orderbook event ...",
			zap.Uint64("start_block", startBlock),
			zap.Uint64("end_block", endBlock))
	}
}

// 处理挂单事件 (LogMake)
// 当用户在链上创建订单时触发
func (s *Service) handleMakeEvent(log ethereumTypes.Log) {
	// 1. 检查区块链分叉 (Reorg)
	// 如果发现当前事件所在的交易哈希已经存在，但区块高度不同，说明发生了分叉
	if err := s.checkAndHandleFork(log.BlockNumber, log.TxHash.String()); err != nil {
		xzap.WithContext(s.ctx).Error("failed to handle fork for make event",
			zap.Error(err),
			zap.Uint64("block_number", log.BlockNumber),
			zap.String("tx_hash", log.TxHash.String()))
		return
	}

	// 定义用于解析 LogMake 事件非索引参数的匿名结构体
	// 必须与合约 ABI 中的 event 定义严格匹配
	var event struct {
		OrderKey [32]byte // 订单唯一标识符
		Nft      struct { // 嵌套结构体，对应 solidity 中的 struct Asset
			TokenId        *big.Int       // NFT Token ID
			CollectionAddr common.Address // NFT 合约地址
			Amount         *big.Int       // 数量 (ERC721 为 1, ERC1155 可能大于 1)
		}
		Price  *big.Int // 价格 (Wei)
		Expiry uint64   // 过期时间戳
		Salt   uint64   // 随机盐值，用于防止哈希冲突
	}

	// 2. 解析事件日志数据
	// 使用 ABI Unpack 将日志数据解析到 event 结构体中
	err := s.parsedAbi.UnpackIntoInterface(&event, "LogMake", log.Data) // 通过ABI解析日志数据
	if err != nil {
		xzap.WithContext(s.ctx).Error("Error unpacking LogMake event:", zap.Error(err))
		return
	}
	// 3. 提取 Indexed 字段 (Topic 1, 2, 3)
	// Topic 0 是事件签名，Topic 1-3 是 indexed 参数
	// 这些参数不包含在 log.Data 中，必须从 Topics 中解析
	side := uint8(new(big.Int).SetBytes(log.Topics[1].Bytes()).Uint64())     // Topic 1: 买单/卖单 (0: Listing, 1: Bid)
	saleKind := uint8(new(big.Int).SetBytes(log.Topics[2].Bytes()).Uint64()) // Topic 2: 销售类型 (0: FixPrice, 1: Auction)
	maker := common.BytesToAddress(log.Topics[3].Bytes())                    // Topic 3: 挂单者地址

	// 4. 确定订单类型
	var orderType int64
	if side == Bid { // 买单 (Offer)
		if saleKind == FixForCollection { // 针对集合的买单 (Collection Offer)
			orderType = multi.CollectionBidOrder
		} else { // 针对某个具体NFT的买单 (Item Offer)
			orderType = multi.ItemBidOrder
		}
	} else { // 卖单 (Listing)
		orderType = multi.ListingOrder
	}
	// 5. 创建订单对象并保存到数据库
	// 将链上事件数据转换为数据库模型
	newOrder := multi.Order{
		CollectionAddress: event.Nft.CollectionAddr.String(),
		MarketplaceId:     multi.MarketOrderBook, // 标识来自自营市场
		TokenId:           event.Nft.TokenId.String(),
		OrderID:           HexPrefix + hex.EncodeToString(event.OrderKey[:]), // OrderKey 转换为十六进制字符串作为 ID
		OrderStatus:       multi.OrderStatusActive,                           // 初始状态为 Active
		EventTime:         time.Now().Unix(),
		ExpireTime:        int64(event.Expiry),
		CurrencyAddress:   s.cfg.ContractCfg.EthAddress, // 目前仅支持 ETH 支付
		Price:             decimal.NewFromBigInt(event.Price, 0),
		Maker:             maker.String(),
		Taker:             ZeroAddress,              // 尚未成交，Taker 为空
		QuantityRemaining: event.Nft.Amount.Int64(), // 剩余可交易数量
		Size:              event.Nft.Amount.Int64(), // 订单总数量
		OrderType:         orderType,
		Salt:              int64(event.Salt),
	}
	if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&newOrder).Error; err != nil { // 将订单信息存入数据库
		xzap.WithContext(s.ctx).Error("failed on create order",
			zap.Error(err))
	}

	// 6. 更新或创建 NFT Item 信息
	// 无论是否存在，都尝试更新 Item 信息（主要是为了确保数据库中有这个 Item）
	newItem := multi.Item{
		CollectionAddress: event.Nft.CollectionAddr.String(),
		TokenId:           event.Nft.TokenId.String(),
		Owner:             maker.String(), // 既然能挂单，说明 Maker 大概率是 Owner (或者有授权)
		Supply:            event.Nft.Amount.Int64(),
		ListPrice:         decimal.NewFromBigInt(event.Price, 0), // 更新当前挂牌价
		ListTime:          time.Now().Unix(),
		UpdateTime:        time.Now().Unix(),
	}
	// 将 NFT 写入数据库
	if err = s.db.WithContext(s.ctx).Table(multi.ItemTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&newItem).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed on create item",
			zap.Error(err))
	}

	// 7. 写入扩展元数据 (createItemExternal)
	// 尝试从链上获取 TokenURI，并解析 Metadata (image, attributes 等)
	// 如果是第一次见到这个 NFT，这步操作会填充其元数据
	s.createItemExternal(event.Nft.CollectionAddr.String(), event.Nft.TokenId.String())

	blockTime, err := s.chainClient.BlockTimeByNumber(s.ctx, big.NewInt(int64(log.BlockNumber)))
	if err != nil {
		xzap.WithContext(s.ctx).Error("failed to get block time", zap.Error(err))
		return
	}
	// 8. 记录活动日志 (Activity)
	// Activity 表用于前端展示“活动历史”
	// 根据订单类型确定活动类型
	var activityType int
	if side == Bid {
		if saleKind == FixForCollection {
			activityType = multi.CollectionBid // 集合出价
		} else {
			activityType = multi.ItemBid // 单品出价
		}
	} else {
		activityType = multi.Listing // 上架活动 (Listing)
	}
	newActivity := multi.Activity{ // 将订单信息存入活动表
		ActivityType:      activityType,
		Maker:             maker.String(),
		Taker:             ZeroAddress,
		MarketplaceID:     multi.MarketOrderBook,
		CollectionAddress: event.Nft.CollectionAddr.String(),
		TokenId:           event.Nft.TokenId.String(),
		CurrencyAddress:   s.cfg.ContractCfg.EthAddress,
		Price:             decimal.NewFromBigInt(event.Price, 0),
		BlockNumber:       int64(log.BlockNumber),
		TxHash:            log.TxHash.String(),
		EventTime:         int64(blockTime),
	}
	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&newActivity).Error; err != nil {
		xzap.WithContext(s.ctx).Warn("failed on create activity",
			zap.Error(err))
	}

	// 9. 将订单添加到 OrderManager 队列
	// 用于后续的状态管理，如过期检查
	if err := s.orderManager.AddToOrderManagerQueue(&multi.Order{ // 将订单信息存入订单管理队列
		ExpireTime:        newOrder.ExpireTime,
		OrderID:           newOrder.OrderID,
		CollectionAddress: newOrder.CollectionAddress,
		TokenId:           newOrder.TokenId,
		Price:             newOrder.Price,
		Maker:             newOrder.Maker,
	}); err != nil {
		xzap.WithContext(s.ctx).Error("failed on add order to manager queue",
			zap.Error(err),
			zap.String("order_id", newOrder.OrderID))
	}

	// 10. 维护 Collection 和 Item 统计信息
	// 只有卖单（Listing）才需要维护 collection 和 item 信息（如 floor price 更新）
	if side == List { // 卖单
		s.maintainCollectionAndItem(event.Nft.CollectionAddr.String(), event.Nft.TokenId.String(), decimal.NewFromBigInt(event.Price, 0))
	}
}

// handleMatchEvent 处理成交事件 (LogMatch)
// 当买卖双方订单匹配成功时触发
func (s *Service) handleMatchEvent(log ethereumTypes.Log) {
	// 1. 检查区块链分叉
	if err := s.checkAndHandleFork(log.BlockNumber, log.TxHash.String()); err != nil {
		xzap.WithContext(s.ctx).Error("failed to handle fork for match event",
			zap.Error(err),
			zap.Uint64("block_number", log.BlockNumber),
			zap.String("tx_hash", log.TxHash.String()))
		return
	}

	// 定义用于解析 LogMatch 事件数据的结构体
	// 注意：LogMatch 事件包含嵌套的 MakeOrder 和 TakeOrder 结构体
	var event struct {
		MakeOrder Order    // 挂单详情 (被动方)
		TakeOrder Order    // 吃单详情 (主动方)
		FillPrice *big.Int // 成交价格
	}

	// 2. 解析事件日志
	err := s.parsedAbi.UnpackIntoInterface(&event, "LogMatch", log.Data)
	if err != nil {
		xzap.WithContext(s.ctx).Error("Error unpacking LogMatch event:", zap.Error(err))
		return
	}

	// 3. 提取订单 ID
	// Topic 1: makeOrderKey (挂单ID)
	// Topic 2: takeOrderKey (吃单ID)
	makeOrderId := HexPrefix + hex.EncodeToString(log.Topics[1].Bytes()) // 通过topic获取订单ID
	takeOrderId := HexPrefix + hex.EncodeToString(log.Topics[2].Bytes())
	var owner string
	var collection string
	var tokenId string
	var from string
	var to string
	var sellOrderId string
	var buyOrder multi.Order

	// 4. 确定买卖双方角色
	// MakeOrder 是挂单（被动成交，早已存在于数据库中），TakeOrder 是吃单（主动成交，刚刚触发交易）
	// 根据 MakeOrder 的 Side (Bid/List) 判断谁是买家谁是卖家，以及交易的发起方向
	if event.MakeOrder.Side == Bid { // Case A: 挂单是买单 (Bid)，吃单是卖单 (Listing) -> 卖家主动成交 (Accept Offer)
		// 场景：Alice 挂了一个 Offer (MakeOrder), Bob 接受了这个 Offer (TakeOrder)
		owner = strings.ToLower(event.MakeOrder.Maker.String()) // 新 owner 是买家 (MakeOrder.Maker)
		collection = event.TakeOrder.Nft.CollectionAddr.String()
		tokenId = event.TakeOrder.Nft.TokenId.String()
		from = event.TakeOrder.Maker.String() // 卖家 (TakeOrder.Maker)
		to = event.MakeOrder.Maker.String()   // 买家 (MakeOrder.Maker)
		sellOrderId = takeOrderId             // 卖单是 TakeOrder (虽然是 Taker，但在 Offer 匹配场景下，Taker 提供 NFT，即卖单)

		// 4.1 更新卖方订单状态 (Filled)
		// 吃单 (TakeOrder) 通常是立即完全成交的，因为它是在交易函数中即时构建的
		if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
			Where("order_id = ?", takeOrderId).
			Updates(map[string]interface{}{
				"order_status":       multi.OrderStatusFilled,
				"quantity_remaining": 0,
				"taker":              to,
			}).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed on update order status",
				zap.String("order_id", takeOrderId))
			return
		}

		// 4.2 更新买方订单状态 (Partial Fill or Filled)
		// 挂单 (MakeOrder) 可能是部分成交 (例如：求购 10 个，只成交了 1 个)
		// 查询买方订单信息，不存在则无需更新，说明该订单可能不是从平台前端发起的（或者是数据同步延迟）
		if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
			Where("order_id = ?", makeOrderId).
			First(&buyOrder).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed on get buy order",
				zap.Error(err))
			return
		}
		// 更新买方订单的剩余数量
		if buyOrder.QuantityRemaining > 1 {
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("order_id = ?", makeOrderId).
				Update("quantity_remaining", buyOrder.QuantityRemaining-1).Error; err != nil {
				xzap.WithContext(s.ctx).Error("failed on update order quantity_remaining",
					zap.String("order_id", makeOrderId))
				return
			}
		} else {
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("order_id = ?", makeOrderId).
				Updates(map[string]interface{}{
					"order_status":       multi.OrderStatusFilled,
					"quantity_remaining": 0,
				}).Error; err != nil {
				xzap.WithContext(s.ctx).Error("failed on update order status",
					zap.String("order_id", makeOrderId))
				return
			}
		}
	} else { // Case B: 挂单是卖单 (Listing)，吃单是买单 (Bid) -> 买家主动成交 (Buy Now)
		// 场景：Alice 挂了一个 Listing (MakeOrder), Bob 直接购买 (TakeOrder)
		owner = strings.ToLower(event.TakeOrder.Maker.String()) // 新 owner 是买家 (TakeOrder.Maker)
		collection = event.MakeOrder.Nft.CollectionAddr.String()
		tokenId = event.MakeOrder.Nft.TokenId.String()
		from = event.MakeOrder.Maker.String() // 卖家 (MakeOrder.Maker)
		to = event.TakeOrder.Maker.String()   // 买家 (TakeOrder.Maker)
		sellOrderId = makeOrderId             // 卖单是 MakeOrder (Listing)

		if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
			Where("order_id = ?", makeOrderId).
			Updates(map[string]interface{}{
				"order_status":       multi.OrderStatusFilled,
				"quantity_remaining": 0,
				"taker":              to,
			}).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed on update order status",
				zap.String("order_id", makeOrderId))
			return
		}

		if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
			Where("order_id = ?", takeOrderId).
			First(&buyOrder).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed on get buy order",
				zap.Error(err))
			return
		}
		if buyOrder.QuantityRemaining > 1 {
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("order_id = ?", takeOrderId).
				Update("quantity_remaining", buyOrder.QuantityRemaining-1).Error; err != nil {
				xzap.WithContext(s.ctx).Error("failed on update order quantity_remaining",
					zap.String("order_id", takeOrderId))
				return
			}
		} else {
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("order_id = ?", takeOrderId).
				Updates(map[string]interface{}{
					"order_status":       multi.OrderStatusFilled,
					"quantity_remaining": 0,
				}).Error; err != nil {
				xzap.WithContext(s.ctx).Error("failed on update order status",
					zap.String("order_id", takeOrderId))
				return
			}
		}
	}

	blockTime, err := s.chainClient.BlockTimeByNumber(s.ctx, big.NewInt(int64(log.BlockNumber)))
	if err != nil {
		xzap.WithContext(s.ctx).Error("failed to get block time", zap.Error(err))
		return
	}
	// 5. 记录交易活动 (Activity: Sale)
	newActivity := multi.Activity{
		ActivityType:      multi.Sale,
		Maker:             event.MakeOrder.Maker.String(),
		Taker:             event.TakeOrder.Maker.String(),
		MarketplaceID:     multi.MarketOrderBook,
		CollectionAddress: collection,
		TokenId:           tokenId,
		CurrencyAddress:   s.cfg.ContractCfg.EthAddress,
		Price:             decimal.NewFromBigInt(event.FillPrice, 0),
		BlockNumber:       int64(log.BlockNumber),
		TxHash:            log.TxHash.String(),
		EventTime:         int64(blockTime),
	}
	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&newActivity).Error; err != nil {
		xzap.WithContext(s.ctx).Warn("failed on create activity",
			zap.Error(err))
	}

	// 6. 更新 NFT 所有权 (Item)
	if err := s.db.WithContext(s.ctx).Table(multi.ItemTableName(s.chain)).
		Where("collection_address = ? and token_id = ?", strings.ToLower(collection), tokenId).
		Update("owner", owner).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed to update item owner",
			zap.Error(err))
		return
	}

	// 7. 发送价格更新事件 (用于计算新的 Floor Price 等)
	// 通知 OrderManager 有新的交易发生，可能影响集合的地板价、交易量等统计数据
	if err := ordermanager.AddUpdatePriceEvent(s.kv, &ordermanager.TradeEvent{ // 将交易信息存入价格更新队列
		OrderId:        sellOrderId,
		CollectionAddr: collection,
		EventType:      ordermanager.Buy,
		TokenID:        tokenId,
		From:           from,
		To:             to,
	}, s.chain); err != nil {
		xzap.WithContext(s.ctx).Error("failed on add update price event",
			zap.Error(err),
			zap.String("type", "sale"),
			zap.String("order_id", sellOrderId))
	}
}

// handleCancelEvent 处理取消订单事件 (LogCancel)
// 当用户主动取消订单时触发
func (s *Service) handleCancelEvent(log ethereumTypes.Log) {
	// 1. 检查区块链分叉
	if err := s.checkAndHandleFork(log.BlockNumber, log.TxHash.String()); err != nil {
		xzap.WithContext(s.ctx).Error("failed to handle fork for cancel event",
			zap.Error(err),
			zap.Uint64("block_number", log.BlockNumber),
			zap.String("tx_hash", log.TxHash.String()))
		return
	}

	// 2. 提取订单 ID
	// Topic 1: orderKey (32 bytes)
	// 将 bytes32转换为 hex string 作为数据库主键
	orderId := HexPrefix + hex.EncodeToString(log.Topics[1].Bytes())
	//maker := common.BytesToAddress(log.Topics[2].Bytes())

	// 3. 更新订单状态为已取消 (Cancelled)
	if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
		Where("order_id = ?", orderId).
		Update("order_status", multi.OrderStatusCancelled).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed on update order status",
			zap.String("order_id", orderId))
		return
	}

	// 4. 查询被取消的订单信息
	// 需要获取订单详情来记录 Activity
	var cancelOrder multi.Order
	if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
		Where("order_id = ?", orderId).
		First(&cancelOrder).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed on get cancel order",
			zap.Error(err))
		return
	}

	blockTime, err := s.chainClient.BlockTimeByNumber(s.ctx, big.NewInt(int64(log.BlockNumber)))
	if err != nil {
		xzap.WithContext(s.ctx).Error("failed to get block time", zap.Error(err))
		return
	}
	// 5. 确定活动类型 (Cancel Listing/Bid)
	// 根据原订单类型，决定是 "取消挂单" 还是 "取消出价"
	var activityType int
	if cancelOrder.OrderType == multi.ListingOrder {
		activityType = multi.CancelListing // 取消卖单
	} else if cancelOrder.OrderType == multi.CollectionBidOrder {
		activityType = multi.CancelCollectionBid // 取消集合出价
	} else {
		activityType = multi.CancelItemBid // 取消单品出价
	}
	// 记录取消活动
	// 即使订单已取消，也需要记录这条 Activity，供前端展示历史记录
	newActivity := multi.Activity{
		ActivityType:      activityType,
		Maker:             cancelOrder.Maker,
		Taker:             ZeroAddress,
		MarketplaceID:     multi.MarketOrderBook,
		CollectionAddress: cancelOrder.CollectionAddress,
		TokenId:           cancelOrder.TokenId,
		CurrencyAddress:   s.cfg.ContractCfg.EthAddress,
		Price:             cancelOrder.Price,
		BlockNumber:       int64(log.BlockNumber),
		TxHash:            log.TxHash.String(),
		EventTime:         int64(blockTime),
	}
	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&newActivity).Error; err != nil {
		xzap.WithContext(s.ctx).Warn("failed on create activity",
			zap.Error(err))
	}

	// 6. 发送价格更新事件 (用于更新 Floor Price)
	// 取消卖单可能会影响地板价（例如取消了当前最低价的订单），需要重新计算
	if err := ordermanager.AddUpdatePriceEvent(s.kv, &ordermanager.TradeEvent{
		OrderId:        cancelOrder.OrderID,
		CollectionAddr: cancelOrder.CollectionAddress,
		TokenID:        cancelOrder.TokenId,
		EventType:      ordermanager.Cancel,
	}, s.chain); err != nil {
		xzap.WithContext(s.ctx).Error("failed on add update price event",
			zap.Error(err),
			zap.String("type", "cancel"),
			zap.String("order_id", cancelOrder.OrderID))
	}
}

// handleApprovalEvent 处理 ERC721 授权事件 (Approval)
// 当 NFT 被授权/取消授权给某个地址时触发
func (s *Service) handleApprovalEvent(log ethereumTypes.Log) {
	// 1. 检查分叉
	if err := s.checkAndHandleFork(log.BlockNumber, log.TxHash.String()); err != nil {
		xzap.WithContext(s.ctx).Error("failed to handle fork for approval event",
			zap.Error(err),
			zap.Uint64("block_number", log.BlockNumber),
			zap.String("tx_hash", log.TxHash.String()))
		return
	}

	// 2. 解析事件参数
	// Topics: [Signature, Owner, Approved, TokenId]
	owner := common.BytesToAddress(log.Topics[1].Bytes())
	approved := common.BytesToAddress(log.Topics[2].Bytes())
	tokenId := new(big.Int).SetBytes(log.Topics[3].Bytes())
	collectionAddress := log.Address.String()

	// 记录授权信息
	xzap.WithContext(s.ctx).Info("ERC721 Approval event detected",
		zap.String("collection", collectionAddress),
		zap.String("token_id", tokenId.String()),
		zap.String("owner", owner.String()),
		zap.String("approved", approved.String()),
		zap.Bool("is_vault_approved", strings.EqualFold(approved.String(), s.vaultAddress)))

	// 可以在这里添加数据库存储逻辑，记录授权状态
	blockTime, err := s.chainClient.BlockTimeByNumber(s.ctx, big.NewInt(int64(log.BlockNumber)))
	if err != nil {
		xzap.WithContext(s.ctx).Error("failed to get block time", zap.Error(err))
		return
	}

	// 存储授权记录到数据库（可选）
	approvalRecord := multi.Activity{
		ActivityType:      multi.Listing, // 暂时使用现有的类型，后续可以添加新的常量
		Maker:             owner.String(),
		Taker:             approved.String(),
		MarketplaceID:     multi.MarketOrderBook,
		CollectionAddress: collectionAddress,
		TokenId:           tokenId.String(),
		CurrencyAddress:   s.cfg.ContractCfg.EthAddress,
		Price:             decimal.Zero,
		BlockNumber:       int64(log.BlockNumber),
		TxHash:            log.TxHash.String(),
		EventTime:         int64(blockTime),
	}

	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(&approvalRecord).Error; err != nil {
		xzap.WithContext(s.ctx).Warn("failed on create approval activity",
			zap.Error(err))
	}
}

// CheckNFTApprovalStatus 检查指定NFT是否已授权给vault合约
func (s *Service) CheckNFTApprovalStatus(collectionAddress string, tokenId string) (bool, error) {
	// 调用 getApproved 方法
	tokenIdBig := new(big.Int)
	tokenIdBig.SetString(tokenId, 10)

	// 获取当前授权的地址
	approvedAddress, err := s.getApprovedAddress(collectionAddress, tokenIdBig)
	if err != nil {
		return false, errors.Wrap(err, "failed to get approved address")
	}

	// 检查是否授权给vault合约
	isApproved := strings.EqualFold(approvedAddress.String(), s.vaultAddress)

	xzap.WithContext(s.ctx).Info("NFT approval status checked",
		zap.String("collection", collectionAddress),
		zap.String("token_id", tokenId),
		zap.String("approved_to", approvedAddress.String()),
		zap.String("vault_address", s.vaultAddress),
		zap.Bool("is_vault_approved", isApproved))

	return isApproved, nil
}

// getApprovedAddress 获取NFT当前授权的地址
func (s *Service) getApprovedAddress(collectionAddress string, tokenId *big.Int) (common.Address, error) {
	// ERC721 getApproved 方法的方法签名
	methodSig := "0x081812fc" // getApproved(uint256)的方法签名

	// 编码参数
	paddedTokenId := common.LeftPadBytes(tokenId.Bytes(), 32)
	data := append(common.Hex2Bytes(methodSig[2:]), paddedTokenId...)

	// 调用合约
	contractAddr := common.HexToAddress(collectionAddress)
	callMsg := ethereum.CallMsg{
		To:   &contractAddr,
		Data: data,
	}

	result, err := s.chainClient.CallContract(s.ctx, callMsg, nil)
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to call getApproved")
	}

	if len(result) < 32 {
		return common.Address{}, errors.New("invalid response length")
	}

	// 解析结果
	approvedAddress := common.BytesToAddress(result[12:32]) // 取最后20字节作为地址
	return approvedAddress, nil
}

// CheckMultipleNFTApprovals 批量检查多个NFT的授权状态
func (s *Service) CheckMultipleNFTApprovals(nfts []struct {
	CollectionAddress string
	TokenId           string
}) (map[string]bool, error) {
	results := make(map[string]bool)

	for _, nft := range nfts {
		key := fmt.Sprintf("%s:%s", nft.CollectionAddress, nft.TokenId)
		approved, err := s.CheckNFTApprovalStatus(nft.CollectionAddress, nft.TokenId)
		if err != nil {
			xzap.WithContext(s.ctx).Error("failed to check NFT approval",
				zap.String("collection", nft.CollectionAddress),
				zap.String("token_id", nft.TokenId),
				zap.Error(err))
			results[key] = false
			continue
		}
		results[key] = approved
	}

	return results, nil
}

// CanMarketBuyNFT 判断市场合约是否能购买指定的NFT
func (s *Service) CanMarketBuyNFT(collectionAddress string, tokenId string) (bool, string, error) {
	// 1. 检查NFT是否授权给vault
	isApproved, err := s.CheckNFTApprovalStatus(collectionAddress, tokenId)
	if err != nil {
		return false, "检查授权状态失败", err
	}

	if !isApproved {
		return false, "NFT未授权给vault合约", nil
	}

	// 2. 检查是否有有效的挂单
	var activeOrder multi.Order
	if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
		Where("collection_address = ? AND token_id = ? AND order_type = ? AND order_status = ? AND expire_time > ?",
			strings.ToLower(collectionAddress), tokenId, multi.ListingOrder, multi.OrderStatusActive, time.Now().Unix()).
		First(&activeOrder).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "没有有效的挂单", nil
		}
		return false, "查询挂单失败", err
	}

	// 3. 验证挂单的maker确实是NFT的owner（可选的额外安全检查）
	// 这里可以添加更多的验证逻辑

	return true, "可以购买", nil
}

// checkAndHandleFork 检查并处理区块链分叉 (Reorg)
// 如果检测到当前事件所在的交易哈希已经存在于数据库中，但区块高度不一致，主要说明发生了分叉
// Reorg 发生时，旧链上的交易虽然已经执行，但在新链上可能未执行或执行顺序不同
// 这里采取的策略是：一旦发现分叉，回滚该交易在数据库中的所有状态变更
func (s *Service) checkAndHandleFork(blockNumber uint64, txHash string) error {
	// 1. 检查交易是否存在但区块高度不同
	// tx_hash 相同但 block_number 不同，是 Reorg 的典型特征
	var count int64
	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).
		Where("tx_hash = ? AND block_number != ?", txHash, blockNumber).
		Count(&count).Error; err != nil {
		return errors.Wrap(err, "failed to check transaction existence")
	}

	// 2. 如果检测到分叉
	if count > 0 {
		xzap.WithContext(s.ctx).Warn("fork detected, rolling back transaction",
			zap.String("tx_hash", txHash),
			zap.Uint64("new_block_number", blockNumber))

		// 2.1 回滚受影响的订单状态
		// 调用 rollbackOrderStatus 函数，根据 txHash 查找所有相关的活动记录，
		// 并将这些活动所导致的订单状态和NFT所有权变更进行回滚，恢复到分叉前的状态。
		if err := s.rollbackOrderStatus(txHash); err != nil {
			return errors.Wrap(err, "failed to rollback order status")
		}

		// 2.2 删除原有的（分叉前的）活动记录
		// 删除 Activity 记录，相当于撤销了这笔交易的历史痕迹。
		// 这是为了确保数据库中只保留属于最终确定链的交易记录。
		if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).
			Where("tx_hash = ?", txHash).
			Delete(&multi.Activity{}).Error; err != nil {
			return errors.Wrap(err, "failed to delete old activity")
		}

		xzap.WithContext(s.ctx).Info("handled fork situation",
			zap.String("tx_hash", txHash),
			zap.Uint64("new_block_number", blockNumber))
	}

	return nil
}

// rollbackOrderStatus 回滚订单状态
// 当发生分叉时，将相关订单恢复到分叉前的状态，依赖 Activity 表记录的历史操作
// 此函数通过查询与给定交易哈希相关的所有活动记录，并根据活动类型执行相应的数据库回滚操作。
func (s *Service) rollbackOrderStatus(txHash string) error {
	// 1. 查找该交易产生的所有活动
	// Activity 表记录了交易类型 (Sale/Cancel) 和相关参数，是回滚的依据。
	// 这些记录代表了在分叉链上发生的、现在需要撤销的操作。
	var activities []multi.Activity
	if err := s.db.WithContext(s.ctx).Table(multi.ActivityTableName(s.chain)).
		Where("tx_hash = ?", txHash).
		Find(&activities).Error; err != nil {
		return errors.Wrap(err, "failed to find activities")
	}

	// 2. 遍历活动并恢复状态
	for _, activity := range activities {
		switch activity.ActivityType {
		case multi.Sale:
			// 2.1 对于成交 (Sale) 活动
			// 之前的操作：订单状态从 Active 变为 Filled (已成交)。
			// 回滚操作：将订单状态恢复为 Active (活跃)，表示该订单并未成交。
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("maker = ? AND collection_address = ? AND token_id = ? AND price = ?",
					activity.Maker, activity.CollectionAddress, activity.TokenId, activity.Price).
				Updates(map[string]interface{}{
					"order_status":       multi.OrderStatusActive,
					"quantity_remaining": 1,           // 假设 ERC721 数量为 1，恢复
					"taker":              ZeroAddress, // 清除 taker 信息
				}).Error; err != nil {
				return errors.Wrap(err, "failed to restore order status for sale")
			}

			// 恢复NFT所有权给 Maker (卖方)
			// 之前的操作：NFT所有权从 Maker 转移到 Taker (买方)。
			// 回滚操作：将NFT的所有者恢复为原始的 Maker，撤销所有权转移。
			if err := s.db.WithContext(s.ctx).Table(multi.ItemTableName(s.chain)).
				Where("collection_address = ? AND token_id = ?",
					activity.CollectionAddress, activity.TokenId).
				Update("owner", activity.Maker).Error; err != nil {
				return errors.Wrap(err, "failed to restore item owner")
			}

		case multi.CancelListing, multi.CancelCollectionBid, multi.CancelItemBid:
			// 2.2 对于取消 (Cancel) 活动
			// 之前的操作：订单状态从 Active 变为 Cancelled (已取消)。
			// 回滚操作：将订单状态恢复为 Active (活跃)，表示该订单并未被取消。
			if err := s.db.WithContext(s.ctx).Table(multi.OrderTableName(s.chain)).
				Where("maker = ? AND collection_address = ? AND token_id = ? AND price = ?",
					activity.Maker, activity.CollectionAddress, activity.TokenId, activity.Price).
				Update("order_status", multi.OrderStatusActive).Error; err != nil {
				return errors.Wrap(err, "failed to restore order status for cancel")
			}
		}
	}
	return nil
}

// UpKeepingCollectionFloorChangeLoop 地板价维护循环
// 定期更新集合地板价，并清理过期的历史记录
func (s *Service) UpKeepingCollectionFloorChangeLoop() {
	// 定时器设置
	timer := time.NewTicker(comm.DaySeconds * time.Second) // 每天执行一次清理
	defer timer.Stop()
	updateFloorPriceTimer := time.NewTicker(comm.MaxCollectionFloorTimeDifference * time.Second) // 定期更新地板价
	defer updateFloorPriceTimer.Stop()

	var indexedStatus base.IndexedStatus
	if err := s.db.WithContext(s.ctx).Table(base.IndexedStatusTableName()).
		Select("last_indexed_time").
		Where("chain_id = ? and index_type = ?", s.chainId, comm.CollectionFloorChangeIndexType).
		First(&indexedStatus).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed on get collection floor change index status",
			zap.Error(err))
		return
	}

	for {
		select {
		case <-s.ctx.Done():
			xzap.WithContext(s.ctx).Info("UpKeepingCollectionFloorChangeLoop stopped due to context cancellation")
			return
		case <-timer.C:
			// 清理过期的地板价记录
			if err := s.deleteExpireCollectionFloorChangeFromDatabase(); err != nil {
				xzap.WithContext(s.ctx).Error("failed on delete expire collection floor change",
					zap.Error(err))
			}
		case <-updateFloorPriceTimer.C:
			// 查询并更新地板价
			if s.cfg.ProjectCfg.Name == gdb.OrderBookDexProject {
				floorPrices, err := s.QueryCollectionsFloorPrice()
				if err != nil {
					xzap.WithContext(s.ctx).Error("failed on query collections floor change",
						zap.Error(err))
					continue
				}

				if err := s.persistCollectionsFloorChange(floorPrices); err != nil {
					xzap.WithContext(s.ctx).Error("failed on persist collections floor price",
						zap.Error(err))
					continue
				}
			}
		default:
		}
	}
}

func (s *Service) deleteExpireCollectionFloorChangeFromDatabase() error {
	stmt := fmt.Sprintf(`DELETE FROM %s where event_time < UNIX_TIMESTAMP() - %d`, gdb.GetMultiProjectCollectionFloorPriceTableName(s.cfg.ProjectCfg.Name, s.chain), comm.CollectionFloorTimeRange)

	if err := s.db.Exec(stmt).Error; err != nil {
		return errors.Wrap(err, "failed on delete expire collection floor price")
	}

	return nil
}

func (s *Service) QueryCollectionsFloorPrice() ([]multi.CollectionFloorPrice, error) {
	timestamp := time.Now().Unix()
	timestampMilli := time.Now().UnixMilli()
	var collectionFloorPrice []multi.CollectionFloorPrice
	sql := fmt.Sprintf(`SELECT co.collection_address as collection_address,min(co.price) as price
FROM %s as ci
         left join %s co on co.collection_address = ci.collection_address and co.token_id = ci.token_id
WHERE (co.order_type = ? and
       co.order_status = ? and expire_time > ? and co.maker = ci.owner) group by co.collection_address`, gdb.GetMultiProjectItemTableName(s.cfg.ProjectCfg.Name, s.chain), gdb.GetMultiProjectOrderTableName(s.cfg.ProjectCfg.Name, s.chain))
	if err := s.db.WithContext(s.ctx).Raw(
		sql,
		multi.ListingType,
		multi.OrderStatusActive,
		time.Now().Unix(),
	).Scan(&collectionFloorPrice).Error; err != nil {
		return nil, errors.Wrap(err, "failed on get collection floor price")
	}

	for i := 0; i < len(collectionFloorPrice); i++ {
		collectionFloorPrice[i].EventTime = timestamp
		collectionFloorPrice[i].CreateTime = timestampMilli
		collectionFloorPrice[i].UpdateTime = timestampMilli
	}

	return collectionFloorPrice, nil
}

// persistCollectionsFloorChange 持久化集合地板价变更
// 批量插入 floor_price_change 表
func (s *Service) persistCollectionsFloorChange(FloorPrices []multi.CollectionFloorPrice) error {
	// 分批处理，避免 SQL 语句过长
	for i := 0; i < len(FloorPrices); i += comm.DBBatchSizeLimit {
		end := i + comm.DBBatchSizeLimit
		if i+comm.DBBatchSizeLimit >= len(FloorPrices) {
			end = len(FloorPrices)
		}

		valueStrings := make([]string, 0)
		valueArgs := make([]interface{}, 0)

		for _, t := range FloorPrices[i:end] {
			valueStrings = append(valueStrings, "(?,?,?,?,?)")
			valueArgs = append(valueArgs, t.CollectionAddress, t.Price, t.EventTime, t.CreateTime, t.UpdateTime)
		}

		stmt := fmt.Sprintf(`INSERT INTO %s (collection_address,price,event_time,create_time,update_time)  VALUES %s
		ON DUPLICATE KEY UPDATE update_time=VALUES(update_time)`, gdb.GetMultiProjectCollectionFloorPriceTableName(s.cfg.ProjectCfg.Name, s.chain), strings.Join(valueStrings, ","))

		if err := s.db.Exec(stmt, valueArgs...).Error; err != nil {
			return errors.Wrap(err, "failed on persist collection floor price info")
		}
	}
	return nil
}

// maintainCollectionAndItem 维护 Collection 和 Item 信息
// 当有新 Listing 创建时调用，用于初始化集合信息和更新地板价。
// 此函数确保数据库中存在对应的 Collection 和 Item 记录，并更新其上架信息和集合的地板价。
func (s *Service) maintainCollectionAndItem(collectionAddress, tokenId string, price decimal.Decimal) {
	// 1. 检查并创建 collection 记录（如果不存在）
	// 确保每个 NFT 所属的集合在数据库中都有对应的记录。
	// 如果集合不存在，则会创建一个带有默认信息的新记录。
	s.ensureCollectionExists(collectionAddress)

	// 2. 更新 item 的上架信息
	// 更新特定 NFT 的上架价格和上架时间。
	// 如果该 NFT 之前不存在，则会创建一条新的 Item 记录。
	s.updateItemListingInfo(collectionAddress, tokenId, price)

	// 3. 更新 collection 的 floor_price
	// 根据当前集合中所有活跃的 Listing，重新计算并更新该集合的最低地板价。
	// 这一步确保集合的地板价始终反映最新的市场情况。
	s.updateCollectionFloorPrice(collectionAddress)
}

// ensureCollectionExists 确保 collection 记录存在
func (s *Service) ensureCollectionExists(collectionAddress string) {
	collectionTableName := gdb.GetMultiProjectCollectionTableName(s.cfg.ProjectCfg.Name, s.chain)

	// 检查 collection 是否存在
	var count int64
	if err := s.db.WithContext(s.ctx).Table(collectionTableName).
		Where("address = ?", collectionAddress).
		Count(&count).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed to check collection existence",
			zap.Error(err),
			zap.String("collection_address", collectionAddress))
		return
	}

	// 如果不存在，创建基础 collection 记录
	if count == 0 {
		now := time.Now().Unix()
		collection := map[string]interface{}{
			"address":        collectionAddress,
			"chain_id":       s.chainId,
			"symbol":         "Unknown", // 默认值，后续可通过其他服务更新
			"name":           "Unknown Collection",
			"creator":        "0x0000000000000000000000000000000000000000",
			"token_standard": 721, // 默认 ERC721
			"auth":           0,
			"owner_amount":   0,
			"item_amount":    0,
			"floor_price":    nil,
			"sale_price":     nil,
			"volume_total":   decimal.Zero,
			"create_time":    now,
			"update_time":    now,
		}

		if err := s.db.WithContext(s.ctx).Table(collectionTableName).
			Create(&collection).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed to create collection record",
				zap.Error(err),
				zap.String("collection_address", collectionAddress))
		} else {
			xzap.WithContext(s.ctx).Info("created new collection record",
				zap.String("collection_address", collectionAddress))
		}
	}
}

// updateItemListingInfo 更新 item 的上架信息
func (s *Service) updateItemListingInfo(collectionAddress, tokenId string, price decimal.Decimal) {
	itemTableName := gdb.GetMultiProjectItemTableName(s.cfg.ProjectCfg.Name, s.chain)
	now := time.Now().Unix()

	// 更新或插入 item 记录
	updates := map[string]interface{}{
		"list_price":  price,
		"list_time":   now,
		"update_time": now,
	}

	// 先尝试更新
	result := s.db.WithContext(s.ctx).Table(itemTableName).
		Where("collection_address = ? AND token_id = ?", collectionAddress, tokenId).
		Updates(updates)

	if result.Error != nil {
		xzap.WithContext(s.ctx).Error("failed to update item listing info",
			zap.Error(result.Error),
			zap.String("collection_address", collectionAddress),
			zap.String("token_id", tokenId))
		return
	}

	// 如果没有更新任何记录，说明 item 不存在，创建基础记录
	if result.RowsAffected == 0 {
		item := map[string]interface{}{
			"chain_id":           s.chainId,
			"token_id":           tokenId,
			"collection_address": collectionAddress,
			"name":               fmt.Sprintf("Token #%s", tokenId),
			"creator":            "0x0000000000000000000000000000000000000000",
			"owner":              nil, // 需要通过其他方式获取
			"supply":             1,
			"list_price":         price,
			"list_time":          now,
			"sale_price":         nil,
			"views":              0,
			"is_opensea_banned":  false,
			"create_time":        now,
			"update_time":        now,
		}

		if err := s.db.WithContext(s.ctx).Table(itemTableName).
			Create(&item).Error; err != nil {
			xzap.WithContext(s.ctx).Error("failed to create item record",
				zap.Error(err),
				zap.String("collection_address", collectionAddress),
				zap.String("token_id", tokenId))
		} else {
			xzap.WithContext(s.ctx).Info("created new item record",
				zap.String("collection_address", collectionAddress),
				zap.String("token_id", tokenId))
		}
	}
}

// updateCollectionFloorPrice 更新 collection 的 floor_price
func (s *Service) updateCollectionFloorPrice(collectionAddress string) {
	itemTableName := gdb.GetMultiProjectItemTableName(s.cfg.ProjectCfg.Name, s.chain)
	collectionTableName := gdb.GetMultiProjectCollectionTableName(s.cfg.ProjectCfg.Name, s.chain)

	// 查询该 collection 中最低的上架价格
	var minPrice decimal.Decimal
	if err := s.db.WithContext(s.ctx).Table(itemTableName).
		Select("MIN(list_price)").
		Where("collection_address = ? AND list_price IS NOT NULL AND list_price > 0", collectionAddress).
		Scan(&minPrice).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed to get collection min price",
			zap.Error(err),
			zap.String("collection_address", collectionAddress))
		return
	}

	// 更新 collection 的 floor_price
	if err := s.db.WithContext(s.ctx).Table(collectionTableName).
		Where("address = ?", collectionAddress).
		Update("floor_price", minPrice).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed to update collection floor price",
			zap.Error(err),
			zap.String("collection_address", collectionAddress),
			zap.String("floor_price", minPrice.String()))
	} else {
		xzap.WithContext(s.ctx).Debug("updated collection floor price",
			zap.String("collection_address", collectionAddress),
			zap.String("floor_price", minPrice.String()))
	}
}

// getTokenURI 获取NFT的tokenURI（元数据URI）
func (s *Service) getTokenURI(collectionAddress string, tokenId *big.Int) (string, error) {
	// ERC721 tokenURI 方法的方法签名: tokenURI(uint256)
	methodSig := "0xc87b56dd" // tokenURI(uint256)的方法签名

	// 编码参数
	paddedTokenId := common.LeftPadBytes(tokenId.Bytes(), 32)
	data := append(common.Hex2Bytes(methodSig[2:]), paddedTokenId...)

	// 调用合约
	contractAddr := common.HexToAddress(collectionAddress)
	callMsg := ethereum.CallMsg{
		To:   &contractAddr,
		Data: data,
	}

	result, err := s.chainClient.CallContract(s.ctx, callMsg, nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to call tokenURI")
	}

	if len(result) < 32 {
		return "", errors.New("invalid response length")
	}

	// 解析结果：tokenURI返回的是string类型（动态类型）
	// 前32字节是offset（指向实际数据的位置）
	offset := new(big.Int).SetBytes(result[0:32]).Uint64()

	// 如果offset为0或超出结果长度，说明格式不对
	if offset == 0 || len(result) < int(offset+32) {
		return "", errors.New("invalid offset in tokenURI response")
	}

	// offset位置的前32字节是字符串长度
	length := new(big.Int).SetBytes(result[offset : offset+32]).Uint64()

	// 检查长度是否合理（不超过剩余数据）
	if length == 0 || len(result) < int(offset+32+length) {
		return "", errors.New("invalid string length in tokenURI response")
	}

	// 提取字符串数据（从offset+32开始，长度为length）
	strBytes := result[offset+32 : offset+32+length]
	tokenURI := string(strBytes)

	// 移除可能的空字符和空白字符
	tokenURI = strings.TrimRight(tokenURI, "\x00")
	tokenURI = strings.TrimSpace(tokenURI)

	return tokenURI, nil
}

// getImageFromMetadata 从元数据URI获取JSON并提取image字段
func (s *Service) getImageFromMetadata(metaDataURI string) (string, error) {
	// 创建HTTP请求，设置超时
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", metaDataURI, nil)
	if err != nil {
		return "", errors.Wrap(err, "failed to create HTTP request")
	}

	// 设置请求头
	req.Header.Set("User-Agent", "EasySwapSync/1.0")
	req.Header.Set("Accept", "application/json")

	// 发送请求
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "failed to fetch metadata")
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "failed to read response body")
	}

	// 解析JSON
	var metadata map[string]interface{}
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", errors.Wrap(err, "failed to parse JSON")
	}

	// 提取image字段
	image, ok := metadata["image"]
	if !ok {
		return "", errors.New("image field not found in metadata")
	}

	// 转换为字符串
	imageStr, ok := image.(string)
	if !ok {
		return "", errors.New("image field is not a string")
	}

	return imageStr, nil
}

// createItemExternal 创建扩展 Item 信息 (Metadata)
// 获取 TokenURI 并解析 Metadata (Image, Attributes 等)
// 表结构: ob_item_external_{chain}
// 唯一索引: (collection_address, token_id)
func (s *Service) createItemExternal(collectionAddress, tokenId string) {
	itemExternalTableName := fmt.Sprintf("ob_item_external_%s", s.chain)
	now := time.Now().Unix()

	// 尝试获取tokenURI（元数据URI）和image_uri
	var metaDataURI string
	var imageURI string
	tokenIdBig := new(big.Int)
	if _, ok := tokenIdBig.SetString(tokenId, 10); ok {
		// 调用合约获取 TokenURI
		uri, err := s.getTokenURI(collectionAddress, tokenIdBig)
		if err != nil {
			xzap.WithContext(s.ctx).Warn("failed to get tokenURI",
				zap.Error(err),
				zap.String("collection_address", collectionAddress),
				zap.String("token_id", tokenId))
			// 即使获取失败也继续创建记录，metaDataURI为空
		} else {
			metaDataURI = uri
			// 限制长度，避免超过varchar(512)
			if len(metaDataURI) > 512 {
				metaDataURI = metaDataURI[:512]
			}

			// 从tokenURI获取元数据并提取image字段
			if metaDataURI != "" {
				// 发送 HTTP 请求获取 Metadata JSON 并解析
				image, err := s.getImageFromMetadata(metaDataURI)
				if err != nil {
					xzap.WithContext(s.ctx).Warn("failed to get image from metadata",
						zap.Error(err),
						zap.String("meta_data_uri", metaDataURI),
						zap.String("collection_address", collectionAddress),
						zap.String("token_id", tokenId))
					// 即使获取失败也继续，imageURI为空
				} else {
					imageURI = image
					// 限制长度，避免超过varchar(512)
					if len(imageURI) > 512 {
						imageURI = imageURI[:512]
					}
				}
			}
		}
	}

	// 创建 item_external 记录
	// 根据DDL: id是自增主键，不需要设置
	// 唯一索引 (collection_address, token_id) 由 OnConflict 处理
	itemExternal := map[string]interface{}{
		"collection_address":  collectionAddress,
		"token_id":            tokenId,
		"meta_data_uri":       metaDataURI, // varchar(512)，可能为NULL
		"image_uri":           imageURI,    // varchar(512)，从元数据JSON中提取的image字段
		"is_uploaded_oss":     false,       // tinyint(1) DEFAULT '0'
		"upload_status":       0,           // tinyint NOT NULL DEFAULT '0'
		"is_video_uploaded":   false,       // tinyint(1) DEFAULT '0'
		"video_upload_status": 0,           // tinyint NOT NULL DEFAULT '0'
		"video_type":          "0",         // varchar(64) NOT NULL DEFAULT '0'
		"create_time":         now,         // bigint
		"update_time":         now,         // bigint
		// 以下字段为NULL，不设置：
		// oss_uri, video_uri, video_oss_uri
	}

	// 使用 OnConflict 处理唯一索引冲突
	if err := s.db.WithContext(s.ctx).Table(itemExternalTableName).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "collection_address"}, {Name: "token_id"}},
			DoNothing: true,
		}).Create(&itemExternal).Error; err != nil {
		xzap.WithContext(s.ctx).Error("failed to create item_external record",
			zap.Error(err),
			zap.String("collection_address", collectionAddress),
			zap.String("token_id", tokenId))
	} else {
		xzap.WithContext(s.ctx).Debug("created item_external record",
			zap.String("collection_address", collectionAddress),
			zap.String("token_id", tokenId),
			zap.String("meta_data_uri", metaDataURI),
			zap.String("image_uri", imageURI))
	}
}
