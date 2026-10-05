package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PromptInput is the stable context for one run.
type PromptInput struct {
	Name       string
	Persona    string
	Workspace  string
	Brief      string
	AllowShell bool
}

// SystemPrompt tells the model who it is and which tools exist.
func SystemPrompt(in PromptInput) string {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = "Beam"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "You are %s, a local autonomous agent running on the user's computer.\n", name)
	if strings.TrimSpace(in.Persona) != "" {
		fmt.Fprintf(&b, "\nPersona:\n%s\n", strings.TrimSpace(in.Persona))
	}
	fmt.Fprintf(&b, "\nWorkspace (the only directory you may read or write):\n%s\n", in.Workspace)
	if strings.TrimSpace(in.Brief) != "" {
		fmt.Fprintf(&b, "\nWorkspace instructions:\n%s\n", strings.TrimSpace(in.Brief))
	}
	b.WriteString(`
Each turn, reply with one JSON object and no other text:
{"thought":"short reason","tool":"tool_name","args":{}}

Tools:
- list_dir: args {"path":"."} lists one directory level
- read_file: args {"path":"file"} reads a text file
- write_file: args {"path":"file","content":"text"} writes a file inside the workspace
- delete_file: args {"path":"file"} deletes one file inside the workspace
- fetch_url: args {"url":"https://example.com"} fetches a web page as text
`)
	if in.AllowShell {
		b.WriteString("- shell: args {\"command\":\"...\"} runs a command with the workspace as the working directory\n")
	} else {
		b.WriteString("Shell is disabled. Do not attempt shell commands.\n")
	}
	b.WriteString(`- finish: args {"summary":"what you did"} when the goal is complete

Rules:
- Use finish as soon as the goal is met.
- Do not invent file contents or web pages. Read them first.
- Keep paths inside the workspace.
- If a tool returns an error, change approach.
- Prefer a short written file in the workspace when the user would want to keep the result.
`)
	return b.String()
}

func readBrief(root string) string {
	for _, name := range []string{"BEAM.md", "AGENTS.md"} {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			continue
		}
		return clip(text, 6000)
	}
	return ""
}
