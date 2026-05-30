package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/imroc/req/v3"
	"github.com/schollz/progressbar/v3"
)

type Options struct {
	BaseURL            string
	AppName            string
	Token              string
	ProcessName        string
	WaitProcessTimeout time.Duration
	Writer             io.Writer
	Progress           bool
}

type Client struct {
	baseURL            string
	appName            string
	token              string
	processName        string
	waitProcessTimeout time.Duration
	writer             io.Writer
	progress           bool
	http               *req.Client
}

type fileAction int

const (
	fileSkipped fileAction = iota
	fileDownloaded
)

func New(options Options) (*Client, error) {
	if strings.TrimSpace(options.BaseURL) == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if strings.TrimSpace(options.AppName) == "" {
		return nil, fmt.Errorf("app name is required")
	}
	if _, err := BuildUpdateListURL(options.BaseURL, options.AppName); err != nil {
		return nil, err
	}
	writer := options.Writer
	if writer == nil {
		writer = io.Discard
	}

	return &Client{
		baseURL:            strings.TrimSpace(options.BaseURL),
		appName:            strings.TrimSpace(options.AppName),
		token:              strings.TrimSpace(options.Token),
		processName:        strings.TrimSpace(options.ProcessName),
		waitProcessTimeout: options.WaitProcessTimeout,
		writer:             writer,
		progress:           options.Progress,
		http: req.C().
			SetTimeout(2*time.Minute).
			SetCommonRetryCount(2).
			SetCommonRetryBackoffInterval(500*time.Millisecond, 3*time.Second),
	}, nil
}

func (c *Client) FetchManifest(ctx context.Context) (Manifest, error) {
	updateListURL, err := BuildUpdateListURL(c.baseURL, c.appName)
	if err != nil {
		return Manifest{}, err
	}

	resp, err := c.authorize(c.http.R().SetContext(ctx)).Get(updateListURL)
	if err != nil {
		return Manifest{}, fmt.Errorf("failed to send request: %w, url:%s", err, updateListURL)
	}
	if !resp.IsSuccessState() {
		return Manifest{}, fmt.Errorf("request failed with status code: %d, url:%s", resp.StatusCode, updateListURL)
	}

	var manifest Manifest
	if err = resp.UnmarshalJson(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	if manifest.Ret != "ok" {
		return Manifest{}, fmt.Errorf("server returned ret=%s", manifest.Ret)
	}
	return manifest, nil
}

func (c *Client) Run(ctx context.Context) (Result, error) {
	manifest, err := c.FetchManifest(ctx)
	if err != nil {
		return Result{}, err
	}
	return c.Update(ctx, manifest)
}

func (c *Client) Update(ctx context.Context, manifest Manifest) (Result, error) {
	if c.processName != "" {
		if err := waitForProcessExit(ctx, c.processName, c.waitProcessTimeout, c.writer); err != nil {
			return Result{}, err
		}
	}

	files := manifest.AppList.FileList
	result := Result{Total: len(files)}

	fileBar := c.newFileProgressBar(len(files))
	if fileBar != nil {
		defer fileBar.Finish()
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		action, err := c.updateFile(ctx, file)
		if err != nil {
			result.Failed = append(result.Failed, FileError{File: file, Err: err})
			c.printf("文件下载失败: %s, 错误: %v\n", file.Name, err)
			addProgress(fileBar)
			continue
		}

		if action == fileDownloaded {
			result.Downloaded++
		} else {
			result.Skipped++
		}
		addProgress(fileBar)
	}

	if len(result.Failed) > 0 {
		return result, UpdateError{Failures: result.Failed}
	}
	return result, nil
}

func (c *Client) updateFile(ctx context.Context, file File) (fileAction, error) {
	downloadURL := JoinURL(c.baseURL, file.DownloadURL)
	relativePath, err := ExtractRelativePath(file.Path, c.appName)
	if err != nil {
		return fileSkipped, fmt.Errorf("failed to resolve file path: %w", err)
	}

	c.println("--------------------------------------------------------------------")
	if fileExists(relativePath) {
		localSize, err := fileSize(relativePath)
		if err != nil {
			c.printf("读取本地文件大小错误:%s, 重新下载\n", err)
		} else if localSize != file.Size {
			c.printf("文件名[%s], 已存在，但大小不一致，准备重新下载\n", file.Name)
		} else {
			sha, err := CalculateFileSHA256(relativePath)
			if err != nil {
				c.printf("计算 SHA256 错误:%s, 重新下载\n", err)
			} else if !strings.EqualFold(sha, file.Sha256) {
				c.printf("文件名[%s], 已存在，但本地和云端不一致，准备重新下载\n", file.Name)
			} else {
				c.printf("文件名[%s], 已存在，且本地和云端 SHA256 一致，跳过下载\n", file.Name)
				return fileSkipped, nil
			}
		}
	}

	c.printf("开始下载文件[%s]\n文件 SHA256:%s\n文件大小:%s\n", file.Name, file.Sha256, humanize.Bytes(uint64(file.Size)))
	if err := c.downloadFile(ctx, downloadURL, relativePath, file.Size, file.Sha256); err != nil {
		return fileSkipped, err
	}
	c.printf("\n文件名%s, 下载完成并校验通过\n", file.Name)
	return fileDownloaded, nil
}

func (c *Client) downloadFile(ctx context.Context, url, file string, size int64, expectedSHA256 string) (err error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	tmpFile := file + ".tmp"
	bar := c.newDownloadProgressBar(size, file)
	if bar != nil {
		defer func() {
			if cerr := bar.Finish(); cerr != nil && err == nil {
				err = fmt.Errorf("failed to finish progress bar: %w", cerr)
			}
		}()
	}
	defer func() {
		if err != nil {
			_ = os.Remove(tmpFile)
		}
	}()

	callback := func(info req.DownloadInfo) {
		if bar != nil && info.Response.Response != nil {
			_ = bar.Set64(info.DownloadedSize)
		}
	}

	resp, err := c.authorize(c.http.R()).
		SetContext(ctx).
		SetOutputFile(tmpFile).
		SetDownloadCallbackWithInterval(callback, 100*time.Millisecond).
		Get(url)
	if err != nil {
		return fmt.Errorf("failed to download file from %s: %w", url, err)
	}
	if !resp.IsSuccessState() {
		return fmt.Errorf("download failed with status code: %d, url:%s", resp.StatusCode, url)
	}

	info, err := os.Stat(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to stat downloaded file: %w", err)
	}
	if size >= 0 && info.Size() != size {
		return fmt.Errorf("downloaded file size mismatch: expected %d, got %d", size, info.Size())
	}

	actualSHA256, err := CalculateFileSHA256(tmpFile)
	if err != nil {
		return fmt.Errorf("failed to calculate downloaded file sha256: %w", err)
	}
	if !strings.EqualFold(actualSHA256, expectedSHA256) {
		return fmt.Errorf("sha256 mismatch: expected %s, got %s", expectedSHA256, actualSHA256)
	}

	if err := replaceFile(tmpFile, file); err != nil {
		return fmt.Errorf("failed to replace target file: %w", err)
	}
	return nil
}

func BuildUpdateListURL(base, app string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("url must include scheme and host")
	}
	return url.JoinPath(u.String(), "updateList", app)
}

