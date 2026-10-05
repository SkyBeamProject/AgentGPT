// Package server hosts the local Beam Agent UI on 127.0.0.1.
package server

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/agent"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/config"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/llm"
	"github.com/SkyBeamProject/AgentGPT/local-agent/internal/transcript"
)

//go:embed web/index.html
var webFS embed.FS

// Runner executes one goal.
type Runner interface {
	Run(ctx context.Context, in RunInput, emit func(agent.Event)) (agent.Result, error)
}

// RunInput is everything required to start a run.
type RunInput struct {
	Name       string
	Persona    string
	Goal       string
	Workspace  string
	AllowShell bool
	MaxSteps   int
	Mock       bool
	Client     *llm.Client
}

// RealRunner calls the agent loop.
type RealRunner struct{}

// Run implements Runner.
func (RealRunner) Run(ctx context.Context, in RunInput, emit func(agent.Event)) (agent.Result, error) {
	var decider agent.Decider
	if in.Mock {
		decider = agent.MockDecider{}
	} else if in.Client != nil {
		decider = in.Client
	}
	return agent.Run(ctx, agent.Request{
		Name:       in.Name,
		Persona:    in.Persona,
		Goal:       in.Goal,
		Workspace:  in.Workspace,
		AllowShell: in.AllowShell,
		MaxSteps:   in.MaxSteps,
		Decider:    decider,
	}, emit)
}

// Options configures the local server.
type Options struct {
	Token   string
	Version string
	Runner  Runner
}

// Server is the loopback HTTP API and page.
type Server struct {
	token   string
	version string
	runner  Runner
	hub     *hub
	page    string
}

// New builds a server. A token is generated when opts.Token is empty.
func New(opts Options) *Server {
	if opts.Token == "" {
		opts.Token = newToken()
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	if opts.Runner == nil {
		opts.Runner = RealRunner{}
	}
	page, err := webFS.ReadFile("web/index.html")
	if err != nil {
		panic(err)
	}
	html := strings.ReplaceAll(string(page), "__BEAM_TOKEN__", opts.Token)
	html = strings.ReplaceAll(html, "__BEAM_VERSION__", opts.Version)
	return &Server{
		token:   opts.Token,
		version: opts.Version,
		runner:  opts.Runner,
		hub:     newHub(),
		page:    html,
	}
}

// Handler returns the local-only HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.servePage)
	mux.HandleFunc("GET /api/config", s.authed(s.getConfig))
	mux.HandleFunc("PUT /api/config", s.authed(s.putConfig))
	mux.HandleFunc("POST /api/config/preset", s.authed(s.postPreset))
	mux.HandleFunc("POST /api/test", s.authed(s.postTest))
	mux.HandleFunc("POST /api/agents", s.authed(s.postAgent))
	mux.HandleFunc("DELETE /api/agents", s.authed(s.deleteAgent))
	mux.HandleFunc("POST /api/runs", s.authed(s.postRun))
	mux.HandleFunc("GET /api/runs/{id}", s.authed(s.getRun))
	mux.HandleFunc("POST /api/runs/{id}/stop", s.authed(s.stopRun))
	return localOnly(securityHeaders(mux))
}

// Token is the per-process secret embedded in the local page.
func (s *Server) Token() string { return s.token }

func (s *Server) servePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, s.page)
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(cfg, s.version))
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var in config.Update
	if err := decode(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, err := config.LoadFile()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := config.ApplyUpdate(cur, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	abs, err := config.EnsureWorkspace(next.Workspace)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	next.Workspace = abs
	if err := config.Save(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	effective, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(effective, s.version))
}

func (s *Server) postPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, err := config.LoadFile()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := config.ApplyPreset(cur, body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.Save(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	effective, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(effective, s.version))
}

func (s *Server) postTest(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if needsKey(cfg.BaseURL, cfg.APIKey) {
		writeErr(w, http.StatusBadRequest, "Add an API key, or point the base URL at a local server such as Ollama.")
		return
	}
	client := &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model}
	models, err := client.ListModels(r.Context())
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "models": models})
}

func (s *Server) postAgent(w http.ResponseWriter, r *http.Request) {
	var agentProfile config.AgentProfile
	if err := decode(w, r, &agentProfile); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, err := config.LoadFile()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := config.UpsertAgent(cur, agentProfile)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.Save(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	effective, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(effective, s.version))
}

func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decode(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cur, err := config.LoadFile()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	next, err := config.RemoveAgent(cur, body.Name)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := config.Save(next); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	effective, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, config.PublicView(effective, s.version))
}

func (s *Server) postRun(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Agent string `json:"agent"`
		Goal  string `json:"goal"`
		Mock  bool   `json:"mock"`
	}
	if err := decode(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		writeErr(w, http.StatusBadRequest, "Write a goal first.")
		return
	}
	if len([]rune(goal)) > 8000 {
		writeErr(w, http.StatusBadRequest, "That goal is too long.")
		return
	}
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	profile, ok := config.FindAgent(cfg, body.Agent)
	if !ok {
		writeErr(w, http.StatusBadRequest, "Choose an agent first.")
		return
	}
	workspace, err := config.EnsureWorkspace(cfg.Workspace)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !body.Mock && needsKey(cfg.BaseURL, cfg.APIKey) {
		writeErr(w, http.StatusBadRequest, "Add an API key in Model and workspace, or use a practice run.")
		return
	}
	in := RunInput{
		Name:       profile.Name,
		Persona:    profile.Persona,
		Goal:       goal,
		Workspace:  workspace,
		AllowShell: cfg.AllowShell,
		MaxSteps:   cfg.MaxSteps,
		Mock:       body.Mock,
		Client:     &llm.Client{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model},
	}
	rs, err := s.hub.start()
	if err != nil {
		writeErr(w, http.StatusConflict, "A run is already in progress.")
		return
	}
	log.Printf("run %s started agent=%s mock=%v", rs.id, profile.Name, body.Mock)
	go s.execute(rs, in)
	writeJSON(w, http.StatusOK, map[string]string{"id": rs.id})
}

