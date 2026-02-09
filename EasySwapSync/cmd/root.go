/**
 * root.go - Cobra 命令行框架根命令
 *
 * 功能：
 *   - 定义 CLI 程序的根命令
 *   - 处理配置文件加载
 *   - 支持环境变量覆盖配置
 *
 * 使用方式：
 *   ./EasySwapSync daemon -c ./config/config.toml
 */
package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// cfgFile 配置文件路径，通过命令行参数 -c 或 --config 指定
var cfgFile string

// rootCmd 是根命令，当没有指定子命令时执行
// 通常不直接执行，而是作为子命令（如 daemon）的父命令
var rootCmd = &cobra.Command{
	Use:   "sync",         // 命令名称
	Short: "root server.", // 简短描述
	Long:  `root server.`, // 详细描述
	// 如果需要根命令执行某些操作，取消注释下面一行：
	// Run: func(cmd *cobra.Command, args []string) { },
}

// Execute 执行根命令
// 这个函数被 main.main() 调用，是整个 CLI 程序的入口
// 它会解析命令行参数并执行相应的命令
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	fmt.Println("cfgFile=", cfgFile)
}

// init 初始化函数，在 main() 之前自动执行
// 用于设置命令行参数和配置加载逻辑
func init() {
	// 注册配置初始化函数，在执行任何命令之前调用
	cobra.OnInitialize(initConfig)

	// 定义全局持久化参数（对所有子命令生效）
	flags := rootCmd.PersistentFlags()

	// -c / --config 参数：指定配置文件路径
	// 默认值：./config/config_import.toml
	flags.StringVarP(&cfgFile, "config", "c", "./config/config_import.toml", "config file (default is $HOME/.config_import.toml)")
}

// initConfig 读取配置文件和环境变量
// 配置加载优先级：命令行参数 > 环境变量 > 配置文件
func initConfig() {
	if cfgFile != "" {
		// 使用命令行参数指定的配置文件
		viper.SetConfigFile(cfgFile)
	} else {
		// 如果没有指定配置文件，从用户主目录查找
		// 例如：/Users/username/ 或 C:\Users\username\
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// 在主目录下查找名为 "config_import" 的配置文件
		viper.AddConfigPath(home)
		viper.SetConfigName("config_import")
	}

	// 自动读取匹配的环境变量
	// 例如：EasySwap_DB_HOST 会覆盖配置中的 db.host
	viper.AutomaticEnv()

	// 设置配置文件格式为 TOML
	viper.SetConfigType("toml")

	// 设置环境变量前缀，避免与其他程序冲突
	// 只有以 "EasySwap_" 开头的环境变量会被读取
	viper.SetEnvPrefix("EasySwap")

	// 将配置路径中的 "." 替换为 "_" 以支持嵌套配置
	// 例如：db.host -> EasySwap_DB_HOST
	replacer := strings.NewReplacer(".", "_")
	viper.SetEnvKeyReplacer(replacer)

	// 读取配置文件
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	} else {
		// 配置文件读取失败，程序无法继续
		panic(err)
	}
}
