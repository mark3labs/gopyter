//go:build !noai

package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCatalog(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "sk-test")

	start := time.Now()
	c := Catalog()
	t.Logf("%d models in %v", len(c), time.Since(start))
	if len(c) < 100 {
		t.Fatalf("only %d models", len(c))
	}
	find := func(id string) *ModelEntry {
		i := slices.IndexFunc(c, func(e ModelEntry) bool { return e.ID() == id })
		if i < 0 {
			return nil
		}
		return &c[i]
	}
	if e := find("anthropic/claude-sonnet-5"); e == nil || !e.Ready || !e.Recommended {
		t.Errorf("anthropic model with a key: %+v", e)
	}
	if c[0].ID() != "anthropic/claude-sonnet-5" {
		t.Errorf("the recommended model of the only provider with a key isn't first: %s", c[0].ID())
	}
	if e := find("xai/grok-4.6"); e == nil || e.Ready || !strings.Contains(e.Missing, "XAI_API_KEY") {
		t.Errorf("xai model without a key: %+v", e)
	}
	// Ready models come first.
	if i := slices.IndexFunc(c, func(e ModelEntry) bool { return !e.Ready }); i >= 0 &&
		slices.ContainsFunc(c[i:], func(e ModelEntry) bool { return e.Ready }) {
		t.Error("a ready model is sorted after an unready one")
	}
	for _, e := range c {
		if e.Provider == OllamaProvider || e.Provider == "custom" {
			t.Fatalf("catalog lists %s", e.ID())
		}
	}
}

func TestOllamaModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"models":[{"name":"qwen3:1.7b"},{"name":"gpt-oss:20b"}]}`))
	}))
	defer srv.Close()

	t.Setenv("OLLAMA_HOST", srv.URL)
	got, err := OllamaModels(context.Background())
	if err != nil || !slices.Equal(got, []string{"gpt-oss:20b", "qwen3:1.7b"}) {
		t.Fatalf("got %v, %v", got, err)
	}

	srv.Close()
	if _, err := OllamaModels(context.Background()); err == nil || !strings.Contains(err.Error(), "is it running") {
		t.Errorf("stopped server: %v", err)
	}
}

func TestOllamaURL(t *testing.T) {
	for host, want := range map[string]string{
		"":                      "http://localhost:11434",
		"0.0.0.0:11434":         "http://0.0.0.0:11434",
		"https://gpu.example/":  "https://gpu.example",
		"http://127.0.0.1:9999": "http://127.0.0.1:9999",
	} {
		t.Setenv("OLLAMA_HOST", host)
		if got := OllamaURL(); got != want {
			t.Errorf("OLLAMA_HOST=%q: %q, want %q", host, got, want)
		}
	}
}
