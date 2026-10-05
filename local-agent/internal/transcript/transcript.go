// Package transcript writes a finished run into the workspace.
package transcript

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/agent"
)

// Write saves a markdown transcript and returns its path relative to the workspace.
func Write(workspace, agentName, goal string, events []agent.Event) (string, error) {
	dir := filepath.Join(workspace, "beam-runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := time.Now().Format("20060102-150405") + ".md"
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", safeLine(agentName))
	fmt.Fprintf(&b, "Goal: %s\n\n", strings.TrimSpace(goal))
	for _, ev := range events {
		switch ev.Kind {
		case "action":
			fmt.Fprintf(&b, "## Step %d · %s\n\n%s\n\n", ev.Step, ev.Tool, strings.TrimSpace(ev.Thought))
		case "observation":
			fmt.Fprintf(&b, "```\n%s\n```\n\n", ev.Observation)
		case "done":
			fmt.Fprintf(&b, "## Finished\n\n%s\n", strings.TrimSpace(ev.Summary))
		case "error":
			fmt.Fprintf(&b, "## Error\n\n%s\n", strings.TrimSpace(ev.Error))
		case "status":
			fmt.Fprintf(&b, "%s\n\n", strings.TrimSpace(ev.Observation))
		}
	}
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(b.String()), 0o644); err != nil {
		return "", err
	}
	return filepath.Join("beam-runs", name), nil
}

func safeLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if s == "" {
		return "Beam Agent"
	}
	return s
}
