package termimg

import (
	"github.com/charmbracelet/colorprofile"
	"testing"
)

func TestParseMode(t *testing.T) {
	for s, want := range map[string]Mode{"": Auto, "auto": Auto, "blocks": Blocks, "kitty": KittyMode} {
		if got, err := ParseMode(s); err != nil || got != want {
			t.Fatalf("%q: %v %v", s, got, err)
		}
	}
	if _, err := ParseMode("sixel"); err == nil {
		t.Fatal("invalid mode accepted")
	}
}

func TestResolve(t *testing.T) {
	old := tmuxPassthrough
	t.Cleanup(func() { tmuxPassthrough = old })
	for _, tc := range []struct {
		name    string
		mode    Mode
		env     []string
		profile colorprofile.Profile
		pass    bool
		want    Mode
		warn    bool
	}{
		{"kitty id", Auto, []string{"KITTY_WINDOW_ID=1"}, colorprofile.TrueColor, false, KittyMode, false},
		{"kitty term", Auto, []string{"TERM=xterm-kitty"}, colorprofile.TrueColor, false, KittyMode, false},
		{"ghostty", Auto, []string{"TERM_PROGRAM=ghostty"}, colorprofile.ANSI256, false, KittyMode, false},
		{"unknown", Auto, []string{"TERM=xterm-256color"}, colorprofile.TrueColor, false, Blocks, false},
		{"wezterm", Auto, []string{"TERM_PROGRAM=WezTerm"}, colorprofile.TrueColor, false, Blocks, false},
		{"tmux off auto", Auto, []string{"TMUX=x", "KITTY_WINDOW_ID=1"}, colorprofile.TrueColor, false, Blocks, false},
		{"tmux off forced", KittyMode, []string{"TMUX=x"}, colorprofile.TrueColor, false, Blocks, true},
		{"tmux on", Auto, []string{"TMUX=x", "GHOSTTY_RESOURCES_DIR=x"}, colorprofile.TrueColor, true, KittyMode, false},
		{"low color", KittyMode, nil, colorprofile.ANSI, false, Blocks, true},
		{"blocks", Blocks, []string{"KITTY_WINDOW_ID=1"}, colorprofile.TrueColor, false, Blocks, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tmuxPassthrough = func(string) bool { return tc.pass }
			got, tmux, warn := Resolve(tc.mode, tc.env, tc.profile)
			if got != tc.want || (warn != "") != tc.warn || tmux != (env(tc.env, "TMUX") != "") {
				t.Fatalf("%v %v %q", got, tmux, warn)
			}
		})
	}
}
