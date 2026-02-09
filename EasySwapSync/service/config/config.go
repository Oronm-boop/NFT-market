/**
 * config.go - EasySwapSync 服务配置结构定义
 *
 * 功能：
 *   - 定义服务所需的所有配置项结构
 *   - 支持从 TOML 配置文件读取
 *   - 支持环境变量覆盖
 *
 * 配置文件示例：
 *   config/config_import.toml
 */
package config

import (
	"strings"

	"github.com/spf13/viper"

	logging "github.com/ProjectsTask/EasySwapBase/logger"
	"github.com/ProjectsTask/EasySwapBase/stores/gdb"
)

// Config 主配置结构，包含服务运行所需的所有配置
type Config struct {
	Monitor     *Monitor         `toml:"monitor" mapstructure:"monitor" json:"monitor"`                // 监控配置（pprof 等）
	Log         *logging.LogConf `toml:"log" mapstructure:"log" json:"log"`                            // 日志配置
	Kv          *KvConf          `toml:"kv" mapstructure:"kv" json:"kv"`                               // Redis 缓存配置
	DB          *gdb.Config      `toml:"db" mapstructure:"db" json:"db"`                               // 数据库配置
	AnkrCfg     AnkrCfg          `toml:"ankr_cfg" mapstructure:"ankr_cfg" json:"ankr_cfg"`             // 区块链 RPC 配置
	ChainCfg    ChainCfg         `toml:"chain_cfg" mapstructure:"chain_cfg" json:"chain_cfg"`          // 链配置
	ContractCfg ContractCfg      `toml:"contract_cfg" mapstructure:"contract_cfg" json:"contract_cfg"` // 合约地址配置
	ProjectCfg  ProjectCfg       `toml:"project_cfg" mapstructure:"project_cfg" json:"project_cfg"`    // 项目配置
}

// ChainCfg 区块链配置
type ChainCfg struct {
	Name string `toml:"name" mapstructure:"name" json:"name"` // 链名称，如 "eth", "sepolia", "optimism"
	ID   int64  `toml:"id" mapstructure:"id" json:"id"`       // 链 ID，如 1 (ETH), 11155111 (Sepolia)
}

// ContractCfg 智能合约地址配置
type ContractCfg struct {
	EthAddress   string `toml:"eth_address" mapstructure:"eth_address" json:"eth_address"`       // ETH 地址（原生代币）
	WethAddress  string `toml:"weth_address" mapstructure:"weth_address" json:"weth_address"`    // WETH 合约地址
	DexAddress   string `toml:"dex_address" mapstructure:"dex_address" json:"dex_address"`       // EasySwapOrderBook 合约地址
	VaultAddress string `toml:"vault_address" mapstructure:"vault_address" json:"vault_address"` // EasySwapVault 合约地址
}

// Monitor 监控配置
type Monitor struct {
	PprofEnable bool  `toml:"pprof_enable" mapstructure:"pprof_enable" json:"pprof_enable"` // 是否启用 pprof 性能分析
	PprofPort   int64 `toml:"pprof_port" mapstructure:"pprof_port" json:"pprof_port"`       // pprof HTTP 端口，如 6060
}

// AnkrCfg 区块链 RPC 节点配置
// 用于连接区块链网络，获取链上数据
type AnkrCfg struct {
	ApiKey       string `toml:"api_key" mapstructure:"api_key" json:"api_key"`                   // RPC 服务 API Key
	HttpsUrl     string `toml:"https_url" mapstructure:"https_url" json:"https_url"`             // HTTPS RPC URL，如 "https://sepolia.infura.io/v3/"
	WebsocketUrl string `toml:"websocket_url" mapstructure:"websocket_url" json:"websocket_url"` // WebSocket RPC URL（用于实时订阅）
	EnableWss    bool   `toml:"enable_wss" mapstructure:"enable_wss" json:"enable_wss"`          // 是否启用 WebSocket 连接
}

// ProjectCfg 项目配置
type ProjectCfg struct {
	Name string `toml:"name" mapstructure:"name" json:"name"` // 项目名称，用于区分多个项目的数据
}

// KvConf Redis 缓存配置
type KvConf struct {
	Redis []*Redis `toml:"redis" json:"redis"` // Redis 节点列表，支持集群模式
}

// Redis 单个 Redis 节点配置
type Redis struct {
	Host string `toml:"host" json:"host"` // Redis 地址，如 "localhost:6379"
	Type string `toml:"type" json:"type"` // 连接类型，"node" 或 "cluster"
	Pass string `toml:"pass" json:"pass"` // 密码（可选）
}

// LogLevel 日志级别配置（已弃用，使用 logging.LogConf）
type LogLevel struct {
	Api      string `toml:"api" json:"api"`     // API 层日志级别
	DataBase string `toml:"db" json:"db"`       // 数据库层日志级别
	Utils    string `toml:"utils" json:"utils"` // 工具层日志级别
}

// UnmarshalConfig 从指定路径读取并解析配置文件
// @param configFilePath: 配置文件的完整路径
// @return: 解析后的配置对象和可能的错误
func UnmarshalConfig(configFilePath string) (*Config, error) {
	// 设置配置文件路径
	viper.SetConfigFile(configFilePath)
	// 设置配置文件格式为 TOML
	viper.SetConfigType("toml")
	// 自动读取环境变量
	viper.AutomaticEnv()
	// 设置环境变量前缀为 "CNFT"
	// 例如：CNFT_DB_HOST 会覆盖 db.host 配置
	viper.SetEnvPrefix("CNFT")
	// 将配置键中的 "." 替换为 "_" 以匹配环境变量
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)

	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	// 将配置文件内容解析到 Config 结构体
	var c Config
	if err := viper.Unmarshal(&c); err != nil {
		return nil, err
	}

	return &c, nil
}

// UnmarshalCmdConfig 从 viper 已加载的配置中解析
// 注意：调用此函数前，需要先通过 cmd/root.go 中的 initConfig() 加载配置
// @return: 解析后的配置对象和可能的错误
func UnmarshalCmdConfig() (*Config, error) {
	// 读取 viper 中已经设置的配置文件
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var c Config

	// 将配置解析到结构体
	if err := viper.Unmarshal(&c); err != nil {
		return nil, err
	}

	return &c, nil
}
