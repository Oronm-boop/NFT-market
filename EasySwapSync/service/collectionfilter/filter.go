package collectionfilter

import (
	"context"
	"strings"
	"sync"

	"github.com/ProjectsTask/EasySwapBase/stores/gdb"
	"github.com/pkg/errors"

	"gorm.io/gorm"

	"github.com/ProjectsTask/EasySwapSync/service/comm"
)

// Filter 是一个线程安全的字符串集合过滤器
// 用于快速判断某个 NFT 集合是否需要被索引或处理（例如只处理白名单中的集合）
type Filter struct {
	ctx     context.Context
	db      *gorm.DB
	chain   string
	set     map[string]bool // 存储集合地址的 Set (map实现)
	lock    *sync.RWMutex   // 读写锁，保证并发安全
	project string          // 项目名称 (如 "opensea", "looksrare", "easyswap")
}

// New 创建一个新的 Filter 实例
func New(ctx context.Context, db *gorm.DB, chain string, project string) *Filter {
	return &Filter{
		ctx:     ctx,
		db:      db,
		chain:   chain,
		set:     make(map[string]bool),
		lock:    &sync.RWMutex{},
		project: project,
	}
}

// Add 向过滤器中添加一个新元素（集合地址）
// 元素在插入前会自动转换为小写
func (f *Filter) Add(element string) {
	f.lock.Lock()         // 加写锁
	defer f.lock.Unlock() // 函数返回时解锁
	f.set[strings.ToLower(element)] = true
}

// Remove 从过滤器中删除一个元素
func (f *Filter) Remove(element string) {
	f.lock.Lock()
	defer f.lock.Unlock()
	delete(f.set, strings.ToLower(element))
}

// Contains 检查过滤器是否包含指定元素
// 元素在检查前会自动转换为小写
func (f *Filter) Contains(element string) bool {
	f.lock.RLock()         // 加读锁
	defer f.lock.RUnlock() // 函数返回时解锁
	_, exists := f.set[strings.ToLower(element)]
	return exists
}

// PreloadCollections 从数据库预加载集合地址到过滤器中
// 通常在服务启动时调用，加载所有状态为 "已导入" (CollectionFloorPriceImported) 的集合
func (f *Filter) PreloadCollections() error {
	var addresses []string
	var err error

	// 直接从数据库查询地址
	// 表名根据项目和链动态生成，例如：ob_collection_eth
	err = f.db.WithContext(f.ctx).
		Table(gdb.GetMultiProjectCollectionTableName(f.project, f.chain)).
		Select("address").
		Where("floor_price_status = ?", comm.CollectionFloorPriceImported).
		Scan(&addresses).Error

	if err != nil {
		return errors.Wrap(err, "failed on query collections from db")
	}

	// 将所有地址加入过滤器
	for _, address := range addresses {
		f.Add(address)
	}

	return nil
}
