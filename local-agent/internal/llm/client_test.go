package llm

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompletionsAndModelsURL(t *testing.T) {
	got, err := CompletionsURL("http://127.0.0.1:11434/v1")
	if err != nil || got != "http://127.0.0.1:11434/v1/chat/completions" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = CompletionsURL("https://example.com/v1/chat/completions")
	if err != nil || got != "https://example.com/v1/chat/completions" {
		t.Fatalf("%s %v", got, err)
	}
	got, err = ModelsURL("https://example.com/v1/chat/completions")
	if err != nil || got != "https://example.com/v1/models" {
		t.Fatalf("%s %v", got, err)
	}
	if _, err := CompletionsURL("ftp://example.com"); err == nil {
		t.Fatal("expected scheme rejection")
	}
}

func TestChatSuccessAndUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":{"message":"nope"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"tool\":\"finish\"}"}}]}`)
	}))
	defer srv.Close()

	client := &Client{BaseURL: srv.URL + "/v1", APIKey: "secret", Model: "m", HTTP: srv.Client()}
	out, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "finish") {
		t.Fatal(out)
	}

	client.APIKey = "wrong"
	if _, err := client.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatal(err)
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"llama3.2"},{"id":"other"}]}`)
	}))
	defer srv.Close()
	client := &Client{BaseURL: srv.URL + "/v1", Model: "m", HTTP: srv.Client()}
	got, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "llama3.2" {
		t.Fatalf("%v", got)
	}
}
