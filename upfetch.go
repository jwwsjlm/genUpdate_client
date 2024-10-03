package main

import (
	"fmt"
	"github.com/imroc/req/v3"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
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

func downloadFile(url, file string, size int64) error {
	//bar := progressbar.DefaultBytes(size)
	bar := progressbar.NewOptions64(size,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()), //you should install "github.com/k0kubun/go-ansi"
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetSpinnerChangeInterval(0),
		progressbar.OptionSetPredictTime(true),
		//progressbar.OptionSetRenderBlankState(true),
		//progressbar.RenderBlank(),
		progressbar.OptionSetDescription("正在下载:["+filepath.Base(file)+"]..."),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[red]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	bar.Reset()
	//开始时间
	//startTime := time.Now()
	callback := func(info req.DownloadInfo) {
		if info.Response.Response != nil {
			//progress := float64(info.DownloadedSize) / float64(info.Response.ContentLength) * 100.0
			//elapsedTime := time.Since(startTime).Seconds()
			//downloadSpeed := float64(info.DownloadedSize) / elapsedTime
			bar.Set64(info.DownloadedSize)
			//fmt.Printf("\r%s 下载进度: %.2f%%, 下载速度: %s /s", symbols[symbolIndex], progress, humanize.Bytes(uint64(downloadSpeed)))

		}
		//fmt.Printf("文件名:%s,下载完成\n", info.Response.Header.)
	}

	_, err := client.R().
		SetOutputFile(file).
		SetDownloadCallbackWithInterval(callback, 100*time.Millisecond).
		Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}
	bar.Finish()
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
