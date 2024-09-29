package main

import (
	"fmt"
	"github.com/duke-git/lancet/v2/fileutil"
	"log"
)

const baseURL = "https://up.975135.xyz"
const appname = "星月"

func main() {
	content, err := getUpdateContent(baseURL + "/updateList/" + appname)
	if err != nil {
		panic(err)
	}
	if content.Ret != "ok" {
		panic(content.Ret)
	}
	fmt.Printf("软件名称:%s \r\n", content.AppList.FileName)
	fmt.Printf("软件公告:%s \r\n", content.AppList.ReleaseNote.Description)
	fmt.Printf("软件版本:%s \r\n", content.AppList.ReleaseNote.Version)

	for _, v := range content.AppList.FileList {
		if fileutil.IsExist(v.Path) {
			sha, _ := fileutil.Sha(v.Path, 256)
			if sha == v.Sha256 {
				fmt.Printf("文件名:%s,已存在,且本地和云版本sha256一致\n", v.Name)
				continue
			}
			fmt.Printf("文件名:%s,已存在,本地和云端不一致,准备下载\n", v.Name)
		}

		fmt.Print("开始下载文件:" + v.Name + "\n" + "文件sha256:" + v.Sha256 + "\n")
		err := downloadFile(baseURL+v.DownloadURL, v.Path)
		if err != nil {
			log.Printf("文件下载失败: %s, 错误: %v\n", v.Name, err)
			continue

		}

		fmt.Printf("\r文件名:%s,下载完成\n", v.Name)
	}

}
