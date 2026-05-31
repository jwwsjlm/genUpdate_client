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

	if c.concurrency > 1 {
		return c.updateConcurrent(ctx, files, result)
	}

	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		action, err := c.updateFile(ctx, file, nil)
		c.recordFileResult(&result, file, action, err)
		c.printFileResult(nil, result, file, err)
	}

	if len(result.Failed) > 0 {
		return result, UpdateError{Failures: result.Failed}
	}
	return result, nil
}

func (c *Client) updateConcurrent(ctx context.Context, files []File, result Result) (Result, error) {
	workerCount := c.concurrency
	if workerCount > len(files) {
		workerCount = len(files)
	}
	if workerCount < 1 {
		workerCount = 1
	}

	progress := c.newConcurrentProgress()
	if progress != nil {
		defer progress.Close()
	}
	c.progressPrintf(progress, "并发下载模式: 共%d 个文件，并发数:%d\n", len(files), workerCount)

	jobs := make(chan File)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				if err := ctx.Err(); err != nil {
					mu.Lock()
					result.Failed = append(result.Failed, FileError{File: file, Err: err})
					snapshot := result
					mu.Unlock()
					c.printFileResult(progress, snapshot, file, err)
					continue
				}

				action, err := c.updateFile(ctx, file, progress)
				mu.Lock()
				c.recordFileResult(&result, file, action, err)
				snapshot := result
				mu.Unlock()
				c.printFileResult(progress, snapshot, file, err)
			}
		}()
	}

sendJobs:
	for _, file := range files {
		select {
		case <-ctx.Done():
			break sendJobs
		case jobs <- file:
		}
	}
	close(jobs)
	wg.Wait()

	if len(result.Failed) > 0 {
		return result, UpdateError{Failures: result.Failed}
	}
	return result, ctx.Err()
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

func (c *Client) printFileResult(progress *concurrentProgress, result Result, file File, err error) {
	if err != nil {
		c.progressPrintf(progress, "文件下载失败: %s, 错误: %v\n", file.Name, err)
	}
	c.progressPrintf(progress, "剩余待更新文件:%d (已下载:%d, 已跳过:%d, 失败:%d)\n", result.remaining(), result.Downloaded, result.Skipped, len(result.Failed))
}

func (c *Client) updateFile(ctx context.Context, file File, progress *concurrentProgress) (fileAction, error) {
	downloadURL, err := BuildDownloadURL(c.baseURL, file.DownloadURL)
	if err != nil {
		return fileSkipped, err
	}
	relativePath, err := ExtractRelativePath(file.Path, c.appName)
	if err != nil {
		return fileSkipped, fmt.Errorf("failed to resolve file path: %w", err)
	}

	c.progressPrintln(progress, "--------------------------------------------------------------------")
	if fileExists(relativePath) {
		localSize, err := fileSize(relativePath)
		if err != nil {
			c.progressPrintf(progress, "读取本地文件大小错误:%s, 重新下载\n", err)
		} else if localSize != file.Size {
			c.progressPrintf(progress, "文件名[%s], 已存在，但大小不一致，准备重新下载\n", file.Name)
		} else {
			sha, err := CalculateFileSHA256(relativePath)
			if err != nil {
				c.progressPrintf(progress, "计算 SHA256 错误:%s, 重新下载\n", err)
			} else if !strings.EqualFold(sha, file.Sha256) {
				c.progressPrintf(progress, "文件名[%s], 已存在，但本地和云端不一致，准备重新下载\n", file.Name)
			} else {
				c.progressPrintf(progress, "文件名[%s], 已存在，且本地和云端 SHA256 一致，跳过下载\n", file.Name)
				return fileSkipped, nil
			}
		}
	}

	actionText := "开始下载文件"
	if progress != nil {
		actionText = "准备下载文件"
	}
	c.progressPrintf(progress, "%s[%s]\n文件 SHA256:%s\n文件大小:%s\n", actionText, file.Name, file.Sha256, humanize.Bytes(uint64(file.Size)))
	if err := c.downloadFile(ctx, downloadURL, relativePath, file.Size, file.Sha256, progress); err != nil {
		return fileSkipped, err
	}
	c.progressPrintf(progress, "\n文件名%s, 下载完成并校验通过\n", file.Name)
	return fileDownloaded, nil
}

