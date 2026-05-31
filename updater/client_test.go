package updater

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
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
	err = client.downloadFile(context.Background(), server.URL, target, int64(len(body)+1), expectedSHA, nil)
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
	if err := client.downloadFile(context.Background(), server.URL, target, int64(len(body)), expectedSHA, nil); err != nil {
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

func TestConcurrentProgressPrioritizesNearestCompletion(t *testing.T) {
	client, err := New(Options{
		BaseURL:     "https://updates.example.com",
		AppName:     "app",
		Writer:      bytes.NewBuffer(nil),
		Progress:    true,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	progress := &concurrentProgress{
		client: client,
		events: make(chan progressEvent, 8),
		done:   make(chan struct{}),
		files:  make(map[string]*progressState),
		order:  make([]string, 0, 2),
	}

	progress.apply(progressEvent{id: "large", name: "large.bin", total: 100, current: 0, set: true})
	progress.apply(progressEvent{id: "small", name: "small.bin", total: 10, current: 0, set: true})
	progress.render("large")
	if progress.current != "small" || progress.barID != "small" {
		t.Fatalf("current = %q, barID = %q, want small", progress.current, progress.barID)
	}

	progress.apply(progressEvent{id: "small", current: 5, set: true})
	progress.render("small")
	if progress.current != "small" || progress.barID != "small" {
		t.Fatalf("current = %q, barID = %q, want small", progress.current, progress.barID)
	}

	progress.apply(progressEvent{id: "large", current: 50, set: true})
	progress.render("large")
	if progress.current != "small" || progress.barID != "small" {
		t.Fatalf("current = %q, barID = %q, want small", progress.current, progress.barID)
	}
	if got := progress.files["large"].current; got != 50 {
		t.Fatalf("large progress = %d, want 50", got)
	}

	progress.apply(progressEvent{id: "small", current: 10, set: true, finished: true})
	progress.render("small")
	if progress.current != "large" || progress.barID != "large" {
		t.Fatalf("current = %q, barID = %q, want large", progress.current, progress.barID)
	}
}

func TestConcurrentDownloadReporterFinishesOnceAcrossRangeRetry(t *testing.T) {
	client, err := New(Options{
		BaseURL:     "https://updates.example.com",
		AppName:     "app",
		Writer:      bytes.NewBuffer(nil),
		Progress:    true,
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}

	progress := client.newConcurrentProgress()
	if progress == nil {
		t.Fatalf("newConcurrentProgress returned nil")
	}

	target := filepath.Join(t.TempDir(), "payload.bin")
	if err := os.WriteFile(target+".tmp", []byte("partial"), 0o644); err != nil {
		t.Fatalf("failed to write temporary file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		_, _ = w.Write([]byte("fresh"))
	}))
	defer server.Close()

	err = client.downloadToTemp(context.Background(), server.URL, target+".tmp", int64(len("fresh")), target, progress)
	if err != nil {
		t.Fatalf("downloadToTemp returned error: %v", err)
	}

	progress.Close()
	state := progress.files[target]
	if state == nil || !state.finished {
		t.Fatalf("progress state = %+v, want finished", state)
	}
}

func sha256Hex(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
