package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/duke-git/lancet/v2/fileutil"
	"github.com/dustin/go-humanize"
)

var baseURL string
var appName string
var autoYes bool
var skipWait bool

func init() {
	flag.StringVar(&baseURL, "url", "", "更新服务端地址，例如: https://example.com")
	flag.StringVar(&appName, "name", "", "软件名称")
	flag.BoolVar(&autoYes, "y", false, "自动确认更新，无需交互")
	flag.BoolVar(&skipWait, "no-wait", false, "程序结束后立即退出，不等待回车")
}

func main() {
	flag.Parse()

	defer waitForExit(skipWait)

	if appName == "" || baseURL == "" {
		fmt.Println("appName/baseURL 未设置，请通过 -name 和 -url 传入后再运行程序。")
		flag.PrintDefaults()
		return
	}

	content, err := getUpdateContent(baseURL + "/updateList/" + appName)
	if err != nil {
		fmt.Println("访问失败", err)
		return
	}
	if content.Ret != "ok" {
		fmt.Println("返回 ret 失败", content.Ret)
		return
	}

	fmt.Printf("软件名称:%s \n", content.AppList.ReleaseNote.AppName)
	fmt.Printf("软件公告:%s \n", content.AppList.ReleaseNote.Description)
	fmt.Printf("软件版本:%s \n", content.AppList.ReleaseNote.Version)

	if !autoYes {
		fmt.Printf("运行之前,请确保 '%s' 相关软件已经关闭。如果更新失败,可尝试重启电脑后再次使用。\n输入 Y 继续运行，N 退出更新程序。\n", content.AppList.ReleaseNote.AppName)
		if !confirmProceed() {
			os.Exit(0)
		}
	}

	for _, v := range content.AppList.FileList {
		downloadURL := joinURL(baseURL, v.DownloadURL)
		relativePath, err := extractRelativePath(v.Path, appName)
		if err != nil {
			fmt.Println("解析路径出错:", err)
			continue
		}

		fmt.Println("--------------------------------------------------------------------")
		if fileutil.IsExist(relativePath) {
			sha, err := fileutil.Sha(relativePath, 256)
			if err != nil {
				fmt.Printf("计算 SHA256 错误:%s, 重新下载\n", err)
			} else if sha != v.Sha256 {
				fmt.Printf("文件名:[%s], 已存在，但本地和云端不一致，准备重新下载\n", v.Name)
			} else {
				fmt.Printf("文件名:[%s], 已存在，且本地和云端 SHA256 一致，跳过下载\n", v.Name)
				continue
			}
		}

		fmt.Print("开始下载文件:[" + v.Name + "]\n" + "文件 SHA256:" + v.Sha256 + "\n" + "文件大小:" + humanize.Bytes(uint64(v.Size)) + "\n")
		err = downloadFile(downloadURL, relativePath, v.Size, v.Sha256)
		if err != nil {
			fmt.Printf("文件下载失败: %s, 错误: %v\n", v.Name, err)
			continue
		}

		fmt.Printf("\n文件名:%s, 下载完成并校验通过\n", v.Name)
	}
}

func confirmProceed() bool {
	for {
		var input string
		fmt.Print("请输入 Y 或 N: ")
		if _, err := fmt.Scanln(&input); err != nil {
			fmt.Println("读取输入时出错:", err)
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

func joinURL(base, path string) string {
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}
