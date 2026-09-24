//go:build !noai

package ai

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	kit "github.com/mark3labs/kit/pkg/kit"
)

// catalog is kit's model catalog without credential state, built once:
// it holds thousands of models and only changes with a kit upgrade.
var catalog = sync.OnceValue(func() []ModelEntry {
	var out []ModelEntry
	for _, p := range kit.GetLLMProviders() {
		// Ollama models come from the local server (OllamaModels); custom
		// models need a kit config, which isolated agents don't load.
		if p == OllamaProvider || p == "custom" {
			continue
		}
		ms, err := kit.GetModelsForProvider(p)
		if err != nil {
			continue
		}
		for id, info := range ms {
			out = append(out, ModelEntry{Provider: p, Model: id, Name: info.Name})
		}
	}
	return out
})

// Catalog lists the models kit knows for the model picker: those whose
// provider has credentials first, gopyter's recommended default of each
// provider leading its group, then by provider and model. Credentials are
// checked on every call, so a key exported since the last call counts.
func Catalog() []ModelEntry {
	out := slices.Clone(catalog())
	ready := map[string]bool{}
	for i := range out {
		p := out[i].Provider
		r, ok := ready[p]
		if !ok {
			r = kit.ValidateEnvironment(p, "") == nil
			ready[p] = r
		}
		out[i].Ready = r
		if !r {
			out[i].Missing = EnvHint(p)
		}
		out[i].Recommended = slices.Contains(Providers, Provider{p, out[i].Model})
	}
	first := func(a, b bool) int {
		switch {
		case a == b:
			return 0
		case a:
			return -1
		}
		return 1
	}
	slices.SortFunc(out, func(a, b ModelEntry) int {
		return cmp.Or(
			first(a.Ready, b.Ready),
			first(a.Recommended, b.Recommended),
			cmp.Compare(a.Provider, b.Provider),
			cmp.Compare(a.Model, b.Model),
		)
	})
	return out
}

// OllamaURL is the address of the Ollama server, from OLLAMA_HOST like
// kit and the ollama CLI.
func OllamaURL() string {
	host := strings.TrimSpace(os.Getenv("OLLAMA_HOST"))
	if host == "" {
		return "http://localhost:11434"
	}
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	return strings.TrimRight(host, "/")
}

// ollamaTimeout bounds the model listing: a local server answers at once,
// and the picker shouldn't hang when there is none.
const ollamaTimeout = 3 * time.Second

// OllamaModels lists the models installed in the local Ollama server,
// sorted by name.
func OllamaModels(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, ollamaTimeout)
	defer cancel()
	url := OllamaURL()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("can't reach Ollama at %s: is it running? (set OLLAMA_HOST for another address)", url)
	}
	defer func() { _ = resp.Body.Close() }() // read-only request: nothing to lose
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the Ollama server at %s answered %s", url, resp.Status)
	}
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("reading models from the Ollama server at %s: %w", url, err)
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		if m.Name != "" {
			names = append(names, m.Name)
		}
	}
	slices.Sort(names)
	return names, nil
}
