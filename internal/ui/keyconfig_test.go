package ui

import (
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
)

func TestKeyCommandsCoverKeyMap(t *testing.T) {
	var k keyMap
	v := reflect.ValueOf(&k).Elem()
	fields := map[any]bool{}
	names := map[string]bool{}
	for _, c := range keyCommands {
		ptr := c.field(&k)
		if fields[ptr] || names[c.name] || !regexp.MustCompile(`^[a-z]+(_[a-z]+)*$`).MatchString(c.name) {
			t.Fatalf("duplicate or invalid command: %s", c.name)
		}
		fields[ptr], names[c.name] = true, true
	}
	for i := 0; i < v.NumField(); i++ {
		if !fields[v.Field(i).Addr().Interface()] {
			t.Errorf("missing field %s", v.Type().Field(i).Name)
		}
	}
}

func TestKeyCommandNamesStable(t *testing.T) {
	want := strings.Fields("restart_clear save_as to_raw toggle_input scroll_output_left scroll_output_right external_edit copy_output run_advance run run_insert save interrupt input up down top bottom page_up page_down edit insert_above insert_below delete undelete cut copy paste move_up move_down to_markdown to_code toggle_output clear_output clear_all_output run_all restart help theme toggle_vim fix ask ai_model palette quit escape undo redo info")
	var got []string
	for _, c := range keyCommands {
		got = append(got, c.name)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("names: %v", got)
	}
}

func TestDefaultKeysNoConflicts(t *testing.T) {
	got, w := newKeyMapFrom(nil)
	if len(w) != 0 || !reflect.DeepEqual(got, newKeyMap()) {
		t.Fatalf("defaults changed: %v", w)
	}
}

func TestParseKey(t *testing.T) {
	for _, s := range []string{"ctrl+s", "A", "?", "f1", "pgdown", "space", "+", "ctrl++", "ctrl+shift+a"} {
		if got, err := parseKey(s); err != nil || got != s {
			t.Errorf("%q: %q %v", s, got, err)
		}
	}
	if got, err := parseKey("shift+ctrl+x"); err != nil || got != "ctrl+shift+x" {
		t.Fatalf("normalization: %s %v", got, err)
	}
	for _, s := range []string{"", "ctrl+", "hyperr+x", "ctrl+ctrl+x", "foo", "shift+a", "ctrl+A", " ", "\n"} {
		if _, err := parseKey(s); err == nil {
			t.Errorf("accepted %q", s)
		}
	}
}

func TestParseKeyNamesMatchBubbleTea(t *testing.T) {
	for _, code := range []rune{tea.KeyEnter, tea.KeyTab, tea.KeyEscape, tea.KeyPgDown, tea.KeyF1, tea.KeyHome, tea.KeyDelete, tea.KeySpace, tea.KeyBackspace, tea.KeyInsert, tea.KeyEnd, tea.KeyUp} {
		s := tea.KeyPressMsg{Code: code}.String()
		if got, err := parseKey(s); err != nil || got != s {
			t.Errorf("%q: %q %v", s, got, err)
		}
	}
}

