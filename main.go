package main

import (
	"fmt"
	"github.com/duke-git/lancet/v2/fileutil"
)

const baseURL = "https://up.975135.xyz"

var appName string

func main() {
	defer func() {
		fmt.Println("====================================================================")
		fmt.Println("程序运行完毕，按 Enter 键以退出...")
		var input string
		fmt.Scanln(&input)
	}()

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

	for _, v := range content.AppList.FileList {
		downloadURL := baseURL + v.DownloadURL
		relativePath, err := extractRelativePath(v.Path, appName)
		if err != nil {
			fmt.Println("解析路径出错:", err)
			continue
		}
		fmt.Println("--------------------------------------------------------------------")
		if fileutil.IsExist(relativePath) {
			sha, _ := fileutil.Sha(relativePath, 256)
			if sha == v.Sha256 {
				fmt.Printf("文件名:%s,	已存在,且本地和云版本sha256一致\n", v.Name)
				continue
			}
			fmt.Printf("文件名:%s,	已存在,本地和云端不一致,准备下载\n", v.Name)
		}

		fmt.Print("开始下载文件:" + v.Name + "\n" + "文件sha256:" + v.Sha256 + "\n")
		err = downloadFile(downloadURL, relativePath)
		if err != nil {
			fmt.Printf("文件下载失败: %s, 错误: %v\n", v.Name, err)
			continue

		}

		fmt.Printf("\n文件名:%s,下载完成\n", v.Name)
	}
}
