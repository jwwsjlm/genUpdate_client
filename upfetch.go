package main

import (
	"fmt"
	"github.com/imroc/req/v3"
)

func getUpdateContent(Url string) (JSONData, error) {

	client := req.C()
	resp, err := client.R().Get(Url)
	if err != nil {
		return JSONData{}, err
	}
	if !resp.IsSuccessState() {
		return JSONData{}, fmt.Errorf("request failed with status code: %d", resp.StatusCode)
	}

	var data JSONData
	err = resp.UnmarshalJson(&data)
	if err != nil {
		return JSONData{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return data, nil
}

func downloadFile(url, file string) error {
	client := req.C()
	//size := 100 * 1024 // 100 KB
	//url = fmt.Sprintf("https://httpbin.org/bytes/%d", size)
	callback := func(info req.DownloadInfo) {
		if info.Response.Response != nil {
			fmt.Printf("\r下载进度: %.2f%%", float64(info.DownloadedSize)/float64(info.Response.ContentLength)*100.0)
		}
		//fmt.Printf("文件名:%s,下载完成\n", info.Response.Header.)
	}

	_, err := client.R().
		SetOutputFile(file).
		SetDownloadCallback(callback).
		Get(url)
	if err != nil {
		return err
	}
	return nil

}
