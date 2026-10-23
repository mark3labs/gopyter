package termimg

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/colorprofile"
)

// Mode selects an image renderer.
type Mode int

const (
	Auto Mode = iota
	Blocks
	KittyMode
)

// ParseMode accepts auto (also empty), kitty or blocks.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", "auto":
		return Auto, nil
	case "blocks":
		return Blocks, nil
	case "kitty":
		return KittyMode, nil
	default:
		return Auto, fmt.Errorf("unknown graphics mode %q (want auto, kitty or blocks)", s)
	}
}

func env(environ []string, key string) string {
	for _, entry := range environ {
		if v, ok := strings.CutPrefix(entry, key+"="); ok {
			return v
		}
	}
	return ""
}

// DetectKitty detects placeholder-capable terminals without querying stdin.
// WezTerm's partial Kitty support does not include Unicode placeholders.
func DetectKitty(environ []string) bool {
	return env(environ, "KITTY_WINDOW_ID") != "" || env(environ, "TERM") == "xterm-kitty" ||
		env(environ, "TERM_PROGRAM") == "ghostty" || env(environ, "GHOSTTY_RESOURCES_DIR") != ""
}

// tmuxPassthrough checks the active pane first, then the global option.
// A bounded subprocess never consumes the application's stdin.
var tmuxPassthrough = func(pane string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if pane != "" {
		out, err := exec.CommandContext(ctx, "tmux", "show-options", "-pqv", "-t", pane, "allow-passthrough").Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return passthroughOn(out)
		}
	}
	out, err := exec.CommandContext(ctx, "tmux", "show-options", "-gqv", "allow-passthrough").Output()
	return err == nil && passthroughOn(out)
}

func passthroughOn(out []byte) bool {
	s := strings.TrimSpace(string(out))
	return s == "on" || s == "all"
}

// Resolve chooses a renderer compatible with the environment and color profile.
// An explicit kitty request warns when prerequisites are missing.
func Resolve(m Mode, environ []string, p colorprofile.Profile) (mode Mode, tmux bool, warn string) {
	tmux = env(environ, "TMUX") != ""
	if m == Blocks || (m == Auto && !DetectKitty(environ)) {
		return Blocks, tmux, ""
	}
	reason := ""
	if p < colorprofile.ANSI256 {
		reason = "kitty graphics needs at least 256 colors"
	} else if tmux && !tmuxPassthrough(env(environ, "TMUX_PANE")) {
		reason = "kitty graphics needs tmux allow-passthrough on (set -g allow-passthrough on)"
	}
	if reason != "" {
		if m == KittyMode {
			warn = reason + "; using blocks"
		}
		return Blocks, tmux, warn
	}
	return KittyMode, tmux, ""
}