func (s *Server) execute(rs *runState, in RunInput) {
	defer rs.finish()
	var events []agent.Event
	emit := func(ev agent.Event) {
		events = append(events, ev)
		rs.add(ev)
	}
	result, err := s.runner.Run(rs.ctx, in, emit)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, context.Canceled) {
			msg = "Stopped."
		}
		ev := agent.Event{Kind: "error", Error: msg}
		events = append(events, ev)
		rs.add(ev)
		s.saveTranscript(rs, in, events)
		log.Printf("run %s error", rs.id)
		return
	}
	events = append(events, agent.Event{Kind: "done", Summary: result.Summary, Step: result.Steps})
	s.saveTranscript(rs, in, events)
	rs.add(agent.Event{Kind: "done", Summary: result.Summary, Step: result.Steps})
	log.Printf("run %s finished steps=%d", rs.id, result.Steps)
}

func (s *Server) saveTranscript(rs *runState, in RunInput, events []agent.Event) {
	path, err := transcript.Write(in.Workspace, in.Name, in.Goal, events)
	if err != nil {
		rs.add(agent.Event{Kind: "status", Observation: "Could not save a transcript: " + err.Error()})
		return
	}
	rs.add(agent.Event{Kind: "status", Observation: "Saved " + path})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	rs, ok := s.hub.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "That run is no longer available.")
		return
	}
	cursor, _ := strconv.Atoi(r.URL.Query().Get("cursor"))
	events, done, next := rs.slice(cursor)
	writeJSON(w, http.StatusOK, map[string]any{
		"events": events,
		"cursor": next,
		"done":   done,
	})
}

func (s *Server) stopRun(w http.ResponseWriter, r *http.Request) {
	if !s.hub.stop(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "That run is no longer available.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) authed(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !tokenOK(r.Header.Get("X-Beam-Token"), s.token) {
			writeErr(w, http.StatusUnauthorized, "This local page needs a fresh token. Reload it.")
			return
		}
		next(w, r)
	}
}

func needsKey(baseURL, key string) bool {
	if strings.TrimSpace(key) != "" {
		return false
	}
	low := strings.ToLower(baseURL)
	return !strings.Contains(low, "://127.0.0.1") && !strings.Contains(low, "://localhost") && !strings.Contains(low, "://[::1]")
}

func decode(w http.ResponseWriter, r *http.Request, dest any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil && !errors.Is(err, io.EOF) {
		return errors.New("the request body was not valid JSON")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src data:; connect-src 'self'; base-uri 'none'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "local only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func tokenOK(got, want string) bool {
	if len(got) != len(want) || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func newToken() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buf)
}

type hub struct {
	mu     sync.Mutex
	active string
	order  []string
	runs   map[string]*runState
}

func newHub() *hub {
	return &hub{runs: map[string]*runState{}}
}

func (h *hub) start() (*runState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.active != "" {
		if rs := h.runs[h.active]; rs != nil && !rs.isDone() {
			return nil, errors.New("busy")
		}
	}
	rs := newRun()
	h.runs[rs.id] = rs
	h.order = append(h.order, rs.id)
	h.active = rs.id
	if len(h.order) > 10 {
		old := h.order[0]
		h.order = h.order[1:]
		if old != rs.id {
			delete(h.runs, old)
		}
	}
	return rs, nil
}

func (h *hub) get(id string) (*runState, bool) {
	if !validID(id) {
		return nil, false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	rs, ok := h.runs[id]
	return rs, ok
}

func (h *hub) stop(id string) bool {
	rs, ok := h.get(id)
	if !ok {
		return false
	}
	rs.cancel()
	return true
}

func validID(id string) bool {
	if len(id) != 16 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

type runState struct {
	id     string
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	events []agent.Event
	done   bool
}

func newRun() *runState {
	ctx, cancel := context.WithCancel(context.Background())
	return &runState{id: newToken()[:16], ctx: ctx, cancel: cancel}
}

func (r *runState) add(ev agent.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *runState) finish() {
	r.mu.Lock()
	r.done = true
	r.mu.Unlock()
	r.cancel()
}

func (r *runState) isDone() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.done
}

func (r *runState) slice(from int) ([]agent.Event, bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if from < 0 {
		from = 0
	}
	if from > len(r.events) {
		from = len(r.events)
	}
	out := append([]agent.Event(nil), r.events[from:]...)
	return out, r.done, len(r.events)
}

// Listen finds an open port on loopback, starting at start.
func Listen(host string, start int) (net.Listener, int, error) {
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return nil, 0, fmt.Errorf("refusing to listen on %s", host)
	}
	if start < 1 || start > 65535 {
		start = 8787
	}
	var last error
	for port := start; port < start+10 && port <= 65535; port++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return ln, port, nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("no free local port")
	}
	return nil, 0, last
}
