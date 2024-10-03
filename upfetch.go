package main

import (
	"fmt"
	"github.com/dustin/go-humanize"
	"github.com/imroc/req/v3"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var once sync.Once
var client *req.Client
var symbols = []string{"|", "/", "-", "\\"}

func init() {
	once.Do(func() {
		client = req.C()
	})

}
func getUpdateContent(Url string) (JSONData, error) {
	resp, err := client.R().Get(Url)
	if err != nil {
		return JSONData{}, fmt.Errorf("failed to send request: %w,Url:%s", err, Url)
	}
	if !resp.IsSuccessState() {
		return JSONData{}, fmt.Errorf("request failed with status code: %d,Url:%s", resp.StatusCode, Url)
	}
	var data JSONData
	err = resp.UnmarshalJson(&data)
	if err != nil {
		return JSONData{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return data, nil
}

func downloadFile(url, file string) error {
	var symbolIndex = 0
	//size := 100 * 1024 // 100 KB
	//url = fmt.Sprintf("https://httpbin.org/bytes/%d", size)
	//开始时间
	startTime := time.Now()
	callback := func(info req.DownloadInfo) {
		if info.Response.Response != nil {
			progress := float64(info.DownloadedSize) / float64(info.Response.ContentLength) * 100.0
			elapsedTime := time.Since(startTime).Seconds()
			downloadSpeed := float64(info.DownloadedSize) / elapsedTime
			fmt.Printf("\r%s 下载进度: %.2f%%, 下载速度: %s /s", symbols[symbolIndex], progress, humanize.Bytes(uint64(downloadSpeed)))
			symbolIndex = (symbolIndex + 1) % len(symbols)

		}
		//fmt.Printf("文件名:%s,下载完成\n", info.Response.Header.)
	}

	_, err := client.R().
		SetOutputFile(file).
		SetDownloadCallbackWithInterval(callback, 50*time.Millisecond).
		Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}
	return nil

}
func extractRelativePath(fullPath, baseDir string) (string, error) {
	// Normalize paths
	fullPath = filepath.Clean(fullPath)
	baseDir = filepath.Clean(baseDir)

	// Use filepath.Rel to get the relative path
	relPath, err := filepath.Rel(baseDir, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract relative path: %w", err)
	}

	// Check if the path starts with ".." which means baseDir is not a prefix of fullPath
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("base directory is not a prefix of the full path")
	}

	return relPath, nil
}
