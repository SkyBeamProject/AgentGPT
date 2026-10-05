package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolateConfig(t *testing.T) {
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

func TestSaveRoundTripMasksNothingInFileButViewDoes(t *testing.T) {
	isolateConfig(t)
	cfg := Default()
	cfg.APIKey = "sk-test-secret-value"
	cfg.Workspace = filepath.Join(t.TempDir(), "work")
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.APIKey != "sk-test-secret-value" {
		t.Fatalf("key = %q", loaded.APIKey)
	}
	if loaded.KeySource != "file" {
		t.Fatalf("source = %q", loaded.KeySource)
	}
	view := PublicView(loaded, "test")
	raw, _ := os.ReadFile(mustPath(t))
	if strings.Contains(string(raw), "Public") {
		t.Fatal("unexpected")
	}
	if view.HasAPIKey != true || strings.Contains(view.Model, "sk-test") {
		t.Fatalf("view leaked or lost key flag: %+v", view)
	}
	info, err := os.Stat(mustPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o", info.Mode().Perm())
	}
}

func TestEnvFillsEmptyKeyOnly(t *testing.T) {
	isolateConfig(t)
	t.Setenv("OPENAI_API_KEY", "from-env")
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.APIKey != "from-env" || loaded.KeySource != "env" {
		t.Fatalf("%+v", loaded)
	}
	loaded.APIKey = "from-file"
	loaded.Workspace = filepath.Join(t.TempDir(), "work")
	if err := Save(loaded); err != nil {
		t.Fatal(err)
	}
	again, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if again.APIKey != "from-file" || again.KeySource != "file" {
		t.Fatalf("%+v", again)
	}
}

func TestApplyUpdateKeepsKeyUntilCleared(t *testing.T) {
	cur := Default()
	cur.APIKey = "secret"
	cur.Workspace = filepath.Join(t.TempDir(), "work")
	model := "demo-model"
	next, err := ApplyUpdate(cur, Update{Model: &model})
	if err != nil {
		t.Fatal(err)
	}
	if next.APIKey != "secret" || next.Model != "demo-model" {
		t.Fatalf("%+v", next)
	}
	cleared, err := ApplyUpdate(next, Update{ClearAPIKey: true})
	if err != nil {
		t.Fatal(err)
	}
	if cleared.APIKey != "" {
		t.Fatal(cleared.APIKey)
	}
}

func TestPresetAndAgents(t *testing.T) {
	cur := Default()
	cur.Workspace = filepath.Join(t.TempDir(), "work")
	next, err := ApplyPreset(cur, "Ollama")
	if err != nil {
		t.Fatal(err)
	}
	if next.BaseURL != "http://127.0.0.1:11434/v1" || next.Model != "llama3.2" {
		t.Fatalf("%+v", next)
	}
	next, err = UpsertAgent(next, AgentProfile{Name: "Researcher", Persona: "Write short briefs."})
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Agents) != 2 {
		t.Fatalf("%d agents", len(next.Agents))
	}
	if _, err := UpsertAgent(next, AgentProfile{Name: "../x", Persona: "nope"}); err == nil {
		t.Fatal("expected invalid name")
	}
	next, err = RemoveAgent(next, "Researcher")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveAgent(next, "General"); err == nil {
		t.Fatal("expected to keep the last agent")
	}
}

func TestRejectSystemWorkspaces(t *testing.T) {
	if err := rejectWorkspace(`/etc/passwd`, "linux"); err == nil {
		t.Fatal("expected /etc to be rejected")
	}
	if err := rejectWorkspace(`/`, "linux"); err == nil {
		t.Fatal("expected root to be rejected")
	}
	if err := rejectWorkspace(`/home/me/Beam Agent`, "linux"); err != nil {
		t.Fatal(err)
	}
	if err := rejectWorkspace(`C:\Windows\System32`, "windows"); err == nil {
		t.Fatal("expected Windows directory to be rejected")
	}
	if err := rejectWorkspace(`C:\`, "windows"); err == nil {
		t.Fatal("expected drive root to be rejected")
	}
	if err := rejectWorkspace(`C:\Users\me\Documents\Beam Agent`, "windows"); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureWorkspaceCreatesDedicatedFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "notes")
	got, err := EnsureWorkspace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("got %s", got)
	}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		t.Fatal(err)
	}
}

func mustPath(t *testing.T) string {
	t.Helper()
	p, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
