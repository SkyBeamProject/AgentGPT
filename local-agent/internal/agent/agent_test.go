package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDecision(t *testing.T) {
	raw := "```json\n{\"thought\":\"ok\",\"tool\":\"DONE\",\"args\":{\"summary\":\"yes\"}}\n```"
	d, err := ParseDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tool != "finish" || d.Args["summary"] != "yes" {
		t.Fatalf("%+v", d)
	}
	raw = "<think>secret</think>\n{\"thought\":\"read\",\"tool\":\"read\",\"args\":{\"path\":\"a.txt\"}}"
	d, err = ParseDecision(raw)
	if err != nil {
		t.Fatal(err)
	}
	if d.Tool != "read_file" {
		t.Fatal(d.Tool)
	}
	if _, err := ParseDecision("hello"); err == nil {
		t.Fatal("expected error")
	}
}

func TestSystemPromptOmitsShellUntilEnabled(t *testing.T) {
	off := SystemPrompt(PromptInput{Name: "Ada", Persona: "Be precise.", Workspace: "/tmp/work"})
	if !strings.Contains(off, "Be precise.") || !strings.Contains(off, "Shell is disabled") {
		t.Fatal(off)
	}
	if strings.Contains(off, `tool":"shell"`) || strings.Contains(off, "- shell:") {
		t.Fatal("shell tool listed while disabled")
	}
	on := SystemPrompt(PromptInput{Name: "Ada", Workspace: "/tmp/work", AllowShell: true})
	if !strings.Contains(on, "- shell:") {
		t.Fatal(on)
	}
}

func TestMockRunWritesNote(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BEAM.md"), []byte("Prefer short notes."), 0o644); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	res, err := Run(context.Background(), Request{
		Name:      "General",
		Persona:   "test",
		Goal:      "Leave a note",
		Workspace: dir,
		MaxSteps:  6,
		Decider:   MockDecider{},
	}, func(ev Event) { kinds = append(kinds, ev.Kind) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.Finished || !strings.Contains(res.Summary, "beam-practice-note.txt") {
		t.Fatalf("%+v", res)
	}
	b, err := os.ReadFile(filepath.Join(dir, "beam-practice-note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Leave a note") {
		t.Fatalf("note = %s", b)
	}
	if len(kinds) < 4 {
		t.Fatalf("events = %v", kinds)
	}
}

func TestInvalidRepliesStop(t *testing.T) {
	res, err := Run(context.Background(), Request{
		Goal:      "anything",
		Workspace: t.TempDir(),
		MaxSteps:  5,
		Decider:   &staticDecider{replies: []string{"nope", "still nope", "no"}},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Finished || res.Steps != 3 {
		t.Fatalf("%+v", res)
	}
}

type staticDecider struct {
	replies []string
	n       int
}

func (s *staticDecider) Decide(context.Context, string, string) (string, error) {
	if s.n >= len(s.replies) {
		return "nope", nil
	}
	out := s.replies[s.n]
	s.n++
	return out, nil
}

func TestReadBrief(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("Use tables."), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readBrief(dir); got != "Use tables." {
		t.Fatal(got)
	}
}
