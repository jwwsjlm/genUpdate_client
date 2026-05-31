package updater

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBuildUpdateListURL(t *testing.T) {
	got, err := BuildUpdateListURL("https://example.com/api/", "星月 更新")
	if err != nil {
		t.Fatalf("BuildUpdateListURL returned error: %v", err)
	}

	want := "https://example.com/api/updateList/%E6%98%9F%E6%9C%88%20%E6%9B%B4%E6%96%B0"
	if got != want {
		t.Fatalf("BuildUpdateListURL = %q, want %q", got, want)
	}
}

func TestExtractRelativePath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		base    string
		want    string
		wantErr bool
	}{
		{
			name: "normal file",
			path: filepath.Join("app", "data", "qqwry.dat"),
			base: "app",
			want: filepath.Join("data", "qqwry.dat"),
		},
		{
			name:    "sibling app is rejected",
			path:    filepath.Join("app2", "file.txt"),
			base:    "app",
			wantErr: true,
		},
		{
			name:    "parent traversal is rejected",
			path:    filepath.Join("app", "..", "file.txt"),
			base:    "app",
			wantErr: true,
		},
		{
			name:    "app root is rejected",
			path:    "app",
			base:    "app",
			wantErr: true,
		},
		{
			name: "dot-prefixed filename is allowed",
			path: filepath.Join("app", "..file"),
			base: "app",
			want: "..file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractRelativePath(tt.path, tt.base)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ExtractRelativePath returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ExtractRelativePath returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ExtractRelativePath = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildDownloadURLRejectsCrossOriginAbsoluteURL(t *testing.T) {
	_, err := BuildDownloadURL("https://updates.example.com", "https://evil.example.com/file.bin")
	if err == nil {
		t.Fatalf("BuildDownloadURL returned nil error")
	}
}

func TestBuildDownloadURLAllowsSameOriginAbsoluteURL(t *testing.T) {
	got, err := BuildDownloadURL("https://updates.example.com/base", "https://updates.example.com/download/file.bin")
	if err != nil {
		t.Fatalf("BuildDownloadURL returned error: %v", err)
	}
	want := "https://updates.example.com/download/file.bin"
	if got != want {
		t.Fatalf("BuildDownloadURL = %q, want %q", got, want)
	}
}

func TestClientRunDownloadsAndSkipsValidFiles(t *testing.T) {
	body := []byte("update payload")
	expectedSHA := sha256Hex(body)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/updateList/app":
			_ = json.NewEncoder(w).Encode(Manifest{
				Ret: "ok",
				AppList: AppList{
					ReleaseNote: ReleaseNote{AppName: "app", Version: "1.0.0"},
					FileList: []File{
						{
							Path:        filepath.ToSlash(filepath.Join("app", "payload.bin")),
							Name:        "payload.bin",
							Size:        int64(len(body)),
							Sha256:      expectedSHA,
							DownloadURL: "/download/payload.bin",
						},
					},
				},
			})
		case "/download/payload.bin":
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	workingDir := t.TempDir()
	t.Chdir(workingDir)

	client, err := New(Options{
		BaseURL: server.URL,
		AppName: "app",
		Writer:  bytes.NewBuffer(nil),
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Downloaded != 1 || result.Skipped != 0 || len(result.Failed) != 0 {
		t.Fatalf("first result = %+v", result)
	}

	result, err = client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error on second pass: %v", err)
	}
	if result.Downloaded != 0 || result.Skipped != 1 || len(result.Failed) != 0 {
		t.Fatalf("second result = %+v", result)
	}

	got, err := os.ReadFile(filepath.Join(workingDir, "payload.bin"))
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("target content = %q, want %q", string(got), string(body))
	}
}

func TestClientSendsBearerTokenToManifestAndDownload(t *testing.T) {
	body := []byte("protected payload")
	expectedSHA := sha256Hex(body)
	const token = "secret-token"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}

		switch r.URL.Path {
		case "/updateList/app":
			_ = json.NewEncoder(w).Encode(Manifest{
				Ret: "ok",
				AppList: AppList{
					ReleaseNote: ReleaseNote{AppName: "app", Version: "1.0.0"},
					FileList: []File{
						{
							Path:        filepath.ToSlash(filepath.Join("app", "protected.bin")),
							Name:        "protected.bin",
							Size:        int64(len(body)),
							Sha256:      expectedSHA,
							DownloadURL: "/download/protected.bin",
						},
					},
				},
			})
		case "/download/protected.bin":
			_, _ = w.Write(body)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Chdir(t.TempDir())

	client, err := New(Options{
		BaseURL: server.URL,
		AppName: "app",
		Token:   token,
		Writer:  bytes.NewBuffer(nil),
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := client.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Downloaded != 1 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchManifestVerifiesEd25519Signature(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	manifest := Manifest{
		Ret: "ok",
		AppList: AppList{
			FileName:    "app",
			ReleaseNote: ReleaseNote{AppName: "app", Version: "1.0.0"},
			FileList: []File{{
				Path:        filepath.ToSlash(filepath.Join("app", "payload.bin")),
				Name:        "payload.bin",
				Size:        7,
				Sha256:      sha256Hex([]byte("payload")),
				DownloadURL: "/download/payload.bin",
			}},
		},
		SignatureAlgorithm: "ed25519",
		SignatureKeyID:     "test-key",
	}
	payload, err := json.Marshal(manifest.AppList)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	manifest.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:           server.URL,
		AppName:           "app",
		ManifestPublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
		ManifestKeyID:     "test-key",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := client.FetchManifest(context.Background()); err != nil {
		t.Fatalf("FetchManifest returned error: %v", err)
	}
}

func TestFetchManifestRejectsInvalidSignature(t *testing.T) {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey returned error: %v", err)
	}
	manifest := Manifest{
		Ret:                "ok",
		AppList:            AppList{FileName: "app", ReleaseNote: ReleaseNote{AppName: "app"}},
		SignatureAlgorithm: "ed25519",
		SignatureKeyID:     "test-key",
	}
	payload, err := json.Marshal(manifest.AppList)
	if err != nil {
		t.Fatalf("Marshal returned error: %v", err)
	}
	manifest.Signature = base64.RawURLEncoding.EncodeToString(ed25519.Sign(otherPrivateKey, payload))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(manifest)
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:           server.URL,
		AppName:           "app",
		ManifestPublicKey: base64.RawURLEncoding.EncodeToString(publicKey),
		ManifestKeyID:     "test-key",
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, err := client.FetchManifest(context.Background()); err == nil {
		t.Fatalf("FetchManifest returned nil error")
	}
}

func TestNewClampsDownloadConcurrency(t *testing.T) {
	client, err := New(Options{
		BaseURL:     "https://updates.example.com",
		AppName:     "app",
		Concurrency: MaxDownloadConcurrency + 100,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if client.concurrency != MaxDownloadConcurrency {
		t.Fatalf("concurrency = %d, want %d", client.concurrency, MaxDownloadConcurrency)
	}
}

func TestDownloadFileRejectsSizeMismatch(t *testing.T) {
	body := []byte("short")
	expectedSHA := sha256Hex(body)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client, err := New(Options{BaseURL: server.URL, AppName: "app"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	target := filepath.Join(t.TempDir(), "payload.bin")
	err = client.downloadFile(context.Background(), server.URL, target, int64(len(body)+1), expectedSHA)
	if err == nil {
		t.Fatalf("downloadFile returned nil error")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after failed download: %v", statErr)
	}
	if _, statErr := os.Stat(target + ".tmp"); !os.IsNotExist(statErr) {
		t.Fatalf("temporary file exists after failed download: %v", statErr)
	}
}

func TestDownloadFileResumesPartialTemporaryFile(t *testing.T) {
	body := []byte("resume payload")
	expectedSHA := sha256Hex(body)
	offset := int64(6)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", "14")
			return
		}
		if r.Header.Get("Range") != "bytes=6-" {
			t.Fatalf("Range header = %q, want %q", r.Header.Get("Range"), "bytes=6-")
		}
		w.Header().Set("Content-Range", "bytes 6-13/14")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[offset:])
	}))
	defer server.Close()

	client, err := New(Options{BaseURL: server.URL, AppName: "app"})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	target := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(target+".tmp", body[:offset], 0o644); err != nil {
		t.Fatalf("failed to write temporary file: %v", err)
	}
	if err := client.downloadFile(context.Background(), server.URL, target, int64(len(body)), expectedSHA); err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("target content = %q, want %q", string(got), string(body))
	}
}

func TestDownloadFileUsesParallelRangeRequests(t *testing.T) {
	body := []byte("hello world")
	expectedSHA := sha256Hex(body)
	var mu sync.Mutex
	ranges := make([]string, 0, 4)
	var active int
	var maxActive int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			_, _ = w.Write(body)
			return
		}

		start, end, ok := parseRangeHeader(t, rangeHeader)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start < 0 || end >= int64(len(body)) || start > end {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}

		mu.Lock()
		ranges = append(ranges, rangeHeader)
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[start : end+1])

		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:     server.URL,
		AppName:     "app",
		Writer:      bytes.NewBuffer(nil),
		Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	target := filepath.Join(t.TempDir(), "payload.bin")
	if err := client.downloadFile(context.Background(), server.URL, target, int64(len(body)), expectedSHA); err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("target content = %q, want %q", string(got), string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	sort.Strings(ranges)
	wantRanges := []string{"bytes=0-0", "bytes=0-3", "bytes=4-7", "bytes=8-10"}
	sort.Strings(wantRanges)
	if strings.Join(ranges, ",") != strings.Join(wantRanges, ",") {
		t.Fatalf("range requests = %v, want %v", ranges, wantRanges)
	}
	if maxActive < 2 {
		t.Fatalf("range requests were not parallel, max active = %d", maxActive)
	}
}

