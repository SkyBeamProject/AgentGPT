package agent

import (
	"context"
	"encoding/json"
	"strings"
)

// MockDecider walks a fixed practice run: list the workspace, write a note, finish.
type MockDecider struct{}

// Decide ignores the model server and returns the next practice step.
func (MockDecider) Decide(_ context.Context, _, user string) (string, error) {
	goal := goalFromPrompt(user)
	if !strings.Contains(user, "tool: list_dir") {
		return mustDecision(Decision{
			Thought: "Look at the workspace before writing.",
			Tool:    "list_dir",
			Args:    map[string]any{"path": "."},
		}), nil
	}
	if !strings.Contains(user, "tool: write_file") {
		return mustDecision(Decision{
			Thought: "Save a note the user can keep.",
			Tool:    "write_file",
			Args: map[string]any{
				"path":    "beam-practice-note.txt",
				"content": "Practice note\n\nGoal: " + goal + "\n\nThis file was written by Beam Agent without calling a model.\n",
			},
		}), nil
	}
	return mustDecision(Decision{
		Thought: "The note is in the workspace.",
		Tool:    "finish",
		Args:    map[string]any{"summary": "Practice run finished. Wrote beam-practice-note.txt."},
	}), nil
}

func goalFromPrompt(user string) string {
	const prefix = "Goal:\n"
	if !strings.HasPrefix(user, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(user, prefix)
	if i := strings.Index(rest, "\n\n"); i >= 0 {
		return strings.TrimSpace(rest[:i])
	}
	return strings.TrimSpace(rest)
}

func mustDecision(d Decision) string {
	b, err := json.Marshal(d)
	if err != nil {
		return `{"thought":"mock error","tool":"finish","args":{"summary":"Practice run could not build a reply."}}`
	}
	return string(b)
}
