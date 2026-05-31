package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jwwsjlm/genUpdate_client/updater"
)

var baseURL string
var appName string
var autoYes bool
var skipWait bool
var noProgress bool
var showVersion bool
var configPath string
var targetProcess string
var updateToken string
var processWaitTimeout time.Duration
var concurrency int

var version = "dev"

type clientConfig struct {
	BaseURL                   string `json:"url"`
	AppName                   string `json:"name"`
	Token                     string `json:"token"`
	ProcessName               string `json:"process"`
	Concurrency               int    `json:"concurrency"`
	AutoYes                   *bool  `json:"autoYes"`
	SkipWait                  *bool  `json:"noWait"`
	NoProgress                *bool  `json:"noProgress"`
	ProcessWaitTimeoutSeconds int    `json:"waitProcessTimeoutSeconds"`
}

func init() {
	flag.StringVar(&baseURL, "url", "", "更新服务端地址，例如: https://example.com")
	flag.StringVar(&appName, "name", "", "软件名称")
	flag.BoolVar(&autoYes, "y", false, "自动确认更新，无需交互")
	flag.BoolVar(&skipWait, "no-wait", false, "程序结束后立即退出，不等待回车")
	flag.BoolVar(&noProgress, "no-progress", false, "关闭动态进度条，适合第三方程序调用或日志重定向")
	flag.BoolVar(&showVersion, "version", false, "显示更新器版本后退出")
	flag.StringVar(&configPath, "config", "", "配置文件路径，默认读取程序同目录 genUpdate_client.json（如果存在）")
	flag.StringVar(&updateToken, "token", "", "更新服务端访问 token")
	flag.StringVar(&targetProcess, "process", "", "更新前等待退出的目标进程名，例如: yourapp.exe")
	flag.DurationVar(&processWaitTimeout, "wait-timeout", 0, "等待目标进程退出的最长时间，例如: 2m；0 表示一直等待")
	flag.IntVar(&concurrency, "concurrency", 1, "单个文件的并发下载连接数，1 表示单连接")
}

func main() {
	flag.Parse()

	if showVersion {
		fmt.Printf("更新器版本:%s\n", version)
		return
	}

	if err := applyConfig(configPath); err != nil {
		fmt.Println("读取配置失败", err)
		return
	}

	defer waitForExit(skipWait)

	client, err := updater.New(updater.Options{
		BaseURL:            baseURL,
		AppName:            appName,
		Token:              updateToken,
		ProcessName:        targetProcess,
		WaitProcessTimeout: processWaitTimeout,
		Concurrency:        concurrency,
		Writer:             os.Stdout,
		Progress:           !noProgress,
	})
	if err != nil {
		fmt.Println("参数无效", err)
		flag.PrintDefaults()
		return
	}

	ctx := context.Background()
	manifest, err := client.FetchManifest(ctx)
	if err != nil {
		fmt.Println("访问失败", err)
		return
	}

	fmt.Printf("更新器版本:%s \n", version)
	fmt.Printf("软件名称:%s \n", manifest.AppList.ReleaseNote.AppName)
	fmt.Printf("软件公告:%s \n", manifest.AppList.ReleaseNote.Description)
	fmt.Printf("软件最新版本:%s \n", manifest.AppList.ReleaseNote.Version)

	if !autoYes {
		fmt.Printf("运行之前，请确认 '%s' 相关软件已经关闭。如果更新失败，可尝试重启电脑后再次使用。\n输入 Y 继续运行，N 退出更新程序。\n", manifest.AppList.ReleaseNote.AppName)
		if !confirmProceed() {
			os.Exit(0)
		}
	}

	result, err := client.Update(ctx, manifest)
	if err != nil {
		fmt.Printf("更新完成，但有文件失败: %v\n", err)
	}
	fmt.Printf("\n更新结果: 总计 %d, 下载 %d, 跳过 %d, 失败 %d\n", result.Total, result.Downloaded, result.Skipped, len(result.Failed))
}

func confirmProceed() bool {
	for {
		var input string
		fmt.Print("请输入 Y 或 N: ")
		if _, err := fmt.Scanln(&input); err != nil {
			fmt.Println("读取输入时出错", err)
			return false
		}

		input = strings.TrimSpace(input)
		switch strings.ToUpper(input) {
		case "Y":
			return true
		case "N":
			return false
		default:
			fmt.Println("无效输入，请输入 Y 或 N")
		}
	}
}

func waitForExit(skip bool) {
	if skip {
		return
	}

	fmt.Println("====================================================================")
	fmt.Println("程序运行完毕，倒计时 5 秒后退出...")
	for i := 5; i > 0; i-- {
		fmt.Printf("\r%d 秒后退出...", i)
		time.Sleep(1 * time.Second)
	}

	fmt.Println("\n按 Enter 键以退出...")
	_, _ = fmt.Scanln()
}

func applyConfig(path string) error {
	resolvedPath, required, err := resolveConfigPath(path)
	if err != nil {
		return err
	}
	if resolvedPath == "" {
		return nil
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var cfg clientConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("%s: %w", resolvedPath, err)
	}

	if cfg.BaseURL != "" && !flagProvided("url") {
		baseURL = cfg.BaseURL
	}
	if cfg.AppName != "" && !flagProvided("name") {
		appName = cfg.AppName
	}
	if cfg.Token != "" && !flagProvided("token") {
		updateToken = cfg.Token
	}
	if cfg.ProcessName != "" && !flagProvided("process") {
		targetProcess = cfg.ProcessName
	}
	if cfg.Concurrency > 0 && !flagProvided("concurrency") {
		concurrency = cfg.Concurrency
	}
	if cfg.AutoYes != nil && !flagProvided("y") {
		autoYes = *cfg.AutoYes
	}
	if cfg.SkipWait != nil && !flagProvided("no-wait") {
		skipWait = *cfg.SkipWait
	}
	if cfg.NoProgress != nil && !flagProvided("no-progress") {
		noProgress = *cfg.NoProgress
	}
	if cfg.ProcessWaitTimeoutSeconds > 0 && !flagProvided("wait-timeout") {
		processWaitTimeout = time.Duration(cfg.ProcessWaitTimeoutSeconds) * time.Second
	}
	return nil
}

func resolveConfigPath(path string) (resolvedPath string, required bool, err error) {
	if path != "" {
		return path, true, nil
	}

	exe, err := os.Executable()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(filepath.Dir(exe), "genUpdate_client.json"), false, nil
}

func flagProvided(name string) bool {
	provided := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			provided = true
		}
	})
	return provided
}
