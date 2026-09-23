package ui

import (
	"errors"
	"image"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func themeModel(t *testing.T, opts Options) *Model {
	t.Helper()
	opts.Notebook = &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Code, Source: "x := 1"},
		{ID: "b", Type: notebook.Markdown, Source: "# Title"},
	}}
	// A zero kernel is enough to render the header.
	opts.Kernel = &kernel.Kernel{}
	m := New(opts)
	m.width, m.height = 100, 40
	t.Cleanup(func() { setPalette(defaultTheme().palette(true)) })
	return m
}

func keyp(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return press(tea.KeyEnter, 0)
	case "esc":
		return press(tea.KeyEscape, 0)
	case "down":
		return press(tea.KeyDown, 0)
	}
	return tea.KeyPressMsg{Code: []rune(s)[0], Text: s}
}

// The default theme must keep gopyter's original Go-brand look.
func TestDefaultThemeKeepsGopyterColors(t *testing.T) {
	m := themeModel(t, Options{})
	if m.themes.name != DefaultTheme || ThemeNames()[0] != DefaultTheme {
		t.Fatalf("default theme = %q, first = %q", m.themes.name, ThemeNames()[0])
	}
	want := map[string]string{"primary": "#00ADD8", "text": "#E4E4E7", "ink": "#101014"}
	got := map[string]any{"primary": colPrimary, "text": colText, "ink": colInk}
	for k, hex := range want {
		if got[k] != lipgloss.Color(hex) {
			t.Errorf("%s = %v, want %s", k, got[k], hex)
		}
	}
	if m.hl.style != styles.Get("catppuccin-mocha") {
		t.Errorf("default syntax style = %s, want catppuccin-mocha", m.hl.style.Name)
	}
}

func TestKitThemesAvailable(t *testing.T) {
	names := strings.Join(ThemeNames(), ",")
	for _, n := range []string{"kitt", "catppuccin", "dracula", "tokyonight", "nord", "gruvbox", "zenburn"} {
		if !strings.Contains(","+names+",", ","+n+",") {
			t.Errorf("missing theme %q in %s", n, names)
		}
	}
}

// Every theme variant must resolve to valid colors and a real syntax style.
func TestThemePalettesValid(t *testing.T) {
	for _, d := range themes() {
		for _, dark := range []bool{false, true} {
			p := d.palette(dark)
			for i, c := range []string{
				p.primary, p.info, p.accent, p.success, p.warning, p.error, p.errorBar, p.orange,
				p.selection, p.raised, p.text, p.dim, p.muted, p.subtle, p.faint, p.ink,
			} {
				if len(c) != 7 || c[0] != '#' {
					t.Errorf("%s dark=%v: color %d = %q", d.name, dark, i, c)
				}
			}
			if st := syntaxStyle(d.name, p, dark); st == styles.Fallback {
				t.Errorf("%s dark=%v: syntax style fell back", d.name, dark)
			}
		}
	}
}

func TestBlendHex(t *testing.T) {
	if got := blendHex("#000000", "#ffffff", 0.5); got != "#808080" {
		t.Fatalf("blend = %s", got)
	}
	if got := blendHex("#102030", "#ffffff", 0); got != "#102030" {
		t.Fatalf("blend at 0 = %s", got)
	}
}

func TestThemeOptionAndUnknownFallback(t *testing.T) {
	m := themeModel(t, Options{Theme: "dracula"})
	if m.themes.name != "dracula" || colPrimary != lipgloss.Color("#bd93f9") {
		t.Fatalf("theme = %s primary = %v", m.themes.name, colPrimary)
	}
	themeModel(t, Options{Theme: "dracula", LightBackground: true})
	if colPrimary != lipgloss.Color("#7c6bf5") {
		t.Fatalf("light dracula primary = %v", colPrimary)
	}
	m = themeModel(t, Options{Theme: "no-such-theme"})
	if m.themes.name != DefaultTheme {
		t.Fatalf("unknown theme should fall back, got %s", m.themes.name)
	}
}

