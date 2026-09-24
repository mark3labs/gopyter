//go:build !noai

package ai

import (
	"strings"
	"testing"

	kit "github.com/mark3labs/kit/pkg/kit"
)

// TestProvidersInCatalog fails when a kit upgrade drops or deprecates a
// default model, so the table gets updated rather than silently breaking
// "gopyter model <provider>".
func TestProvidersInCatalog(t *testing.T) {
	for _, p := range Providers {
		info := kit.LookupModel(p.ID, p.Model)
		if info == nil {
			t.Errorf("%s/%s is not in kit's catalog", p.ID, p.Model)
			continue
		}
		if info.Status == "deprecated" {
			t.Errorf("%s/%s is deprecated", p.ID, p.Model)
		}
	}
}

func TestResolve(t *testing.T) {
	cases := []struct{ in, want, err string }{
		{"", "", ""},
		{"off", "", ""},
		{" OFF ", "", ""},
		{"anthropic", "anthropic/claude-sonnet-5", ""},
		{"Anthropic", "anthropic/claude-sonnet-5", ""},
		{"openai/gpt-5.6", "openai/gpt-5.6", ""},
		{"ollama/qwen3-coder", "ollama/qwen3-coder", ""},
		{"anthropic/", "", "not a provider/model"},
		{"notaprovider", "", "shorthands: anthropic"},
		{"nope/model", "", "unknown provider"},
	}
	for _, c := range cases {
		got, err := Resolve(c.in)
		if c.err != "" {
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Errorf("Resolve(%q) error = %v, want %q", c.in, err, c.err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

func TestWarning(t *testing.T) {
	if w := Warning("anthropic/claude-sonnet-5"); w != "" {
		t.Errorf("known model warned: %s", w)
	}
	if w := Warning("ollama/anything"); w != "" {
		t.Errorf("catalog-less provider warned: %s", w)
	}
	if w := Warning("anthropic/claude-sonet-5"); !strings.Contains(w, "not in kit's model catalog") {
		t.Errorf("typo not flagged: %q", w)
	}
}

func TestReady(t *testing.T) {
	// A catalog provider with a single key variable and no stored kit
	// credentials in the isolated environment.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XAI_API_KEY", "")
	if err := Ready("xai/grok-4.6"); err == nil || !strings.Contains(err.Error(), "XAI_API_KEY") {
		t.Errorf("Ready without key = %v", err)
	}
	t.Setenv("XAI_API_KEY", "xai-test")
	if err := Ready("xai/grok-4.6"); err != nil {
		t.Errorf("Ready with key = %v", err)
	}
}
