// Package llm talks to any server that implements the OpenAI chat completions API.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client is an OpenAI-compatible chat client.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	HTTP    *http.Client
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Chat sends a single completion request.
func (c *Client) Chat(ctx context.Context, messages []Message) (string, error) {
	if strings.TrimSpace(c.Model) == "" {
		return "", errors.New("model is required")
	}
	url, err := CompletionsURL(c.BaseURL)
	if err != nil {
		return "", err
	}
	payload := map[string]any{
		"model":       c.Model,
		"temperature": 0.2,
		"messages":    messages,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	}
	res, err := c.http().Do(req)
	if err != nil {
		return "", friendlyTransport(c.BaseURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 2<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("%s", friendlyStatus(res.StatusCode, string(body)))
	}
	var parsed chatResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("model server returned an unreadable response")
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", errors.New(clip(parsed.Error.Message, 500))
	}
	if len(parsed.Choices) == 0 || strings.TrimSpace(parsed.Choices[0].Message.Content) == "" {
		return "", errors.New("model server returned an empty reply")
	}
	return parsed.Choices[0].Message.Content, nil
}

// Decide matches the agent decider interface.
func (c *Client) Decide(ctx context.Context, system, user string) (string, error) {
	return c.Chat(ctx, []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	})
}

// ListModels calls the OpenAI-compatible models endpoint.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	url, err := ModelsURL(c.BaseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if strings.TrimSpace(c.APIKey) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.APIKey))
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, friendlyTransport(c.BaseURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", friendlyStatus(res.StatusCode, string(body)))
	}
	var parsed modelsResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, errors.New("model server returned an unreadable model list")
	}
	out := make([]string, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if strings.TrimSpace(item.ID) != "" {
			out = append(out, item.ID)
		}
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

// CompletionsURL builds the chat completions endpoint from a base URL.
func CompletionsURL(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if err := checkBase(base); err != nil {
		return "", err
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base, nil
	}
	return base + "/chat/completions", nil
}

// ModelsURL builds the models endpoint from a base URL.
func ModelsURL(base string) (string, error) {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if err := checkBase(base); err != nil {
		return "", err
	}
	if strings.HasSuffix(base, "/chat/completions") {
		base = strings.TrimSuffix(base, "/chat/completions")
	}
	if strings.HasSuffix(base, "/models") {
		return base, nil
	}
	return base + "/models", nil
}

func checkBase(base string) error {
	low := strings.ToLower(base)
	if base == "" || (!strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://")) {
		return errors.New("base URL must start with http:// or https://")
	}
	return nil
}

func friendlyStatus(status int, body string) string {
	body = clip(strings.TrimSpace(body), 300)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "the model server rejected the API key"
	case http.StatusNotFound:
		return "the model server has no endpoint at that URL"
	default:
		if body == "" {
			return fmt.Sprintf("model server returned HTTP %d", status)
		}
		return fmt.Sprintf("model server returned HTTP %d", status)
	}
}

func friendlyTransport(base string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) || strings.Contains(strings.ToLower(err.Error()), "connection refused") {
		return fmt.Errorf("could not reach the model server at %s", strings.TrimSpace(base))
	}
	return err
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
