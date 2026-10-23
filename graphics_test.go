package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mark3labs/gopyter/internal/config"
	"github.com/spf13/cobra"
)

func TestResolveGraphics(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("TMUX", "")
	t.Setenv("NO_COLOR", "")
	var warnings bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetErr(&warnings)
	if _, err := resolveGraphics(cmd, "sixel", config.Settings{}); err == nil {
		t.Fatal("bad flag accepted")
	}
	if gfx, err := resolveGraphics(cmd, "blocks", config.Settings{Graphics: "kitty"}); err != nil || gfx != nil {
		t.Fatal(gfx, err)
	}
	if _, err := resolveGraphics(cmd, "", config.Settings{Graphics: "sixel"}); err != nil || !strings.Contains(warnings.String(), "using auto") {
		t.Fatal(err, warnings.String())
	}
	root := rootCmd()
	run, _, err := root.Find([]string{"run"})
	if err != nil {
		t.Fatal(err)
	}
	run.InheritedFlags()
	if run.Flag("graphics") == nil {
		t.Fatal("run did not inherit graphics flag")
	}
}
