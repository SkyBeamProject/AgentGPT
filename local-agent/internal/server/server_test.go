package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/agent"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/config"
)

func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "cfg"))
	t.Setenv("BEAM_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("BEAM_BASE_URL", "")
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("BEAM_MODEL", "")
}

func TestPageRequiresLoopbackAndToken(t *testing.T) {
	isolate(t)
	s := New(Options{Token: "tokentokentokentokentokentoken12", Version: "test"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.1.1.1:4000"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote status %d", rec.Code)
	}

	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "tokentokentokentokentokentoken12") {
		t.Fatalf("page status %d", res.StatusCode)
	}
	if strings.Contains(string(body), "__BEAM_TOKEN__") {
		t.Fatal("token placeholder was not replaced")
	}

	api, err := http.Get(srv.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	api.Body.Close()
	if api.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token status %d", api.StatusCode)
	}
}

func TestPracticeRunAndWorkspaceGuard(t *testing.T) {
	isolate(t)
	work := filepath.Join(t.TempDir(), "work")
	cfg := config.Default()
	cfg.Workspace = work
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	s := New(Options{Token: "tokentokentokentokentokentoken12", Version: "test"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	badBody := `{"workspace":"/etc"}`
	res, err := do(srv, http.MethodPut, "/api/config", badBody)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusBadRequest {
		t.Fatalf("system workspace status %d %s", res.Status, res.raw)
	}

	started, err := do(srv, http.MethodPost, "/api/runs", `{"agent":"General","goal":"Leave a note","mock":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if started.Status != http.StatusOK {
		t.Fatalf("start %d %s", started.Status, started.raw)
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(started.raw), &payload); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var last apiResult
	for time.Now().Before(deadline) {
		last, err = do(srv, http.MethodGet, "/api/runs/"+payload.ID+"?cursor=0", "")
		if err != nil {
			t.Fatal(err)
		}
		var view struct {
			Done bool `json:"done"`
		}
		if err := json.Unmarshal([]byte(last.raw), &view); err != nil {
			t.Fatal(err)
		}
		if view.Done {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(last.raw, "Practice run finished") {
		t.Fatalf("transcript: %s", last.raw)
	}
	note, err := os.ReadFile(filepath.Join(work, "beam-practice-note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(note), "Leave a note") {
		t.Fatalf("note %s", note)
	}
}

func TestStopRun(t *testing.T) {
	isolate(t)
	cfg := config.Default()
	cfg.APIKey = "test-key"
	cfg.Workspace = t.TempDir()
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	s := New(Options{
		Token:   "tokentokentokentokentokentoken12",
		Version: "test",
		Runner:  &blockRunner{started: started},
	})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	res, err := do(srv, http.MethodPost, "/api/runs", `{"agent":"General","goal":"wait"}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("start %d %s", res.Status, res.raw)
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(res.raw), &payload); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("runner did not start")
	}
	stopped, err := do(srv, http.MethodPost, "/api/runs/"+payload.ID+"/stop", "")
	if err != nil {
		t.Fatal(err)
	}
	if stopped.Status != http.StatusOK {
		t.Fatalf("stop %d %s", stopped.Status, stopped.raw)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		view, err := do(srv, http.MethodGet, "/api/runs/"+payload.ID+"?cursor=0", "")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(view.raw, "Stopped.") && strings.Contains(view.raw, `"done":true`) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("run did not stop")
}

type blockRunner struct {
	started chan struct{}
	once    sync.Once
}

func (b *blockRunner) Run(ctx context.Context, _ RunInput, emit func(agent.Event)) (agent.Result, error) {
	b.once.Do(func() { close(b.started) })
	emit(agent.Event{Kind: "status", Observation: "started"})
	<-ctx.Done()
	return agent.Result{}, ctx.Err()
}

type apiResult struct {
	Status int
	raw    string
}

func do(srv *httptest.Server, method, path, body string) (apiResult, error) {
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		return apiResult{}, err
	}
	req.Header.Set("X-Beam-Token", "tokentokentokentokentokentoken12")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		return apiResult{}, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return apiResult{}, err
	}
	return apiResult{Status: res.StatusCode, raw: string(raw)}, nil
}