func JoinURL(base, path string) string {
	u, err := url.Parse(path)
	if err == nil && u.IsAbs() {
		return u.String()
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func ExtractRelativePath(fullPath, baseDir string) (string, error) {
	fullPath = filepath.Clean(fullPath)
	baseDir = filepath.Clean(baseDir)
	if filepath.IsAbs(fullPath) || filepath.IsAbs(baseDir) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}

	relPath, err := filepath.Rel(baseDir, fullPath)
	if err != nil {
		return "", fmt.Errorf("failed to extract relative path: %w", err)
	}
	if relPath == "." {
		return "", fmt.Errorf("file path points to the app root, not a file")
	}
	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("base directory is not a prefix of the full path")
	}
	if filepath.IsAbs(relPath) || filepath.VolumeName(relPath) != "" {
		return "", fmt.Errorf("resolved path is not relative")
	}

	return relPath, nil
}

func CalculateFileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	buf := make([]byte, 1024*1024)
	if _, err := io.CopyBuffer(h, f, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (c *Client) newDownloadProgressBar(size int64, file string) *progressbar.ProgressBar {
	if !c.progress {
		return nil
	}
	return progressbar.NewOptions64(size,
		progressbar.OptionSetWriter(c.writer),
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
}

func (c *Client) newFileProgressBar(total int) *progressbar.ProgressBar {
	if !c.progress {
		return nil
	}
	if total < 0 {
		total = 0
	}
	return progressbar.NewOptions(total,
		progressbar.OptionSetWriter(c.writer),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetDescription("整体进度:[cyan][文件]...[reset]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "[green]=[reset]",
			SaucerHead:    "[red]>[reset]",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

func addProgress(bar *progressbar.ProgressBar) {
	if bar != nil {
		_ = bar.Add(1)
	}
}

func (c *Client) printf(format string, args ...any) {
	if c.writer != nil {
		_, _ = fmt.Fprintf(c.writer, format, args...)
	}
}

func (c *Client) println(args ...any) {
	if c.writer != nil {
		_, _ = fmt.Fprintln(c.writer, args...)
	}
}

func (c *Client) authorize(request *req.Request) *req.Request {
	if c.token == "" {
		return request
	}
	return request.SetHeader("Authorization", "Bearer "+c.token)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	if info.IsDir() {
		return 0, fmt.Errorf("%s is a directory", path)
	}
	return info.Size(), nil
}

func replaceFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(src, dst)
}
