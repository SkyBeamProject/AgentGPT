// Package config stores Beam Agent settings on the local machine.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-4o-mini"
	DefaultSteps   = 12
	MaxStepsLimit  = 40
	DefaultPersona = "You are a practical local assistant. Work step by step toward the goal, use tools to check facts, and write anything the user should keep into the workspace."
)

// AgentProfile is a named persona the user can run against a goal.
type AgentProfile struct {
	Name    string `json:"name"`
	Persona string `json:"persona"`
}

// Config is the on-disk configuration. KeySource is filled at load time and is not saved.
type Config struct {
	BaseURL    string         `json:"base_url"`
	APIKey     string         `json:"api_key,omitempty"`
	Model      string         `json:"model"`
	Workspace  string         `json:"workspace"`
	MaxSteps   int            `json:"max_steps"`
	AllowShell bool           `json:"allow_shell"`
	Agents     []AgentProfile `json:"agents"`
	KeySource  string         `json:"-"`
}

// Update is a partial settings change. Nil fields are left as they are.
// An empty APIKey does not clear a saved key; set ClearAPIKey to remove it.
type Update struct {
	BaseURL     *string `json:"base_url"`
	Model       *string `json:"model"`
	APIKey      *string `json:"api_key"`
	ClearAPIKey bool    `json:"clear_api_key"`
	Workspace   *string `json:"workspace"`
	MaxSteps    *int    `json:"max_steps"`
	AllowShell  *bool   `json:"allow_shell"`
}

var agentNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,39}$`)

// Path is the config file for this user account.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "beam-agent", "config.json"), nil
}

// Default returns a usable config before the user has saved anything.
func Default() Config {
	return Config{
		BaseURL:   DefaultBaseURL,
		Model:     DefaultModel,
		Workspace: DefaultWorkspace(),
		MaxSteps:  DefaultSteps,
		Agents: []AgentProfile{{
			Name:    "General",
			Persona: DefaultPersona,
		}},
	}.Normalize()
}

// DefaultWorkspace is a dedicated folder, never the user's whole home directory.
func DefaultWorkspace() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "Beam Agent")
	}
	docs := filepath.Join(home, "Documents")
	if st, err := os.Stat(docs); err == nil && st.IsDir() {
		return filepath.Join(docs, "Beam Agent")
	}
	return filepath.Join(home, "Beam Agent")
}

// LoadFile reads the config file without environment overrides.
func LoadFile() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, fmt.Errorf("config file %s: %w", path, err)
	}
	return cfg.Normalize(), nil
}

// Load returns the saved config with empty fields filled from the environment.
// BEAM_API_KEY and OPENAI_API_KEY are used only when no key is saved.
// BEAM_BASE_URL, OPENAI_BASE_URL, and BEAM_MODEL fill empty values only.
func Load() (Config, error) {
	cfg, err := LoadFile()
	if err != nil {
		return Config{}, err
	}
	return cfg.withEnv(), nil
}

func (c Config) withEnv() Config {
	c.APIKey = strings.TrimSpace(c.APIKey)
	if c.APIKey == "" {
		if v := strings.TrimSpace(os.Getenv("BEAM_API_KEY")); v != "" {
			c.APIKey = v
			c.KeySource = "env"
		} else if v := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); v != "" {
			c.APIKey = v
			c.KeySource = "env"
		} else {
			c.KeySource = "none"
		}
	} else {
		c.KeySource = "file"
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		c.BaseURL = firstNonEmpty(os.Getenv("BEAM_BASE_URL"), os.Getenv("OPENAI_BASE_URL"), DefaultBaseURL)
	}
	if strings.TrimSpace(c.Model) == "" {
		c.Model = firstNonEmpty(os.Getenv("BEAM_MODEL"), DefaultModel)
	}
	return c.Normalize()
}

// Save writes the config. The API key is stored in a file readable only by this user.
func Save(cfg Config) error {
	cfg = cfg.Normalize()
	if err := Validate(cfg); err != nil {
		return err
	}
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600)
}

// ApplyUpdate merges a partial change into the file config.
func ApplyUpdate(cur Config, in Update) (Config, error) {
	if in.BaseURL != nil {
		cur.BaseURL = strings.TrimSpace(*in.BaseURL)
	}
	if in.Model != nil {
		cur.Model = strings.TrimSpace(*in.Model)
	}
	if in.Workspace != nil {
		cur.Workspace = strings.TrimSpace(*in.Workspace)
	}
	if in.MaxSteps != nil {
		cur.MaxSteps = *in.MaxSteps
	}
	if in.AllowShell != nil {
		cur.AllowShell = *in.AllowShell
	}
	if in.ClearAPIKey {
		cur.APIKey = ""
	} else if in.APIKey != nil && strings.TrimSpace(*in.APIKey) != "" {
		cur.APIKey = strings.TrimSpace(*in.APIKey)
	}
	cur = cur.Normalize()
	if err := Validate(cur); err != nil {
		return Config{}, err
	}
	return cur, nil
}

// ApplyPreset sets the endpoint and model for a known local or hosted server.
func ApplyPreset(cur Config, name string) (Config, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "openai":
		cur.BaseURL = DefaultBaseURL
		cur.Model = DefaultModel
	case "ollama":
		cur.BaseURL = "http://127.0.0.1:11434/v1"
		cur.Model = "llama3.2"
	default:
		return Config{}, errors.New("unknown preset; use openai or ollama")
	}
	cur = cur.Normalize()
	if err := Validate(cur); err != nil {
		return Config{}, err
	}
	return cur, nil
}

// UpsertAgent adds or replaces a persona.
func UpsertAgent(cur Config, agent AgentProfile) (Config, error) {
	agent.Name = strings.TrimSpace(agent.Name)
	agent.Persona = strings.TrimSpace(agent.Persona)
	if err := validateAgent(agent); err != nil {
		return Config{}, err
	}
	replaced := false
	for i := range cur.Agents {
		if cur.Agents[i].Name == agent.Name {
			cur.Agents[i] = agent
			replaced = true
			break
		}
	}
	if !replaced {
		if len(cur.Agents) >= 30 {
			return Config{}, errors.New("you can save up to 30 agents")
		}
		cur.Agents = append(cur.Agents, agent)
	}
	cur = cur.Normalize()
	return cur, Validate(cur)
}

// RemoveAgent deletes a persona. The last remaining agent is kept.
func RemoveAgent(cur Config, name string) (Config, error) {
	name = strings.TrimSpace(name)
	if len(cur.Agents) <= 1 {
		return Config{}, errors.New("Keep at least one agent.")
	}
	next := cur.Agents[:0]
	found := false
	for _, agent := range cur.Agents {
		if agent.Name == name {
			found = true
			continue
		}
		next = append(next, agent)
	}
	if !found {
		return Config{}, fmt.Errorf("no agent named %q", name)
	}
	cur.Agents = next
	cur = cur.Normalize()
	return cur, Validate(cur)
}

// FindAgent returns a saved persona by name.
func FindAgent(cfg Config, name string) (AgentProfile, bool) {
	name = strings.TrimSpace(name)
	for _, agent := range cfg.Agents {
		if agent.Name == name {
			return agent, true
		}
	}
	return AgentProfile{}, false
}

// Normalize fills defaults and trims fields. It does not check that the workspace exists.
func (c Config) Normalize() Config {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = DefaultBaseURL
	}
	c.Model = strings.TrimSpace(c.Model)
	if c.Model == "" {
		c.Model = DefaultModel
	}
	c.APIKey = strings.TrimSpace(c.APIKey)
	c.Workspace = strings.TrimSpace(c.Workspace)
	if c.Workspace == "" {
		c.Workspace = DefaultWorkspace()
	}
	if c.MaxSteps < 1 {
		c.MaxSteps = DefaultSteps
	}
	if c.MaxSteps > MaxStepsLimit {
		c.MaxSteps = MaxStepsLimit
	}
	if len(c.Agents) == 0 {
		c.Agents = []AgentProfile{{Name: "General", Persona: DefaultPersona}}
	}
	for i := range c.Agents {
		c.Agents[i].Name = strings.TrimSpace(c.Agents[i].Name)
		c.Agents[i].Persona = strings.TrimSpace(c.Agents[i].Persona)
	}
	return c
}

// Validate checks a normalized config.
func Validate(c Config) error {
	if err := validateBaseURL(c.BaseURL); err != nil {
		return err
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("model is required")
	}
	if c.MaxSteps < 1 || c.MaxSteps > MaxStepsLimit {
		return fmt.Errorf("max steps must be between 1 and %d", MaxStepsLimit)
	}
	if strings.TrimSpace(c.Workspace) == "" {
		return errors.New("workspace is required")
	}
	if len(c.Agents) == 0 {
		return errors.New("save at least one agent")
	}
	seen := map[string]bool{}
	for _, agent := range c.Agents {
		if err := validateAgent(agent); err != nil {
			return err
		}
		if seen[agent.Name] {
			return fmt.Errorf("duplicate agent %q", agent.Name)
		}
		seen[agent.Name] = true
	}
	return nil
}

func validateAgent(agent AgentProfile) error {
	if !agentNamePattern.MatchString(agent.Name) {
		return errors.New("agent name must start with a letter or number and stay under 40 characters")
	}
	if strings.TrimSpace(agent.Persona) == "" {
		return errors.New("agent persona is required")
	}
	if len([]rune(agent.Persona)) > 4000 {
		return errors.New("agent persona is too long")
	}
	return nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return errors.New("base URL is required")
	}
	low := strings.ToLower(raw)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		return errors.New("base URL must start with http:// or https://")
	}
	if strings.ContainsAny(raw, " \t\r\n") {
		return errors.New("base URL contains spaces")
	}
	return nil
}

// Public is the config shape returned to the local UI. The API key itself is omitted.
type Public struct {
	BaseURL    string         `json:"base_url"`
	Model      string         `json:"model"`
	Workspace  string         `json:"workspace"`
	MaxSteps   int            `json:"max_steps"`
	AllowShell bool           `json:"allow_shell"`
	HasAPIKey  bool           `json:"has_api_key"`
	KeySource  string         `json:"key_source"`
	Agents     []AgentProfile `json:"agents"`
	Version    string         `json:"version,omitempty"`
}

// PublicView returns a copy safe to show in the browser.
func PublicView(c Config, version string) Public {
	agents := append([]AgentProfile(nil), c.Agents...)
	return Public{
		BaseURL:    c.BaseURL,
		Model:      c.Model,
		Workspace:  c.Workspace,
		MaxSteps:   c.MaxSteps,
		AllowShell: c.AllowShell,
		HasAPIKey:  strings.TrimSpace(c.APIKey) != "",
		KeySource:  c.KeySource,
		Agents:     agents,
		Version:    version,
	}
}

// MaskKey returns a short label for terminal display.
func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "not set"
	}
	if len(key) <= 8 {
		return "set"
	}
	return "…" + key[len(key)-4:]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// EnsureWorkspace creates the workspace folder when it is a safe location.
func EnsureWorkspace(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		path = DefaultWorkspace()
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if err := rejectWorkspace(abs, runtime.GOOS); err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", errors.New("workspace is not a directory")
	}
	return abs, nil
}

func rejectWorkspace(clean, goos string) error {
	if clean == "" || clean == "." {
		return errors.New("workspace path is empty")
	}
	if goos == "windows" {
		if isDriveRoot(clean) {
			return errors.New("choose a folder, not a drive root")
		}
		low := strings.ToLower(clean)
		for _, prefix := range []string{`c:\windows`, `c:\program files`, `c:\program files (x86)`} {
			if low == prefix || strings.HasPrefix(low, prefix+`\`) {
				return errors.New("workspace is inside a system directory")
			}
		}
		return nil
	}
	if clean == string(os.PathSeparator) {
		return errors.New("choose a folder, not the filesystem root")
	}
	for _, prefix := range []string{"/usr", "/etc", "/bin", "/sbin", "/lib", "/lib64", "/boot", "/proc", "/sys", "/dev"} {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return errors.New("workspace is inside a system directory")
		}
	}
	return nil
}

func isDriveRoot(p string) bool {
	if len(p) != 3 || p[1] != ':' {
		return false
	}
	return p[2] == '\\' || p[2] == '/'
}