func TestKeyOverrides(t *testing.T) {
	for _, tc := range []struct {
		name      string
		overrides map[string][]string
		warnings  int
		command   string
		keys      []string
	}{
		{"replace", map[string][]string{"run_all": {"ctrl+a"}}, 0, "run_all", []string{"ctrl+a"}},
		{"unbind", map[string][]string{"quit": {}}, 0, "quit", nil},
		{"null", map[string][]string{"quit": nil}, 0, "quit", nil},
		{"unknown", map[string][]string{"rnu_all": {"x"}}, 1, "run_all", []string{"A"}},
		{"same mode", map[string][]string{"run_all": {"a"}}, 1, "run_all", nil},
		{"global command", map[string][]string{"save": {"q"}}, 1, "save", nil},
		{"global edit", map[string][]string{"save": {"alt+k"}}, 1, "save", nil},
		{"cross mode", map[string][]string{"undo": {"d"}}, 0, "undo", []string{"d"}},
		{"swap", map[string][]string{"cut": {"c"}, "copy": {"x"}}, 0, "cut", []string{"c"}},
		{"both overridden", map[string][]string{"cut": {"ctrl+g"}, "copy": {"ctrl+g"}}, 1, "copy", nil},
		{"escape", map[string][]string{"escape": {}}, 1, "escape", []string{"esc"}},
		{"bad escape", map[string][]string{"escape": {"foo"}}, 2, "escape", []string{"esc"}},
		{"partial", map[string][]string{"run_all": {"foo", "ctrl+a"}}, 1, "run_all", []string{"ctrl+a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, w := newKeyMapFrom(tc.overrides)
			if len(w) != tc.warnings {
				t.Fatalf("warnings %v", w)
			}
			for _, c := range keyCommands {
				if c.name == tc.command && !slices.Equal(c.field(&k).Keys(), tc.keys) {
					t.Errorf("keys: %v", c.field(&k).Keys())
				}
			}
		})
	}
}

func TestRebindDelete(t *testing.T) {
	m := New(Options{Notebook: ranNotebook(), Keys: map[string][]string{"delete": {"X"}}})
	m.width, m.height = 100, 30
	m.handleKey(synthKey("d"))
	m.handleKey(synthKey("d"))
	if len(m.cells) != 3 {
		t.Fatal("old delete still works")
	}
	m.handleKey(synthKey("X"))
	if !strings.Contains(ansi.Strip(m.renderFooter()), "X…") {
		t.Fatal("missing pending hint")
	}
	m.handleKey(synthKey("X"))
	if len(m.cells) != 2 {
		t.Fatal("rebound delete did not work")
	}
}

func TestRebindEditKeys(t *testing.T) {
	m := New(Options{Keys: map[string][]string{"undo": {"ctrl+u"}, "escape": {"ctrl+g"}}})
	m.cur().ed.InsertText("hello")
	m.handleKey(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if m.cur().ed.Value() != "" {
		t.Fatal("undo did not take priority")
	}
	m.handleKey(synthKey("esc"))
	if m.mode != modeEdit {
		t.Fatal("old escape still bound")
	}
	m.handleKey(tea.KeyPressMsg{Code: 'g', Mod: tea.ModCtrl})
	if m.mode != modeCommand {
		t.Fatal("escape did not work")
	}
}

func TestMouseActionsIgnoreKeys(t *testing.T) {
	m := New(Options{Notebook: ranNotebook(), Keys: map[string][]string{"insert_above": {"ctrl+a"}, "move_up": {}}})
	m.doAction(action{kind: actInsertAbove, cell: 0})
	if len(m.cells) != 4 {
		t.Fatal("insert failed")
	}
	c := m.cells[1]
	m.doAction(action{kind: actMoveUp, cell: 1})
	if m.cells[0] != c {
		t.Fatal("unbound move failed")
	}
}

func TestKeyWarningsInStatus(t *testing.T) {
	m := New(Options{Keys: map[string][]string{"bogus": {"x"}, "oops": {"z"}}})
	_ = m.Init()
	if !strings.Contains(m.status, "bogus") || !strings.Contains(m.status, "(+1 more · gopyter keys)") || m.statusKind != statusError {
		t.Fatalf("status: %s", m.status)
	}
}

func TestTooltipsShowUserKeys(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	m := New(Options{Kernel: k, Notebook: ranNotebook(), Keys: map[string][]string{"run_all": {"ctrl+a"}, "theme": {}, "quit": {}, "insert_above": {"ctrl+g"}}})
	m.width, m.height = 120, 35
	_ = m.View()
	found := false
	for _, z := range m.zones {
		if z.act.kind == actRunAll {
			found = true
			if !strings.HasSuffix(z.tip, "· ^a") {
				t.Errorf("tip: %s", z.tip)
			}
		}
		if z.act.kind == actTheme && strings.Contains(z.tip, "·") {
			t.Errorf("unbound tip: %s", z.tip)
		}
	}
	if !found {
		t.Fatal("missing toolbar")
	}
	for _, sec := range m.keys.fullHelp(false, false, false) {
		for _, b := range sec.keys {
			if b.Help().Desc == "quit" || b.Help().Desc == "color theme" {
				t.Fatal("unbound help included")
			}
		}
	}
	m.openContextMenu(10, headerHeight+1)
	for _, item := range m.menu.items {
		if item.act.kind == actInsertAbove && item.key != "^g" {
			t.Errorf("menu key: %s", item.key)
		}
	}
}

func TestHelpOverlayLongKeys(t *testing.T) {
	m := New(Options{Keys: map[string][]string{"run": {"ctrl+shift+alt+enter"}}})
	m.width, m.height = 80, 40
	m.overlay = overlayHelp
	text, _ := m.renderOverlay()
	for _, line := range strings.Split(text, "\n") {
		if ansi.StringWidth(line) > 80 {
			t.Fatalf("help exceeds viewport: %d", ansi.StringWidth(line))
		}
	}
}

func TestOverrideDispatch(t *testing.T) {
	m := New(Options{Notebook: ranNotebook(), Keys: map[string][]string{"run_all": {"ctrl+a"}, "quit": nil}})
	if name, ok := m.keys.commandFor(synthKey("A")); ok {
		t.Fatalf("old key dispatches %s", name)
	}
	if name, ok := m.keys.commandFor(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}); !ok || name != "run_all" {
		t.Fatal("new key not dispatched")
	}
	if m.keys.RunAll.Help().Key != "^a" || m.keys.Quit.Enabled() {
		t.Fatal("help or unbinding incorrect")
	}
	if cmd := m.handleKey(synthKey("q")); cmd != nil || m.overlay != overlayNone {
		t.Fatal("unbound quit still acts")
	}
}

