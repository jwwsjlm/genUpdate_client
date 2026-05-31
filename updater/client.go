package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
	Concurrency        int
	Writer             io.Writer
	Progress           bool
}

type Client struct {
	baseURL            string
	appName            string
	token              string
	processName        string
	waitProcessTimeout time.Duration
	concurrency        int
	writer             io.Writer
	progress           bool
	http               *req.Client
	outputMu           sync.Mutex
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
	concurrency := options.Concurrency
	if concurrency < 1 {
		concurrency = 1
	}

	return &Client{
		baseURL:            strings.TrimSpace(options.BaseURL),
		appName:            strings.TrimSpace(options.AppName),
		token:              strings.TrimSpace(options.Token),
		processName:        strings.TrimSpace(options.ProcessName),
		waitProcessTimeout: options.WaitProcessTimeout,
		concurrency:        concurrency,
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

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		action, err := c.updateFile(ctx, file)
		c.recordFileResult(&result, file, action, err)
		c.printFileResult(result, file, err)
	}

	if len(result.Failed) > 0 {
		return result, UpdateError{Failures: result.Failed}
	}
	return result, nil
}

func (c *Client) recordFileResult(result *Result, file File, action fileAction, err error) {
	if err != nil {
		result.Failed = append(result.Failed, FileError{File: file, Err: err})
		return
	}
	if action == fileDownloaded {
		result.Downloaded++
		return
	}
	result.Skipped++
}

func (c *Client) printFileResult(result Result, file File, err error) {
	if err != nil {
		c.printf("文件下载失败: %s, 错误: %v\n", file.Name, err)
	}
	c.printf("剩余待更新文件:%d (已下载:%d, 已跳过:%d, 失败:%d)\n", result.remaining(), result.Downloaded, result.Skipped, len(result.Failed))
}

func (c *Client) updateFile(ctx context.Context, file File) (fileAction, error) {
	downloadURL, err := BuildDownloadURL(c.baseURL, file.DownloadURL)
	if err != nil {
		return fileSkipped, err
	}
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
	c.printf("文件名%s, 下载完成并校验通过\n", file.Name)
	return fileDownloaded, nil
}

