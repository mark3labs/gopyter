//go:build !noai

package ai

import (
	"fmt"
	"slices"
	"strings"

	kit "github.com/mark3labs/kit/pkg/kit"
)

// Provider is a provider gopyter knows a default model for.
type Provider struct {
	ID    string // kit provider id, e.g. "anthropic"
	Model string // default model, without the provider prefix
}

// Providers are the shorthands accepted by Resolve ("gopyter model
// anthropic"). Any other "provider/model" kit supports works too; this
// list only saves users from looking up a model name. Model catalogs change
// monthly, so it is kept short, and a test checks every entry is still in
// kit's catalog and not deprecated.
var Providers = []Provider{
	{"anthropic", "claude-sonnet-5"},
	{"openai", "gpt-5.6"},
	{"google", "gemini-flash-latest"},
	{"openrouter", "anthropic/claude-sonnet-5"},
	{"xai", "grok-4.6"},
	{"mistral", "mistral-medium-latest"},
	{"deepseek", "deepseek-v4-pro"},
}

// Resolve turns what a user typed into a model setting. "" and "off"
// disable AI and yield "". A bare provider name picks that provider's
// default model; otherwise s must be "provider/model" with a provider kit
// knows. Models missing from kit's catalog are accepted (it lags behind
// new releases), see Warning.
func Resolve(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, Off) {
		return "", nil
	}
	provider, model, ok := strings.Cut(s, "/")
	if !ok {
		if i := slices.IndexFunc(Providers, func(p Provider) bool { return strings.EqualFold(p.ID, s) }); i >= 0 {
			return Providers[i].ID + "/" + Providers[i].Model, nil
		}
		return "", fmt.Errorf("%q is not a provider/model (shorthands: %s)", s, strings.Join(providerIDs(), ", "))
	}
	if provider == "" || model == "" {
		return "", fmt.Errorf("%q is not a provider/model", s)
	}
	if kit.GetProviderInfo(provider) == nil {
		return "", fmt.Errorf("unknown provider %q", provider)
	}
	return s, nil
}

// Warning returns a note for a resolved model that kit's catalog doesn't
// list, which is usually a typo. It is empty for known models and for
// providers without a catalog, like ollama, which accept any local model.
func Warning(model string) string {
	provider, name, _ := strings.Cut(model, "/")
	if kit.LookupModel(provider, name) != nil {
		return ""
	}
	if ms, err := kit.GetModelsForProvider(provider); err != nil || len(ms) == 0 {
		return ""
	}
	w := fmt.Sprintf("%s is not in kit's model catalog; it is passed to %s as is", name, provider)
	if s := kit.SuggestModels(provider, name); len(s) > 0 {
		w += " (similar: " + strings.Join(s[:min(len(s), 3)], ", ") + ")"
	}
	return w
}

// Ready reports whether credentials for model's provider are available,
// without contacting the provider. The error says how to provide them.
func Ready(model string) error {
	provider, _, _ := strings.Cut(model, "/")
	if kit.ValidateEnvironment(provider, "") == nil {
		return nil
	}
	return fmt.Errorf("no API key for %s: set %s", provider, EnvHint(provider))
}

// EnvHint names the environment variable(s) that hold provider's key.
func EnvHint(provider string) string {
	if info := kit.GetProviderInfo(provider); info != nil && len(info.Env) > 0 {
		return strings.Join(info.Env, " or ")
	}
	return "its API key"
}

func providerIDs() []string {
	ids := make([]string, len(Providers))
	for i, p := range Providers {
		ids[i] = p.ID
	}
	return ids
}
