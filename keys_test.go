package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/ui"
)

func TestKeysCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if runtime.GOOS == "darwin" {
		t.Setenv("HOME", dir)
	}
	path, err := config.Path()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"keys":{"run_all":["ctrl+a"],"quit":[],"bogus":["x"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	cmd := rootCmd()
	cmd.SetArgs([]string{"keys"})
	cmd.SetOut(&out)
	cmd.SetErr(&stderr)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	commands, _ := ui.KeyCommands(nil)
	for _, c := range commands {
		if !strings.Contains(out.String(), c.Name) {
			t.Errorf("missing %s", c.Name)
		}
	}
	if !strings.Contains(out.String(), "* run_all") || !strings.Contains(out.String(), "* quit") || !strings.Contains(out.String(), "ctrl+a") || !strings.Contains(out.String(), path) || !strings.Contains(stderr.String(), "bogus") {
		t.Fatalf("output:\n%s\nstderr:\n%s", out.String(), stderr.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "* quit") && !strings.Contains(line, "-") {
			t.Fatal("unbound quit not marked")
		}
	}
}