func TestEscapeSurvivesConflicts(t *testing.T) {
	k, w := newKeyMapFrom(map[string][]string{"save": {"ctrl+g"}, "escape": {"ctrl+g"}, "undo": {"esc"}})
	if len(w) == 0 || !slices.Equal(k.Escape.Keys(), []string{"esc"}) || slices.Contains(k.Undo.Keys(), "esc") {
		t.Fatalf("escape lost: %v %v", k.Escape.Keys(), w)
	}
}

func TestPrettyKeys(t *testing.T) {
	if got := prettyKeys([]string{"super+pgup", "ctrl+enter", "x"}); got != "super+pgup/^↵" {
		t.Fatalf("pretty keys: %q", got)
	}
	k, w := newKeyMapFrom(map[string][]string{"delete": {"ctrl+x"}})
	if len(w) != 0 || k.Delete.Help().Key != "^x ^x" {
		t.Fatalf("delete help: %q %v", k.Delete.Help().Key, w)
	}
}

func TestConflictPriorityKeepsDefaultBeforeOverrides(t *testing.T) {
	// Save loses to default edit. It must not first strip run_all's key.
	k, w := newKeyMapFrom(map[string][]string{"save": {"enter"}, "run_all": {"enter"}})
	if len(w) != 2 || len(k.Save.Keys()) != 0 || len(k.RunAll.Keys()) != 0 || !slices.Contains(k.Edit.Keys(), "enter") {
		t.Fatalf("priority: %v", w)
	}
	// An edit and command override can share a key once a global loses it.
	k, w = newKeyMapFrom(map[string][]string{"save": {"ctrl+g"}, "run_all": {"ctrl+g"}, "undo": {"ctrl+g"}})
	if len(w) != 2 || !slices.Contains(k.Save.Keys(), "ctrl+g") {
		t.Fatalf("global priority: %v", w)
	}
}
