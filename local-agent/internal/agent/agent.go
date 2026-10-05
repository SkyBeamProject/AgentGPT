// Package agent runs a goal by asking a model for one tool call at a time.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Decider returns the next model reply. The llm client implements this.
type Decider interface {
	Decide(ctx context.Context, system, user string) (string, error)
}

// Request is one local run.
type Request struct {
	Name       string
	Persona    string
	Goal       string
	Workspace  string
	AllowShell bool
	MaxSteps   int
	Decider    Decider
}

// Result is how a run ended.
type Result struct {
	Summary  string
	Steps    int
	Finished bool
}

// Event is one item in the transcript.
type Event struct {
	Kind        string         `json:"kind"`
	Step        int            `json:"step,omitempty"`
	Thought     string         `json:"thought,omitempty"`
	Tool        string         `json:"tool,omitempty"`
	Args        map[string]any `json:"args,omitempty"`
	Observation string         `json:"observation,omitempty"`
	Summary     string         `json:"summary,omitempty"`
	Error       string         `json:"error,omitempty"`
}

type record struct {
	Thought     string
	Tool        string
	Args        map[string]any
	Observation string
}

// Run executes the goal until the model finishes, the step limit is hit, or ctx is cancelled.
func Run(ctx context.Context, req Request, emit func(Event)) (Result, error) {
	if emit == nil {
		emit = func(Event) {}
	}
	goal := strings.TrimSpace(req.Goal)
	if goal == "" {
		return Result{}, errors.New("goal is empty")
	}
	if len([]rune(goal)) > 8000 {
		return Result{}, errors.New("goal is too long")
	}
	if req.Decider == nil {
		return Result{}, errors.New("model client is not configured")
	}
	maxSteps := req.MaxSteps
	if maxSteps < 1 {
		maxSteps = 12
	}
	if maxSteps > 40 {
		maxSteps = 40
	}
	system := SystemPrompt(PromptInput{
		Name:       req.Name,
		Persona:    req.Persona,
		Workspace:  req.Workspace,
		Brief:      readBrief(req.Workspace),
		AllowShell: req.AllowShell,
	})
	var history []record
	bad := 0
	for step := 1; step <= maxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		raw, err := req.Decider.Decide(ctx, system, historyText(goal, history))
		if err != nil {
			return Result{}, err
		}
		decision, err := ParseDecision(raw)
		if err != nil {
			bad++
			obs := "error: the model reply was not a JSON tool call. Reply with one JSON object only."
			history = append(history, record{Thought: clip(raw, 500), Tool: "invalid", Observation: obs})
			emit(Event{Kind: "action", Step: step, Thought: "The model reply could not be used.", Tool: "invalid"})
			emit(Event{Kind: "observation", Step: step, Observation: obs})
			if bad >= 3 {
				return Result{Summary: "Stopped after repeated invalid model replies.", Steps: step}, nil
			}
			continue
		}
		bad = 0
		emit(Event{
			Kind:    "action",
			Step:    step,
			Thought: clip(decision.Thought, 1000),
			Tool:    decision.Tool,
			Args:    decision.Args,
		})
		if decision.Tool == "finish" {
			summary := strings.TrimSpace(argString(decision.Args, "summary", "result", "message"))
			if summary == "" {
				summary = "Finished."
			}
			return Result{Summary: summary, Steps: step, Finished: true}, nil
		}
		obs := execute(ctx, req, decision.Tool, decision.Args)
		history = append(history, record{
			Thought:     decision.Thought,
			Tool:        decision.Tool,
			Args:        decision.Args,
			Observation: obs,
		})
		emit(Event{Kind: "observation", Step: step, Observation: obs})
	}
	return Result{Summary: fmt.Sprintf("Stopped after %d steps without finishing.", maxSteps), Steps: maxSteps}, nil
}

func execute(ctx context.Context, req Request, tool string, args map[string]any) string {
	// Imported in tools.go via a wrapper in run.go to keep this file free of a direct cycle.
	return toolExec(ctx, req.Workspace, req.AllowShell, tool, args)
}

func historyText(goal string, steps []record) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Goal:\n%s\n", goal)
	if len(steps) == 0 {
		b.WriteString("\nNo steps yet. Choose the first tool.\n")
		return b.String()
	}
	b.WriteString("\nSteps so far:\n")
	for i, step := range steps {
		fmt.Fprintf(&b, "\n%d. thought: %s\ntool: %s\nargs: %s\nobservation: %s\n",
			i+1, clip(step.Thought, 500), step.Tool, compact(step.Args), clip(step.Observation, 4000))
	}
	b.WriteString("\nChoose the next tool. Use finish when the goal is complete.\n")
	return b.String()
}

func compact(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return clip(string(b), 2000)
}

func argString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		if s, ok := v.(string); ok {
			return s
		}
		return fmt.Sprint(v)
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
