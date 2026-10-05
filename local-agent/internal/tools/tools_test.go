package tools

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPathJailAndRoundTrip(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	req := Request{Workspace: root}
	if got := Execute(context.Background(), req, "read_file", map[string]any{"path": filepath.Join("..", filepath.Base(outside), "secret.txt")}); !strings.Contains(got, "outside") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "read_file", map[string]any{"path": secret}); !strings.Contains(got, "outside") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "write_file", map[string]any{"path": "notes/hello.txt", "content": "hello"}); !strings.Contains(got, "wrote") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "read_file", map[string]any{"path": "notes/hello.txt"}); got != "hello" {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "list_dir", map[string]any{"path": "."}); !strings.Contains(got, "notes/") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "delete_file", map[string]any{"path": "notes"}); !strings.Contains(got, "only removes files") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "delete_file", map[string]any{"path": "notes/hello.txt"}); !strings.Contains(got, "deleted") {
		t.Fatal(got)
	}
}

func TestSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink test runs where unprivileged symlinks are available")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("hidden"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	got := Execute(context.Background(), Request{Workspace: root}, "read_file", map[string]any{"path": "link/secret.txt"})
	if !strings.Contains(got, "outside") {
		t.Fatal(got)
	}
}

func TestShellGateAndBlocklist(t *testing.T) {
	root := t.TempDir()
	req := Request{Workspace: root}
	if got := Execute(context.Background(), req, "shell", map[string]any{"command": "echo hi"}); !strings.Contains(got, "disabled") {
		t.Fatal(got)
	}
	req.AllowShell = true
	if got := Execute(context.Background(), req, "shell", map[string]any{"command": "echo hi"}); !strings.Contains(got, "hi") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), req, "shell", map[string]any{"command": "rm -rf /"}); !strings.Contains(got, "blocked") {
		t.Fatal(got)
	}
	if got := blockedShell("echo do-not-shutdown-the-word"); got != "" && !strings.Contains("echo do-not-shutdown-the-word", "shutdown") {
		t.Fatal(got)
	}
	if blockedShell("shutdown -h now") == "" {
		t.Fatal("expected power control to be blocked")
	}
}

func TestFetchURLStripsHTML(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Hello &amp; co</h1><script>alert(1)</script></body></html>`))
	}))
	defer srv.Close()
	got := Execute(context.Background(), Request{}, "fetch_url", map[string]any{"url": srv.URL})
	if strings.Contains(got, "alert") || !strings.Contains(got, "Hello & co") {
		t.Fatal(got)
	}
	if got := Execute(context.Background(), Request{}, "fetch_url", map[string]any{"url": "file:///etc/passwd"}); !strings.Contains(got, "http") {
		t.Fatal(got)
	}
}