func TestThemePickerPreviewCancelConfirm(t *testing.T) {
	var saved []string
	m := themeModel(t, Options{SaveTheme: func(n string) error { saved = append(saved, n); return nil }})
	m.renderScreen() // populate caches that a theme change must drop
	if m.cells[1].mdOut == "" {
		t.Fatal("markdown should be rendered")
	}

	m.handleKey(keyp("T"))
	if m.overlay != overlayTheme {
		t.Fatalf("T should open the picker, overlay=%v", m.overlay)
	}
	m.handleKey(keyp("down"))
	previewed := m.themes.names[1]
	if m.themes.name != previewed {
		t.Fatalf("moving should preview %s, active is %s", previewed, m.themes.name)
	}
	if m.cells[1].mdOut != "" {
		t.Fatal("theme change should drop cached markdown")
	}
	m.handleKey(keyp("esc"))
	if m.overlay != overlayNone || m.themes.name != DefaultTheme || len(saved) != 0 {
		t.Fatalf("esc should restore: overlay=%v theme=%s saved=%v", m.overlay, m.themes.name, saved)
	}
	if colPrimary != lipgloss.Color("#00ADD8") {
		t.Fatalf("palette not restored: %v", colPrimary)
	}

	m.handleKey(keyp("T"))
	m.handleKey(keyp("j"))
	m.handleKey(keyp("j"))
	m.handleKey(keyp("enter"))
	want := m.themes.names[2]
	if m.overlay != overlayNone || m.themes.name != want || len(saved) != 1 || saved[0] != want {
		t.Fatalf("enter should apply and save %s: theme=%s saved=%v", want, m.themes.name, saved)
	}
}

func TestThemePickerSaveError(t *testing.T) {
	m := themeModel(t, Options{SaveTheme: func(string) error { return errors.New("read-only") }})
	m.handleKey(keyp("T"))
	m.handleKey(keyp("j"))
	m.handleKey(keyp("enter"))
	if m.themes.name == DefaultTheme || m.statusKind != statusError {
		t.Fatalf("theme should apply with an error status: theme=%s status=%q", m.themes.name, m.status)
	}
}

func TestThemePickerMouse(t *testing.T) {
	var saved string
	m := themeModel(t, Options{SaveTheme: func(n string) error { saved = n; return nil }})

	// The toolbar button opens the picker.
	m.renderScreen()
	var btn zone
	for _, z := range m.zones {
		if z.act.kind == actTheme {
			btn = z
		}
	}
	if btn.rect.Empty() {
		t.Fatal("no theme button in the toolbar")
	}
	m.handleMouseDown(tea.Mouse{X: btn.rect.Min.X, Y: btn.rect.Min.Y, Button: tea.MouseLeft})
	if m.overlay != overlayTheme {
		t.Fatalf("clicking the button should open the picker, overlay=%v", m.overlay)
	}

	// Clicking a row applies and saves that theme.
	m.renderScreen()
	var row zone
	for _, z := range m.zones {
		if z.act.kind == actThemeItem && z.act.cell == 3 {
			row = z
		}
	}
	if row.rect.Empty() {
		t.Fatal("no zone for picker row 3")
	}
	pt := row.rect.Min.Add(image.Pt(1, 0))
	m.click = clickState{} // not a double click
	m.handleMouseDown(tea.Mouse{X: pt.X, Y: pt.Y, Button: tea.MouseLeft})
	if want := m.themes.names[3]; m.overlay != overlayNone || m.themes.name != want || saved != want {
		t.Fatalf("click should apply %s: overlay=%v theme=%s saved=%s", want, m.overlay, m.themes.name, saved)
	}

	// Clicking outside cancels.
	m.openThemePicker()
	m.renderScreen()
	m.handleMouseDown(tea.Mouse{X: 0, Y: m.height - 1, Button: tea.MouseLeft})
	if m.overlay != overlayNone || m.themes.name != saved {
		t.Fatalf("outside click should cancel: overlay=%v theme=%s", m.overlay, m.themes.name)
	}
}

// With few rows the picker scrolls to keep the selection visible.
func TestThemePickerScrolls(t *testing.T) {
	m := themeModel(t, Options{})
	m.height = 14
	m.openThemePicker()
	m.renderScreen()
	for range len(m.themes.names) - 1 {
		m.handleKey(keyp("j"))
		m.renderScreen()
	}
	ts := m.themes
	if ts.idx != len(ts.names)-1 || ts.idx < ts.top || ts.idx >= ts.top+ts.rows {
		t.Fatalf("selection %d not visible in [%d,%d)", ts.idx, ts.top, ts.top+ts.rows)
	}
	screen, _ := m.renderScreen()
	if !strings.Contains(screen, ts.names[ts.idx]) {
		t.Fatalf("selected theme %s not on screen", ts.names[ts.idx])
	}
}
