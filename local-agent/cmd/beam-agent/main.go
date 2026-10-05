package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/agent"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/config"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/llm"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/server"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/transcript"
)

var version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		os.Exit(serveCmd(nil))
	}
	switch os.Args[1] {
	case "serve":
		os.Exit(serveCmd(os.Args[2:]))
	case "run":
		os.Exit(runCmd(os.Args[2:]))
	case "config":
		os.Exit(configCmd(os.Args[2:]))
	case "agents":
		os.Exit(agentsCmd(os.Args[2:]))
	case "version", "-v", "--version":
		fmt.Println(versionString())
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
}

func versionString() string { return "beam-agent " + version }

func usage(w io.Writer) {
	fmt.Fprintf(w, `%s

Beam Agent is a local autonomous agent. It does not need Docker or the AgentGPT web stack.
Double-click the program, or run it with no arguments, to open a page on this computer.

Usage:
  beam-agent                       Open the local page
  beam-agent serve [--port 8787]   Open the local page
  beam-agent run --goal "..."      Run one goal in the terminal
  beam-agent config show           Show settings
  beam-agent config preset ollama  Use a local Ollama server
  beam-agent agents list           List saved agents

The model server is any OpenAI-compatible API. A practice run does not call a model:
  beam-agent run --goal "Leave a note" --mock
`, versionString())
}

func serveCmd(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	port := fs.Int("port", 8787, "local port")
	open := fs.Bool("open", true, "open a browser")
	if err := fs.Parse(args); err != nil {
		usage(os.Stderr)
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if ws, err := config.EnsureWorkspace(cfg.Workspace); err == nil {
		cfg.Workspace = ws
	}
	ln, gotPort, err := server.Listen("127.0.0.1", *port)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	app := server.New(server.Options{Version: version})
	srv := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
	}()
	url := fmt.Sprintf("http://127.0.0.1:%d/", gotPort)
	fmt.Printf("%s\nLocal page: %s\nWorkspace: %s\nModel: %s\nPress Ctrl+C to stop.\n", versionString(), url, cfg.Workspace, cfg.Model)
	if *open {
		if err := openBrowser(url); err != nil {
			fmt.Fprintf(os.Stderr, "Open %s in a browser.\n", url)
		}
	}
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func runCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	goal := fs.String("goal", "", "goal for this run")
	name := fs.String("agent", "", "saved agent name")
	persona := fs.String("persona", "", "persona override for this run")
	workspace := fs.String("workspace", "", "workspace override for this run")
	maxSteps := fs.Int("max-steps", 0, "step limit for this run")
	allowShell := fs.Bool("allow-shell", false, "allow shell for this run")
	noShell := fs.Bool("no-shell", false, "disable shell for this run")
	mock := fs.Bool("mock", false, "practice run without a model")
	if err := fs.Parse(args); err != nil || strings.TrimSpace(*goal) == "" {
		fmt.Fprintln(os.Stderr, "Usage: beam-agent run --goal \"...\" [--agent Name] [--workspace DIR] [--mock]")
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	agentName := strings.TrimSpace(*name)
	if agentName == "" && len(cfg.Agents) > 0 {
		agentName = cfg.Agents[0].Name
	}
	profile, ok := config.FindAgent(cfg, agentName)
	if !ok {
		fmt.Fprintf(os.Stderr, "No agent named %q. Save one with: beam-agent agents add --name %s --persona \"...\"\n", agentName, agentName)
		return 1
	}
	if strings.TrimSpace(*persona) != "" {
		profile.Persona = *persona
	}
	ws := strings.TrimSpace(*workspace)
	if ws == "" {
		ws = cfg.Workspace
	}
	ws, err = config.EnsureWorkspace(ws)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	steps := cfg.MaxSteps
	if *maxSteps > 0 {
		steps = *maxSteps
	}
	allow := cfg.AllowShell
	if *allowShell {
		allow = true
	}
	if *noShell {
		allow = false
	}
	if !*mock && needsKey(cfg.BaseURL, cfg.APIKey) {
		fmt.Fprintln(os.Stderr, "Add an API key with `beam-agent config set api_key ...`, choose the ollama preset, or pass --mock.")
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var events []agent.Event
	emit := func(ev agent.Event) {
		events = append(events, ev)
		printEvent(ev)
	}
	var decider agent.Decider
	if *mock {
		decider = agent.MockDecider{}
	} else {
		decider = &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}
	}
	result, err := agent.Run(ctx, agent.Request{
		Name:       profile.Name,
		Persona:    profile.Persona,
		Goal:       *goal,
		Workspace:  ws,
		AllowShell: allow,
		MaxSteps:   steps,
		Decider:    decider,
	}, emit)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "Stopped.")
			return 1
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	events = append(events, agent.Event{Kind: "done", Summary: result.Summary, Step: result.Steps})
	if path, werr := transcript.Write(ws, profile.Name, *goal, events); werr == nil {
		fmt.Printf("Saved %s\n", path)
	}
	fmt.Println(result.Summary)
	if !result.Finished {
		return 2
	}
	return 0
}

func configCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: beam-agent config show|path|preset|set")
		return 2
	}
	switch args[0] {
	case "show":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("base_url:    %s\nmodel:       %s\napi_key:     %s (%s)\nworkspace:   %s\nmax_steps:   %d\nallow_shell: %v\n",
			cfg.BaseURL, cfg.Model, config.MaskKey(cfg.APIKey), cfg.KeySource, cfg.Workspace, cfg.MaxSteps, cfg.AllowShell)
		return 0
	case "path":
		p, err := config.Path()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println(p)
		return 0
	case "preset":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: beam-agent config preset openai|ollama")
			return 2
		}
		cur, err := config.LoadFile()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		next, err := config.ApplyPreset(cur, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := config.Save(next); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("Preset %s saved. Model %s at %s\n", strings.ToLower(args[1]), next.Model, next.BaseURL)
		return 0
	case "set":
		if len(args) != 3 {
			fmt.Fprintln(os.Stderr, "Usage: beam-agent config set base_url|model|api_key|workspace|max_steps|allow_shell VALUE")
			return 2
		}
		return setConfig(args[1], args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown config command %q\n", args[0])
		return 2
	}
}

func setConfig(key, value string) int {
	cur, err := config.LoadFile()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	in := config.Update{}
	switch key {
	case "base_url":
		in.BaseURL = &value
	case "model":
		in.Model = &value
	case "api_key":
		in.APIKey = &value
	case "workspace":
		in.Workspace = &value
	case "max_steps":
		n, err := strconv.Atoi(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "max_steps must be a number")
			return 1
		}
		in.MaxSteps = &n
	case "allow_shell":
		b, err := strconv.ParseBool(value)
		if err != nil {
			fmt.Fprintln(os.Stderr, "allow_shell must be true or false")
			return 1
		}
		in.AllowShell = &b
	default:
		fmt.Fprintf(os.Stderr, "unknown setting %q\n", key)
		return 2
	}
	next, err := config.ApplyUpdate(cur, in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if key == "workspace" {
		abs, err := config.EnsureWorkspace(next.Workspace)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		next.Workspace = abs
	}
	if err := config.Save(next); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if key == "api_key" {
		fmt.Println("API key saved.")
		return 0
	}
	fmt.Println("Saved.")
	return 0
}

func agentsCmd(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: beam-agent agents list|add|remove")
		return 2
	}
	switch args[0] {
	case "list":
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		for _, item := range cfg.Agents {
			fmt.Printf("%s\n  %s\n", item.Name, item.Persona)
		}
		return 0
	case "add":
		fs := flag.NewFlagSet("agents add", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		name := fs.String("name", "", "agent name")
		persona := fs.String("persona", "", "agent persona")
		if err := fs.Parse(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "Usage: beam-agent agents add --name NAME --persona TEXT")
			return 2
		}
		cur, err := config.LoadFile()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		next, err := config.UpsertAgent(cur, config.AgentProfile{Name: *name, Persona: *persona})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := config.Save(next); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("Saved agent %s\n", strings.TrimSpace(*name))
		return 0
	case "remove":
		if len(args) != 2 {
			fmt.Fprintln(os.Stderr, "Usage: beam-agent agents remove NAME")
			return 2
		}
		cur, err := config.LoadFile()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		next, err := config.RemoveAgent(cur, args[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := config.Save(next); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("Removed agent %s\n", args[1])
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown agents command %q\n", args[0])
		return 2
	}
}

func printEvent(ev agent.Event) {
	switch ev.Kind {
	case "action":
		fmt.Printf("\n== step %d: %s ==\n%s\n", ev.Step, ev.Tool, ev.Thought)
	case "observation":
		fmt.Printf("%s\n", ev.Observation)
	case "error":
		fmt.Fprintln(os.Stderr, ev.Error)
	}
}

func needsKey(baseURL, key string) bool {
	if strings.TrimSpace(key) != "" {
		return false
	}
	low := strings.ToLower(baseURL)
	return !strings.Contains(low, "://127.0.0.1") && !strings.Contains(low, "://localhost") && !strings.Contains(low, "://[::1]")
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
