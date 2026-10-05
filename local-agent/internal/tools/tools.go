// Package tools runs the small set of actions a local agent is allowed to take.
package tools

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxRead  = 64 * 1024
	maxWrite = 256 * 1024
	maxFetch = 200 * 1024
	maxList  = 200
	maxOut   = 12 * 1024
)

// Request is the workspace boundary for one run.
type Request struct {
	Workspace  string
	AllowShell bool
}

// Execute runs one tool and returns an observation for the model.
func Execute(ctx context.Context, req Request, tool string, args map[string]any) string {
	if err := ctx.Err(); err != nil {
		return "error: stopped"
	}
	switch tool {
	case "list_dir":
		return listDir(req, arg(args, "path", "dir"))
	case "read_file":
		return readFile(req, arg(args, "path", "file"))
	case "write_file":
		return writeFile(req, arg(args, "path", "file"), arg(args, "content", "text"))
	case "delete_file":
		return deleteFile(req, arg(args, "path", "file"))
	case "fetch_url":
		return fetchURL(ctx, arg(args, "url", "href"))
	case "shell":
		if !req.AllowShell {
			return "error: shell is disabled for this run"
		}
		return runShell(ctx, req, arg(args, "command", "cmd"))
	default:
		return fmt.Sprintf("error: unknown tool %q", tool)
	}
}

func listDir(req Request, path string) string {
	if strings.TrimSpace(path) == "" {
		path = "."
	}
	full, err := safeJoin(req.Workspace, path)
	if err != nil {
		return "error: path is outside the workspace"
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return "error: " + err.Error()
	}
	if len(entries) == 0 {
		return "(empty directory)"
	}
	var b strings.Builder
	n := len(entries)
	if n > maxList {
		n = maxList
	}
	for i := 0; i < n; i++ {
		name := entries[i].Name()
		if entries[i].IsDir() {
			name += "/"
		}
		b.WriteString(name)
		b.WriteByte('\n')
	}
	if len(entries) > maxList {
		fmt.Fprintf(&b, "…%d more\n", len(entries)-maxList)
	}
	return strings.TrimRight(b.String(), "\n")
}

func readFile(req Request, path string) string {
	if strings.TrimSpace(path) == "" {
		return "error: path is required"
	}
	full, err := safeJoin(req.Workspace, path)
	if err != nil {
		return "error: path is outside the workspace"
	}
	st, err := os.Stat(full)
	if err != nil {
		return "error: " + err.Error()
	}
	if st.IsDir() {
		return "error: path is a directory"
	}
	f, err := os.Open(full)
	if err != nil {
		return "error: " + err.Error()
	}
	defer f.Close()
	buf := make([]byte, maxRead+1)
	n, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "error: " + err.Error()
	}
	data := buf[:n]
	if !utf8.Valid(data) {
		return "error: file is not UTF-8 text"
	}
	if n > maxRead {
		return fmt.Sprintf("truncated to %d bytes:\n%s", maxRead, string(data[:maxRead]))
	}
	return string(data)
}

func writeFile(req Request, path, content string) string {
	if strings.TrimSpace(path) == "" {
		return "error: path is required"
	}
	if len(content) > maxWrite {
		return "error: content is larger than 256KB"
	}
	full, err := safeJoin(req.Workspace, path)
	if err != nil {
		return "error: path is outside the workspace"
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "error: " + err.Error()
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "error: " + err.Error()
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(content), path)
}

func deleteFile(req Request, path string) string {
	if strings.TrimSpace(path) == "" {
		return "error: path is required"
	}
	full, err := safeJoin(req.Workspace, path)
	if err != nil {
		return "error: path is outside the workspace"
	}
	st, err := os.Lstat(full)
	if err != nil {
		return "error: " + err.Error()
	}
	if st.IsDir() {
		return "error: delete_file only removes files"
	}
	if err := os.Remove(full); err != nil {
		return "error: " + err.Error()
	}
	return "deleted " + path
}

func fetchURL(ctx context.Context, rawURL string) string {
	rawURL = strings.TrimSpace(rawURL)
	if !strings.HasPrefix(strings.ToLower(rawURL), "http://") && !strings.HasPrefix(strings.ToLower(rawURL), "https://") {
		return "error: url must start with http:// or https://"
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "error: " + err.Error()
	}
	req.Header.Set("User-Agent", "BeamAgent/0.1")
	req.Header.Set("Accept", "text/plain, text/html, application/json, text/markdown;q=0.9")
	client := &http.Client{CheckRedirect: limitedRedirect}
	res, err := client.Do(req)
	if err != nil {
		return "error: " + err.Error()
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxFetch+1))
	if err != nil {
		return "error: " + err.Error()
	}
	truncated := len(body) > maxFetch
	if truncated {
		body = body[:maxFetch]
	}
	ctype := res.Header.Get("Content-Type")
	text := string(body)
	if strings.Contains(strings.ToLower(ctype), "html") || looksLikeHTML(text) {
		text = htmlToText(text)
	} else if !utf8.Valid(body) || strings.Contains(text, "\x00") {
		return fmt.Sprintf("error: response is not text (HTTP %d, %s)", res.StatusCode, ctype)
	}
	text = clip(strings.TrimSpace(text), maxOut)
	if text == "" {
		text = "(empty response)"
	}
	note := ""
	if truncated {
		note = " truncated"
	}
	return fmt.Sprintf("HTTP %d%s\n%s", res.StatusCode, note, text)
}

func limitedRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 3 {
		return errors.New("too many redirects")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return errors.New("redirect left http")
	}
	return nil
}

func runShell(ctx context.Context, req Request, command string) string {
	command = strings.TrimSpace(command)
	if command == "" {
		return "error: command is required"
	}
	if why := blockedShell(command); why != "" {
		return "error: command blocked (" + why + ")"
	}
	fullRoot, err := safeJoin(req.Workspace, ".")
	if err != nil {
		return "error: workspace is unavailable"
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}
	cmd.Dir = fullRoot
	out, err := cmd.CombinedOutput()
	text := clip(string(out), maxOut)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "error: command timed out\n" + text
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "error: stopped\n" + text
	}
	exit := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			exit = ee.ExitCode()
		} else {
			return "error: " + err.Error()
		}
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Sprintf("exit %d", exit)
	}
	return fmt.Sprintf("exit %d\n%s", exit, text)
}

var blockedShellPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[;&|]|&&|\|\|)\s*rm\s+(?:-[^\s]+\s+)*(?:/\*?|~|\$home)(?:\s|$|[;&|])`),
	regexp.MustCompile(`(?i)(?:^|[;&|]|&&|\|\|)\s*mkfs(\.\w+)?\b`),
	regexp.MustCompile(`(?i)\bdiskpart\b`),
	regexp.MustCompile(`(?i)\bformat\s+[a-z]:`),
	regexp.MustCompile(`(?i)(?:^|[;&|]|&&|\|\|)\s*(shutdown|reboot|poweroff)\b`),
	regexp.MustCompile(`(?i):\(\)\s*\{`),
	regexp.MustCompile(`(?i)\bdd\s+if=`),
	regexp.MustCompile(`(?i)\bdel\s+/[a-z]*s`),
	regexp.MustCompile(`(?i)remove-item\b.*-recurse`),
}

func blockedShell(command string) string {
	reasons := []string{
		"recursive delete of a root or home path",
		"filesystem format",
		"disk partition editor",
		"drive format",
		"power control",
		"fork bomb",
		"raw disk copy",
		"recursive Windows delete",
		"recursive PowerShell delete",
	}
	for i, re := range blockedShellPatterns {
		if re.MatchString(command) {
			return reasons[i]
		}
	}
	return ""
}

func safeJoin(root, name string) (string, error) {
	if strings.Contains(name, "\x00") {
		return "", errOutside
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	rootAbs = filepath.Clean(rootAbs)
	if strings.TrimSpace(name) == "" {
		name = "."
	}
	var candidate string
	if filepath.IsAbs(name) {
		candidate = filepath.Clean(name)
	} else {
		candidate = filepath.Clean(filepath.Join(rootAbs, name))
	}
	if !inside(rootAbs, candidate) {
		return "", errOutside
	}
	realRoot, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return "", err
	}
	existing := candidate
	var suffix []string
	for {
		_, err := os.Lstat(existing)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		if existing == rootAbs || existing == string(filepath.Separator) {
			return "", errOutside
		}
		base := filepath.Base(existing)
		if base == ".." || base == "." || base == string(filepath.Separator) {
			return "", errOutside
		}
		suffix = append([]string{base}, suffix...)
		next := filepath.Dir(existing)
		if next == existing {
			return "", errOutside
		}
		existing = next
	}
	realExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	if !inside(realRoot, realExisting) {
		return "", errOutside
	}
	final := realExisting
	for _, part := range suffix {
		final = filepath.Join(final, part)
	}
	if !inside(realRoot, final) {
		return "", errOutside
	}
	return final, nil
}

func inside(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

var errOutside = errors.New("outside workspace")

var (
	reScript = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle  = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reTags   = regexp.MustCompile(`(?s)<[^>]+>`)
	reSpace  = regexp.MustCompile(`[ \t]{2,}`)
	reBreaks = regexp.MustCompile(`\n{3,}`)
)

func looksLikeHTML(s string) bool {
	low := strings.ToLower(s)
	return strings.Contains(low, "<html") || strings.Contains(low, "<!doctype html") || strings.Contains(low, "<body")
}

func htmlToText(s string) string {
	s = reScript.ReplaceAllString(s, " ")
	s = reStyle.ReplaceAllString(s, " ")
	s = reTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = strings.ReplaceAll(s, "\r", "")
	s = reSpace.ReplaceAllString(s, " ")
	s = reBreaks.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

func arg(args map[string]any, keys ...string) string {
	for _, key := range keys {
		v, ok := args[key]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			return t
		default:
			return fmt.Sprint(t)
		}
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