func (c *Client) downloadFile(ctx context.Context, url, file string, size int64, expectedSHA256 string, progress *concurrentProgress) (err error) {
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
	if err := c.downloadToTemp(ctx, url, tmpFile, size, file, progress); err != nil {
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

func (c *Client) downloadToTemp(ctx context.Context, downloadURL, tmpFile string, size int64, targetFile string, progress *concurrentProgress) (err error) {
	reporter := newConcurrentDownloadReporter(progress, size, targetFile)
	if reporter != nil {
		defer func() {
			reporter.Finish(err == nil)
		}()
	}

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
		if reporter != nil {
			reporter.Set(offset)
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
			} else if reporter != nil {
				reporter.Set(0)
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
		} else if reporter != nil {
			writer = io.MultiWriter(out, progressWriter{add: reporter.Add})
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
	if !c.progress || c.concurrency > 1 {
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
		progressbar.OptionSetPredictTime(true),
		progressbar.OptionSetDescription("正在下载 ["+filepath.Base(file)+"]"),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
	)
}

func (c *Client) newConcurrentProgress() *concurrentProgress {
	if !c.progress || c.concurrency <= 1 {
		return nil
	}
	progress := &concurrentProgress{
		client: c,
		events: make(chan progressEvent, c.concurrency*16),
		done:   make(chan struct{}),
		files:  make(map[string]*progressState),
		order:  make([]string, 0, c.concurrency),
	}
	go progress.run()
	return progress
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

func (c *Client) progressPrintf(progress *concurrentProgress, format string, args ...any) {
	if progress != nil {
		progress.Print(func() {
			c.printf(format, args...)
		})
		return
	}
	c.printf(format, args...)
}

func (c *Client) progressPrintln(progress *concurrentProgress, args ...any) {
	if progress != nil {
		progress.Print(func() {
			c.println(args...)
		})
		return
	}
	c.println(args...)
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

type concurrentProgress struct {
	client      *Client
	events      chan progressEvent
	done        chan struct{}
	files       map[string]*progressState
	order       []string
	current     string
	bar         *progressbar.ProgressBar
	barID       string
	lastLineLen int
}

type progressState struct {
	name     string
	total    int64
	current  int64
	finished bool
}

func (s *progressState) remaining() int64 {
	if s == nil || s.total <= 0 {
		return 1<<63 - 1
	}
	remaining := s.total - s.current
	if remaining < 0 {
		return 0
	}
	return remaining
}

type progressEvent struct {
	id       string
	name     string
	total    int64
	current  int64
	delta    int64
	set      bool
	finished bool
	print    func()
	done     chan struct{}
}

func (e progressEvent) shouldRender() bool {
	return e.finished || e.delta > 0 || e.current > 0
}

func newConcurrentDownloadReporter(progress *concurrentProgress, size int64, file string) *downloadReporter {
	if progress == nil || size <= 0 {
		return nil
	}
	id := file
	name := filepath.Base(file)
	progress.Send(progressEvent{id: id, name: name, total: size, current: 0, set: true})
	return &downloadReporter{id: id, progress: progress}
}

func (p *concurrentProgress) Send(event progressEvent) {
	select {
	case p.events <- event:
	case <-p.done:
		if event.done != nil {
			close(event.done)
		}
	}
}

func (p *concurrentProgress) Close() {
	close(p.events)
	<-p.done
}

func (p *concurrentProgress) Print(print func()) {
	if print == nil {
		return
	}
	done := make(chan struct{})
	p.Send(progressEvent{print: print, done: done})
	<-done
}

func (p *concurrentProgress) run() {
	defer close(p.done)
	for event := range p.events {
		if event.print != nil {
			p.print(event.print)
			if event.done != nil {
				close(event.done)
			}
			continue
		}
		p.apply(event)
		if event.shouldRender() {
			p.render(event.id)
		}
	}
	if p.bar != nil {
		_ = p.bar.Clear()
	}
}

func (p *concurrentProgress) print(print func()) {
	p.clearRenderedLine()
	print()
}

func (p *concurrentProgress) apply(event progressEvent) {
	state := p.files[event.id]
	if state == nil {
		state = &progressState{name: event.name, total: event.total}
		p.files[event.id] = state
		p.order = append(p.order, event.id)
	}
	if event.name != "" {
		state.name = event.name
	}
	if event.total > 0 {
		state.total = event.total
	}
	if event.set {
		state.current = event.current
	}
	if event.delta > 0 {
		state.current += event.delta
	}
	if state.current > state.total && state.total > 0 {
		state.current = state.total
	}
	if event.finished {
		state.finished = true
	}
}

func (p *concurrentProgress) render(eventID string) {
	state := p.files[eventID]
	if p.shouldSwitchTo(eventID, state) {
		p.selectCurrent()
	}
	if p.current == "" {
		p.clearBar()
		return
	}

	currentState := p.files[p.current]
	if p.bar == nil || p.barID != p.current {
		p.replaceBar(p.current, currentState)
	}
	p.lastLineLen = p.renderedLineLen(currentState)
	_ = p.bar.Set64(currentState.current)
}

func (p *concurrentProgress) renderedLineLen(state *progressState) int {
	if state == nil {
		return 0
	}
	return len([]rune("正在下载 ["+state.name+"]")) + 96
}

func (p *concurrentProgress) shouldSwitchTo(eventID string, eventState *progressState) bool {
	if p.current == "" || p.files[p.current] == nil || p.files[p.current].finished {
		return true
	}
	if eventState == nil || eventState.finished || eventID == p.current {
		return false
	}
	current := p.files[p.current]
	return eventState.remaining() < current.remaining()
}

func (p *concurrentProgress) replaceBar(id string, state *progressState) {
	p.clearBar()
	p.bar = p.client.newRawDownloadProgressBar(state.total, state.name)
	p.barID = id
}

func (p *concurrentProgress) clearBar() {
	if p.bar == nil {
		return
	}
	_ = p.bar.Clear()
	p.bar = nil
	p.barID = ""
	p.lastLineLen = 0
}

func (p *concurrentProgress) clearRenderedLine() {
	if p.lastLineLen > 0 {
		padding := strings.Repeat(" ", p.lastLineLen)
		p.client.printf("\r%s\r", padding)
	}
	if p.bar != nil {
		_ = p.bar.Clear()
	}
}

func (p *concurrentProgress) selectCurrent() {
	var selectedID string
	var selectedState *progressState
	for _, id := range p.order {
		state := p.files[id]
		if state == nil || state.finished {
			continue
		}
		if selectedState == nil || state.remaining() < selectedState.remaining() {
			selectedID = id
			selectedState = state
		}
	}
	p.current = selectedID
}

type downloadReporter struct {
	id       string
	progress *concurrentProgress
}

func (r *downloadReporter) Set(current int64) {
	r.progress.Send(progressEvent{id: r.id, current: current, set: true})
}

func (r *downloadReporter) Add(n int) {
	if n <= 0 {
		return
	}
	r.progress.Send(progressEvent{id: r.id, delta: int64(n)})
}

func (r *downloadReporter) Finish(success bool) {
	r.progress.Send(progressEvent{id: r.id, finished: true})
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