func (c *Client) downloadFile(ctx context.Context, url, file string, size int64, expectedSHA256 string) (err error) {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	tmpFile := file + ".tmp"
	defer func() {
		if err != nil {
			_ = os.Remove(tmpFile)
		}
	}()

	if expectedSHA256 != "" {
		if _, err := hex.DecodeString(expectedSHA256); err != nil {
			return fmt.Errorf("invalid sha256 checksum %q: %w", expectedSHA256, err)
		}
	}
	if err := c.downloadToTemp(ctx, url, tmpFile, size, file); err != nil {
		return err
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

func (c *Client) downloadToTemp(ctx context.Context, downloadURL, tmpFile string, size int64, targetFile string) error {
	if c.concurrency > 1 && size > 0 {
		return c.downloadToTempParallel(ctx, downloadURL, tmpFile, size, targetFile)
	}

	return c.downloadToTempSequential(ctx, downloadURL, tmpFile, size, targetFile)
}

func (c *Client) downloadToTempSequential(ctx context.Context, downloadURL, tmpFile string, size int64, targetFile string) (err error) {
	for attempt := 0; attempt < 2; attempt++ {
		offset := int64(0)
		if info, statErr := os.Stat(tmpFile); statErr == nil {
			offset = info.Size()
			if size >= 0 && offset > size {
				_ = os.Remove(tmpFile)
				offset = 0
			}
		}

		if size >= 0 && offset == size {
			return nil
		}

		bar := c.newDownloadProgressBar(size, targetFile)
		if bar != nil {
			_ = bar.Set64(offset)
			defer func() {
				if cerr := bar.Finish(); cerr != nil && err == nil {
					err = fmt.Errorf("failed to finish progress bar: %w", cerr)
				}
			}()
		}
		request := c.authorize(c.http.R().
			SetContext(ctx).
			DisableAutoReadResponse())
		if offset > 0 {
			request.SetHeader("Range", fmt.Sprintf("bytes=%d-", offset))
		}

		response, err := request.Get(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download file from %s: %w", downloadURL, err)
		}
		if response.Body == nil {
			return fmt.Errorf("download response body is empty, url:%s", downloadURL)
		}

		if offset > 0 && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
			_ = response.Body.Close()
			_ = os.Remove(tmpFile)
			continue
		}
		if offset > 0 && response.StatusCode == http.StatusOK {
			offset = 0
			_ = os.Remove(tmpFile)
			if bar != nil {
				_ = bar.Set64(0)
			}
		}
		if offset > 0 && response.StatusCode != http.StatusPartialContent {
			_ = response.Body.Close()
			return fmt.Errorf("resume failed with status code: %d, url:%s", response.StatusCode, downloadURL)
		}
		if (offset == 0 && response.StatusCode < 200) || response.StatusCode >= 300 {
			_ = response.Body.Close()
			return fmt.Errorf("download failed with status code: %d, url:%s", response.StatusCode, downloadURL)
		}

		flags := os.O_CREATE | os.O_WRONLY
		if offset > 0 {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		out, err := os.OpenFile(tmpFile, flags, 0o644)
		if err != nil {
			_ = response.Body.Close()
			return fmt.Errorf("failed to open temporary file: %w", err)
		}

		writer := io.Writer(out)
		if bar != nil {
			writer = io.MultiWriter(out, progressWriter{add: func(n int) { _ = bar.Add(n) }})
		}
		_, copyErr := io.CopyBuffer(writer, response.Body, make([]byte, 1024*1024))
		closeBodyErr := response.Body.Close()
		closeFileErr := out.Close()
		if copyErr != nil {
			return fmt.Errorf("failed to write temporary file: %w", copyErr)
		}
		if closeBodyErr != nil {
			return fmt.Errorf("failed to close response body: %w", closeBodyErr)
		}
		if closeFileErr != nil {
			return fmt.Errorf("failed to close temporary file: %w", closeFileErr)
		}

		return nil
	}
	return fmt.Errorf("failed to resume download after retry")
}

func (c *Client) downloadToTempParallel(ctx context.Context, downloadURL, tmpFile string, size int64, targetFile string) error {
	_ = os.Remove(tmpFile)

	workers := c.concurrency
	if workers < 1 {
		workers = 1
	}
	if int64(workers) > size {
		workers = int(size)
	}
	if workers <= 1 {
		return c.downloadToTempSequential(ctx, downloadURL, tmpFile, size, targetFile)
	}

	if ok, err := c.supportsRange(ctx, downloadURL, size); err != nil {
		return err
	} else if !ok {
		return c.downloadToTempSequential(ctx, downloadURL, tmpFile, size, targetFile)
	}

	c.cleanupParts(tmpFile, workers)
	bar := c.newDownloadProgressBar(size, targetFile)
	if bar != nil {
		defer func() {
			_ = bar.Finish()
		}()
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	partSize := (size + int64(workers) - 1) / int64(workers)
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	var barMu sync.Mutex
	for i := 0; i < workers; i++ {
		start := int64(i) * partSize
		end := start + partSize - 1
		if end >= size {
			end = size - 1
		}
		if start > end {
			continue
		}

		partFile := fmt.Sprintf("%s.part%d", tmpFile, i)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := c.downloadRange(ctx, downloadURL, partFile, start, end, bar, &barMu); err != nil {
				cancel()
				errCh <- err
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		c.cleanupParts(tmpFile, workers)
		return err
	}

	if err := c.mergeParts(tmpFile, workers); err != nil {
		c.cleanupParts(tmpFile, workers)
		return err
	}
	return nil
}

func (c *Client) supportsRange(ctx context.Context, downloadURL string, size int64) (bool, error) {
	resp, err := c.authorize(c.http.R().
		SetContext(ctx).
		SetHeader("Range", "bytes=0-0").
		DisableAutoReadResponse()).
		Get(downloadURL)
	if err != nil {
		return false, fmt.Errorf("failed to probe range support: %w", err)
	}
	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	if resp.StatusCode != http.StatusPartialContent {
		return false, nil
	}
	contentRange := resp.Header.Get("Content-Range")
	return strings.HasSuffix(contentRange, fmt.Sprintf("/%d", size)), nil
}

func (c *Client) downloadRange(ctx context.Context, downloadURL, partFile string, start, end int64, bar *progressbar.ProgressBar, barMu *sync.Mutex) error {
	_ = os.Remove(partFile)
	request := c.authorize(c.http.R().
		SetContext(ctx).
		SetHeader("Range", fmt.Sprintf("bytes=%d-%d", start, end)).
		DisableAutoReadResponse())

	response, err := request.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download range %d-%d: %w", start, end, err)
	}
	if response.Body == nil {
		return fmt.Errorf("download response body is empty, range:%d-%d", start, end)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("range download failed with status code: %d, range:%d-%d", response.StatusCode, start, end)
	}

	out, err := os.OpenFile(partFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open part file: %w", err)
	}
	defer out.Close()

	writer := io.Writer(out)
	if bar != nil {
		writer = io.MultiWriter(out, progressWriter{add: func(n int) {
			barMu.Lock()
			defer barMu.Unlock()
			_ = bar.Add(n)
		}})
	}
	if _, err := io.CopyBuffer(writer, response.Body, make([]byte, 1024*1024)); err != nil {
		return fmt.Errorf("failed to write part file: %w", err)
	}
	if info, err := out.Stat(); err != nil {
		return fmt.Errorf("failed to stat part file: %w", err)
	} else if want := end - start + 1; info.Size() != want {
		return fmt.Errorf("range size mismatch %d-%d: expected %d, got %d", start, end, want, info.Size())
	}
	return nil
}

func (c *Client) mergeParts(tmpFile string, workers int) error {
	out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer out.Close()

	for i := 0; i < workers; i++ {
		partFile := fmt.Sprintf("%s.part%d", tmpFile, i)
		part, err := os.Open(partFile)
		if err != nil {
			return fmt.Errorf("failed to open part file: %w", err)
		}
		if _, err := io.CopyBuffer(out, part, make([]byte, 1024*1024)); err != nil {
			_ = part.Close()
			return fmt.Errorf("failed to merge part file: %w", err)
		}
		if err := part.Close(); err != nil {
			return fmt.Errorf("failed to close part file: %w", err)
		}
		_ = os.Remove(partFile)
	}
	return nil
}

func (c *Client) cleanupParts(tmpFile string, workers int) {
	for i := 0; i < workers; i++ {
		_ = os.Remove(fmt.Sprintf("%s.part%d", tmpFile, i))
	}
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
	joined, err := BuildDownloadURL(base, path)
	if err == nil {
		return joined
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func BuildDownloadURL(base, path string) (string, error) {
	baseURL, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return "", fmt.Errorf("base url must include scheme and host")
	}

	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		if !sameOrigin(baseURL, u) {
			return "", fmt.Errorf("download url host %q does not match base host %q", u.Host, baseURL.Host)
		}
		return u.String(), nil
	}
	return url.JoinPath(baseURL.String(), strings.TrimLeft(path, "/"))
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
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
	return c.newRawDownloadProgressBar(size, file)
}

func (c *Client) newRawDownloadProgressBar(size int64, file string) *progressbar.ProgressBar {
	return progressbar.NewOptions64(size,
		progressbar.OptionSetWriter(c.lockedWriter()),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionSetWidth(20),
		progressbar.OptionSetSpinnerChangeInterval(0),
		progressbar.OptionThrottle(200*time.Millisecond),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionSetElapsedTime(false),
		progressbar.OptionSetDescription("正在下载 ["+filepath.Base(file)+"]"),
		progressbar.OptionOnCompletion(func() {
			_, _ = c.lockedWriter().Write([]byte("\n"))
		}),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

func (c *Client) printf(format string, args ...any) {
	if c.writer != nil {
		c.outputMu.Lock()
		defer c.outputMu.Unlock()
		_, _ = fmt.Fprintf(c.writer, format, args...)
	}
}

func (c *Client) println(args ...any) {
	if c.writer != nil {
		c.outputMu.Lock()
		defer c.outputMu.Unlock()
		_, _ = fmt.Fprintln(c.writer, args...)
	}
}

func (c *Client) lockedWriter() io.Writer {
	return lockedWriter{
		writer: c.writer,
		mu:     &c.outputMu,
	}
}

func (c *Client) authorize(request *req.Request) *req.Request {
	if c.token == "" {
		return request
	}
	return request.SetHeader("Authorization", "Bearer "+c.token)
}

type progressWriter struct {
	add func(int)
}

func (w progressWriter) Write(p []byte) (int, error) {
	if w.add != nil {
		w.add(len(p))
	}
	return len(p), nil
}

type lockedWriter struct {
	writer io.Writer
	mu     *sync.Mutex
}

func (w lockedWriter) Write(p []byte) (int, error) {
	if w.writer == nil {
		return len(p), nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
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
