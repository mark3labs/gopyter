// Package config persists user settings across gopyter sessions.
//
// Settings live in a JSON file under the user's configuration directory as
// reported by os.UserConfigDir: $XDG_CONFIG_HOME/gopyter/config.json (usually
// ~/.config/gopyter/config.json) on Linux and ~/Library/Application
// Support/gopyter/config.json on macOS.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// Settings are the persisted user preferences.
type Settings struct {
	// Theme is the name of the UI color theme.
	Theme string `json:"theme,omitempty"`
	// Vim enables vim key bindings in edit mode.
	Vim bool `json:"vim,omitempty"`
	// AIModel is the "provider/model" used for AI features, kept while they
	// are off so they can be turned back on. API keys are never stored here.
	AIModel string `json:"ai_model,omitempty"`
	// AIOff turns AI features off without forgetting AIModel. AI is on
	// when AIModel is set and AIOff isn't.
	AIOff bool `json:"ai_off,omitempty"`
}

// Path returns the location of the settings file.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gopyter", "config.json"), nil
}

// Load reads the settings from the default location. A missing file yields
// zero settings and no error.
func Load() (Settings, error) {
	p, err := Path()
	if err != nil {
		return Settings{}, err
	}
	return LoadFrom(p)
}

// LoadFrom reads the settings from path. A missing file yields zero settings
// and no error.
func LoadFrom(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

// Update applies mutate to the settings at the default location and writes
// them back.
func Update(mutate func(*Settings)) error {
	p, err := Path()
	if err != nil {
		return err
	}
	return UpdateAt(p, mutate)
}

// UpdateAt applies mutate to the settings stored at path and writes them
// back. Keys this version of gopyter doesn't know about are preserved, so an
// older binary never drops settings written by a newer one. The file is
// replaced atomically so concurrent instances can't leave it half-written.
func UpdateAt(path string, mutate func(*Settings)) error {
	raw := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return err
	default:
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	var s Settings
	if len(data) > 0 {
		if err := json.Unmarshal(data, &s); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	mutate(&s)

	// Merge the known fields over the raw map.
	known, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var knownRaw map[string]json.RawMessage
	if err := json.Unmarshal(known, &knownRaw); err != nil {
		return err
	}
	// Known fields are omitempty, so a zero value must remove its key.
	st := reflect.TypeFor[Settings]()
	for field := range st.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		delete(raw, name)
	}
	maps.Copy(raw, knownRaw)

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()           // the write error is the one worth reporting
		_ = os.Remove(tmp.Name()) // best-effort cleanup of the partial file
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name()) // best-effort cleanup of the partial file
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name()) // best-effort cleanup; the rename error matters
		return err
	}
	return nil
}