func TestDownloadFileFallsBackWhenRangeUnsupported(t *testing.T) {
	body := []byte("fallback payload")
	expectedSHA := sha256Hex(body)
	var mu sync.Mutex
	rangeHeaders := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		rangeHeaders = append(rangeHeaders, r.Header.Get("Range"))
		mu.Unlock()
		_, _ = w.Write(body)
	}))
	defer server.Close()

	client, err := New(Options{
		BaseURL:     server.URL,
		AppName:     "app",
		Writer:      bytes.NewBuffer(nil),
		Concurrency: 3,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	target := filepath.Join(t.TempDir(), "payload.bin")
	if err := client.downloadFile(context.Background(), server.URL, target, int64(len(body)), expectedSHA); err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read target: %v", err)
	}
	if string(got) != string(body) {
		t.Fatalf("target content = %q, want %q", string(got), string(body))
	}

	mu.Lock()
	defer mu.Unlock()
	if len(rangeHeaders) != 2 {
		t.Fatalf("requests = %v, want probe plus sequential download", rangeHeaders)
	}
	if rangeHeaders[0] != "bytes=0-0" || rangeHeaders[1] != "" {
		t.Fatalf("range headers = %v, want [bytes=0-0 \"\"]", rangeHeaders)
	}
}

func TestUpdateProcessesFilesSequentiallyEvenWithConcurrency(t *testing.T) {
	first := []byte("first payload")
	second := []byte("second payload")
	var firstDone atomic.Bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/download/first.bin":
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write(first)
			firstDone.Store(true)
		case "/download/second.bin":
			if !firstDone.Load() {
				t.Fatalf("second download started before first download finished")
			}
			_, _ = w.Write(second)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Chdir(t.TempDir())
	client, err := New(Options{
		BaseURL:     server.URL,
		AppName:     "app",
		Writer:      bytes.NewBuffer(nil),
		Concurrency: 4,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	result, err := client.Update(context.Background(), Manifest{
		Ret: "ok",
		AppList: AppList{FileList: []File{
			{
				Path:        filepath.ToSlash(filepath.Join("app", "first.bin")),
				Name:        "first.bin",
				Size:        int64(len(first)),
				Sha256:      sha256Hex(first),
				DownloadURL: "/download/first.bin",
			},
			{
				Path:        filepath.ToSlash(filepath.Join("app", "second.bin")),
				Name:        "second.bin",
				Size:        int64(len(second)),
				Sha256:      sha256Hex(second),
				DownloadURL: "/download/second.bin",
			},
		}},
	})
	if err != nil {
		t.Fatalf("Update returned error: %v", err)
	}
	if result.Downloaded != 2 || result.Skipped != 0 || len(result.Failed) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func parseRangeHeader(t *testing.T, header string) (int64, int64, bool) {
	t.Helper()
	value, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return 0, 0, false
	}
	startText, endText, ok := strings.Cut(value, "-")
	if !ok {
		return 0, 0, false
	}

	var start, end int64
	if _, err := fmt.Sscanf(startText, "%d", &start); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(endText, "%d", &end); err != nil {
		return 0, 0, false
	}
	return start, end, true
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
