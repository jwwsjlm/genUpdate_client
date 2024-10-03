package main

import (
	"flag"
	"fmt"
	"github.com/duke-git/lancet/v2/fileutil"
	"github.com/dustin/go-humanize"
	"os"
	"strings"
)

var baseURL string

var appName string

func main() {

	defer func() {
		fmt.Println("====================================================================")
		fmt.Println("程序运行完毕，按 Enter 键以退出...")
		var input string
		fmt.Scanln(&input)
	}()
	if appName == "" || baseURL == "" {
		flag.StringVar(&baseURL, "url", "", "你的域名")
		flag.StringVar(&appName, "name", "", "你的软件名称")
		flag.Parse()
		fmt.Println("appName,baseURL 未设置，请设置后再运行程序。")
		return
	}
	if appName == "" || baseURL == "" {
		fmt.Println("appName,baseURL 未设置，请设置后再运行程序。")
		return
	}
	content, err := getUpdateContent(baseURL + "/updateList/" + appName)
	if err != nil {
		fmt.Println("访问失败", err)
		return

	}
	if content.Ret != "ok" {
		fmt.Println("返回ret失败", content.Ret)
		return

	}
	fmt.Printf("软件名称:%s \n", content.AppList.ReleaseNote.AppName)
	fmt.Printf("软件公告:%s \n", content.AppList.ReleaseNote.Description)
	fmt.Printf("软件版本:%s \n", content.AppList.ReleaseNote.Version)
	fmt.Printf("运行之前,请确保'%s'相关软件已经关闭.如果更新失败,可尝试重启电脑之后,再次使用.\n输入Y继续运行,N为退出更新程序.\n", content.AppList.ReleaseNote.AppName)
	var isRun bool
	for {
		var input string
		fmt.Print("请输入 Y 或 N: ")
		_, err := fmt.Scanln(&input)
		if err != nil {
			return
		}
		if err != nil {
			fmt.Println("读取输入时出错:", err)
			continue
		}
		input = strings.TrimSpace(input) // 去掉换行符和空格
		switch strings.ToUpper(input) {
		case "Y":
			isRun = true
			break // 退出循环
		case "N":
			isRun = false
			break // 退出循环
		default:
			fmt.Println("无效输入，请输入 Y 或 N")
			continue // 继续下一次循环
		}
		break // 退出 for 循环
	}
	if !isRun {
		os.Exit(0)
	}
	for _, v := range content.AppList.FileList {
		downloadURL := baseURL + v.DownloadURL
		relativePath, err := extractRelativePath(v.Path, appName)
		if err != nil {
			fmt.Println("解析路径出错:", err)
			continue
		}
		fmt.Println("--------------------------------------------------------------------")
		if fileutil.IsExist(relativePath) {
			sha, err := fileutil.Sha(relativePath, 256)
			if err != nil {
				fmt.Printf("计算 SHA256 错误:%s,重新下载\n", err)
			} else if sha != v.Sha256 {
				fmt.Printf("文件名:[%s],	但已存在,本地和云端不一致,准备重新下载\n", v.Name)
			} else {
				fmt.Printf("文件名:[%s],	已存在,且本地和云版本sha256一致,跳过下载\n", v.Name)
				continue
			}

		}

		fmt.Print("开始下载文件:[" + v.Name + "]\n" + "文件sha256:" + v.Sha256 + "\n" + "文件大小:" + humanize.Bytes(uint64(v.Size)) + "\n")
		err = downloadFile(downloadURL, relativePath, v.Size)
		if err != nil {
			fmt.Printf("文件下载失败: %s, 错误: %v\n", v.Name, err)
			continue

		}

		fmt.Printf("\n文件名:%s,下载完成\n", v.Name)
	}
}
