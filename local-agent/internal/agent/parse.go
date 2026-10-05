package agent

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Decision is one tool call from the model.
type Decision struct {
	Thought string         `json:"thought"`
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
}

var (
	reFence = regexp.MustCompile("(?is)^```(?:json)?\\s*|\\s*```$")
	reThink = regexp.MustCompile(`(?is)<think>.*?</think>`)
)

var toolAliases = map[string]string{
	"finish_goal": "finish",
	"done":        "finish",
	"complete":    "finish",
	"list":        "list_dir",
	"ls":          "list_dir",
	"read":        "read_file",
	"write":       "write_file",
	"delete":      "delete_file",
	"browse":      "fetch_url",
	"web":         "fetch_url",
	"http":        "fetch_url",
	"bash":        "shell",
	"cmd":         "shell",
	"command":     "shell",
}

// ParseDecision extracts one tool call from a model reply.
func ParseDecision(raw string) (Decision, error) {
	s := strings.TrimSpace(raw)
	s = reThink.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = reFence.ReplaceAllString(s, "")
		s = strings.TrimSpace(s)
	}
	i := strings.Index(s, "{")
	if i < 0 {
		return Decision{}, errors.New("model reply did not contain a JSON object")
	}
	dec := json.NewDecoder(strings.NewReader(s[i:]))
	var d Decision
	if err := dec.Decode(&d); err != nil {
		return Decision{}, err
	}
	d.Tool = strings.ToLower(strings.TrimSpace(d.Tool))
	if alias, ok := toolAliases[d.Tool]; ok {
		d.Tool = alias
	}
	if d.Tool == "" {
		return Decision{}, errors.New("model reply is missing a tool")
	}
	if d.Args == nil {
		d.Args = map[string]any{}
	}
	d.Thought = strings.TrimSpace(d.Thought)
	return d, nil
}
