package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imroc/req/v3"
	"github.com/k0kubun/go-ansi"
	"github.com/schollz/progressbar/v3"
)

var once sync.Once
var client *req.Client

func init() {
	once.Do(func() {
		client = req.C()
	})
}

func getUpdateContent(url string) (JSONData, error) {
	resp, err := client.R().Get(url)
	if err != nil {
		return JSONData{}, fmt.Errorf("failed to send request: %w, url:%s", err, url)
	}
	if !resp.IsSuccessState() {
		return JSONData{}, fmt.Errorf("request failed with status code: %d, url:%s", resp.StatusCode, url)
	}
	var data JSONData
	if err = resp.UnmarshalJson(&data); err != nil {
		return JSONData{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return data, nil
}

// NewProgressBar 创建一个进度条用于显示下载进度
func NewProgressBar(size int64, file string) *progressbar.ProgressBar {
	b := progressbar.NewOptions64(size,
		progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetSpinnerChangeInterval(0),
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetDescription("正在下载:[yellow]["+filepath.Base(file)+"]...[reset]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[red]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
	b.Reset()
	return b
}

// 开始下载文件。下载到临时文件，校验 SHA256 后再原子替换目标文件。
func downloadFile(url, file string, size int64, expectedSHA256 string) (err error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	tmpFile := file + ".tmp"
	bar := NewProgressBar(size, file)
	defer func() {
		if cerr := bar.Finish(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to finish progress bar: %w", cerr)
		}
	}()
	defer func() {
		if err != nil {
			_ = os.Remove(tmpFile)
		}
	}()

	callback := func(info req.DownloadInfo) {
		if info.Response.Response != nil {
			_ = bar.Set64(info.DownloadedSize)
		}
	}

	_, err = client.R().
		SetOutputFile(tmpFile).
		SetDownloadCallbackWithInterval(callback, 100*time.Millisecond).
		Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}

	actualSHA256, err := calculateFileSHA256(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to calculate downloaded file sha256: %w", err)
	}
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256)
	}

	if err := os.Rename(tmpFile, file); err != nil {
		return fmt.Errorf("failed to replace target file: %w", err)
	}
	return nil
}

func extractRelativePath(fullPath, baseDir string) (string, error) {
	fullPath = filepath.Clean(fullPath)
	baseDir = filepath.Clean(baseDir)

	relPath, err := filepath.Rel(baseDir, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract relative path: %w", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return "", fmt.Errorf("base directory is not a prefix of the full path")
	}

	return relPath, nil
}

func calculateFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
