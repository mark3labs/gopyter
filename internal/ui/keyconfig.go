package ui

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type keyMode int

const (
	bindGlobal keyMode = iota
	bindCommand
	bindEdit
)

// Names are a permanent config.json contract, independent of Go field names.
type keyCommand struct {
	name  string
	mode  keyMode
	field func(*keyMap) *key.Binding
}

var keyCommands = []keyCommand{
	{"restart_clear", bindGlobal, func(k *keyMap) *key.Binding { return &k.RestartClear }},
	{"save_as", bindGlobal, func(k *keyMap) *key.Binding { return &k.SaveAs }},
	{"to_raw", bindCommand, func(k *keyMap) *key.Binding { return &k.ToRaw }},
	{"toggle_input", bindCommand, func(k *keyMap) *key.Binding { return &k.ToggleInput }},
	{"scroll_output_left", bindCommand, func(k *keyMap) *key.Binding { return &k.ScrollLeft }},
	{"scroll_output_right", bindCommand, func(k *keyMap) *key.Binding { return &k.ScrollRight }},
	{"external_edit", bindCommand, func(k *keyMap) *key.Binding { return &k.ExtEdit }},
	{"copy_output", bindCommand, func(k *keyMap) *key.Binding { return &k.CopyOutput }},
	{"run_advance", bindGlobal, func(k *keyMap) *key.Binding { return &k.RunAdvance }},
	{"run", bindGlobal, func(k *keyMap) *key.Binding { return &k.Run }},
	{"run_insert", bindGlobal, func(k *keyMap) *key.Binding { return &k.RunInsert }},
	{"save", bindGlobal, func(k *keyMap) *key.Binding { return &k.Save }},
	{"interrupt", bindGlobal, func(k *keyMap) *key.Binding { return &k.Interrupt }},
	{"input", bindGlobal, func(k *keyMap) *key.Binding { return &k.Input }},
	{"up", bindCommand, func(k *keyMap) *key.Binding { return &k.Up }},
	{"down", bindCommand, func(k *keyMap) *key.Binding { return &k.Down }},
	{"top", bindCommand, func(k *keyMap) *key.Binding { return &k.Top }},
	{"bottom", bindCommand, func(k *keyMap) *key.Binding { return &k.Bottom }},
	{"page_up", bindCommand, func(k *keyMap) *key.Binding { return &k.PageUp }},
	{"page_down", bindCommand, func(k *keyMap) *key.Binding { return &k.PageDown }},
	{"edit", bindCommand, func(k *keyMap) *key.Binding { return &k.Edit }},
	{"insert_above", bindCommand, func(k *keyMap) *key.Binding { return &k.InsertAbove }},
	{"insert_below", bindCommand, func(k *keyMap) *key.Binding { return &k.InsertBelow }},
	{"delete", bindCommand, func(k *keyMap) *key.Binding { return &k.Delete }},
	{"undelete", bindCommand, func(k *keyMap) *key.Binding { return &k.Undelete }},
	{"cut", bindCommand, func(k *keyMap) *key.Binding { return &k.Cut }},
	{"copy", bindCommand, func(k *keyMap) *key.Binding { return &k.Copy }},
	{"paste", bindCommand, func(k *keyMap) *key.Binding { return &k.Paste }},
	{"move_up", bindCommand, func(k *keyMap) *key.Binding { return &k.MoveUp }},
	{"move_down", bindCommand, func(k *keyMap) *key.Binding { return &k.MoveDown }},
	{"to_markdown", bindCommand, func(k *keyMap) *key.Binding { return &k.ToMarkdown }},
	{"to_code", bindCommand, func(k *keyMap) *key.Binding { return &k.ToCode }},
	{"toggle_output", bindCommand, func(k *keyMap) *key.Binding { return &k.ToggleOutput }},
	{"clear_output", bindCommand, func(k *keyMap) *key.Binding { return &k.ClearOutput }},
	{"clear_all_output", bindCommand, func(k *keyMap) *key.Binding { return &k.ClearAllOutput }},
	{"run_all", bindCommand, func(k *keyMap) *key.Binding { return &k.RunAll }},
	{"restart", bindCommand, func(k *keyMap) *key.Binding { return &k.Restart }},
	{"help", bindCommand, func(k *keyMap) *key.Binding { return &k.Help }},
	{"theme", bindCommand, func(k *keyMap) *key.Binding { return &k.Theme }},
	{"sidebar", bindCommand, func(k *keyMap) *key.Binding { return &k.Sidebar }},
	{"toggle_vim", bindCommand, func(k *keyMap) *key.Binding { return &k.ToggleVim }},
	{"fix", bindCommand, func(k *keyMap) *key.Binding { return &k.Fix }},
	{"ask", bindCommand, func(k *keyMap) *key.Binding { return &k.Ask }},
	{"ai_model", bindCommand, func(k *keyMap) *key.Binding { return &k.AIModel }},
	{"quit", bindCommand, func(k *keyMap) *key.Binding { return &k.Quit }},
	{"escape", bindEdit, func(k *keyMap) *key.Binding { return &k.Escape }},
	{"undo", bindEdit, func(k *keyMap) *key.Binding { return &k.Undo }},
	{"redo", bindEdit, func(k *keyMap) *key.Binding { return &k.Redo }},
	{"info", bindEdit, func(k *keyMap) *key.Binding { return &k.Info }},
}

// parseKey accepts Bubble Tea's canonical key names and modifier order.
func parseKey(s string) (string, error) {
	mods := []string{"ctrl", "alt", "shift", "meta", "hyper", "super"}
	seen := map[string]bool{}
	base := s
	for utf8.RuneCountInString(base) > 1 && strings.Contains(base, "+") {
		mod, rest, _ := strings.Cut(base, "+")
		if !slices.Contains(mods, mod) || seen[mod] {
			return "", fmt.Errorf("unknown or repeated modifier in %q", s)
		}
		seen[mod], base = true, rest
	}
	names := strings.Fields("enter tab backspace esc space up down left right begin find insert delete select pgup pgdown home end equal mul plus comma minus period div sep capslock scrolllock numlock printscreen pause menu mediaplay mediapause mediaplaypause mediareverse mediastop mediafastforward mediarewind medianext mediaprev mediarecord lowervol raisevol mute leftshift leftalt leftctrl leftsuper lefthyper leftmeta rightshift rightalt rightctrl rightsuper righthyper rightmeta isolevel3shift isolevel5shift")
	valid := slices.Contains(names, base)
	for i := 1; i <= 63; i++ {
		valid = valid || base == fmt.Sprintf("f%d", i)
	}
	r, _ := utf8.DecodeRuneInString(base)
	if utf8.RuneCountInString(base) == 1 && unicode.IsPrint(r) && !unicode.IsSpace(r) {
		valid = true
		if seen["shift"] && len(seen) == 1 && unicode.IsLetter(r) {
			return "", fmt.Errorf("%q may never match; use %q", s, string(unicode.ToUpper(r)))
		}
		if len(seen) > 0 && unicode.IsUpper(r) {
			var prefix string
			for _, m := range mods {
				if seen[m] || m == "shift" {
					prefix += m + "+"
				}
			}
			return "", fmt.Errorf("%q may never match; use %q", s, prefix+string(unicode.ToLower(r)))
		}
	}
	if !valid {
		return "", fmt.Errorf("unknown key %q", s)
	}
	var result string
	for _, m := range mods {
		if seen[m] {
			result += m + "+"
		}
	}
	return result + base, nil
}

func prettyKeys(keys []string) string {
	out := make([]string, 0, min(2, len(keys)))
	for _, k := range keys[:min(2, len(keys))] {
		parts := strings.Split(k, "+")
		for i, p := range parts {
			if label, ok := map[string]string{"enter": "↵", "space": "␣", "up": "↑", "down": "↓", "left": "←", "right": "→"}[p]; ok {
				parts[i] = label
			}
		}
		k = strings.Join(parts, "+")
		k = strings.NewReplacer("ctrl+", "^", "shift+", "⇧").Replace(k)
		out = append(out, k)
	}
	return strings.Join(out, "/")
}

func newKeyMapFrom(overrides map[string][]string) (keyMap, []string) {
	k := newKeyMap()
	var warnings []string
	known := map[string]bool{}
	for _, c := range keyCommands {
		known[c.name] = true
	}
	var unknown []string
	for name := range overrides {
		if !known[name] {
			unknown = append(unknown, name)
		}
	}
	slices.Sort(unknown)
	for _, name := range unknown {
		warnings = append(warnings, fmt.Sprintf("unknown command %q", name))
	}
	for _, c := range keyCommands {
		values, ok := overrides[c.name]
		if !ok {
			continue
		}
		var keys []string
		for _, value := range values {
			parsed, err := parseKey(value)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: %v", c.name, err))
				continue
			}
			if !slices.Contains(keys, parsed) {
				keys = append(keys, parsed)
			}
		}
		if c.name == "escape" && len(keys) == 0 {
			warnings = append(warnings, "escape cannot be unbound; keeping esc")
			continue
		}
		c.field(&k).SetKeys(keys...)
	}
	// Resolve on the final map (so swaps work). Defaults always win;
	// among overrides, the earlier table entry wins.
	priority := slices.Clone(keyCommands)
	slices.SortStableFunc(priority, func(a, b keyCommand) int {
		_, ao := overrides[a.name]
		_, bo := overrides[b.name]
		if ao == bo {
			return 0
		}
		if ao {
			return 1
		}
		return -1
	})
	for i, c := range priority {
		b := c.field(&k)
		var kept []string
		for _, value := range b.Keys() {
			winner := ""
			for _, d := range priority[:i] {
				if c.mode != d.mode && c.mode != bindGlobal && d.mode != bindGlobal {
					continue
				}
				if slices.Contains(d.field(&k).Keys(), value) {
					winner = d.name
					break
				}
			}
			if winner == "" {
				kept = append(kept, value)
			} else {
				warnings = append(warnings, fmt.Sprintf("%s: key %q conflicts with %s; ignored", c.name, value, winner))
			}
		}
		if !slices.Equal(b.Keys(), kept) {
			b.SetKeys(kept...)
		}
	}
	if len(k.Escape.Keys()) == 0 {
		warnings = append(warnings, "escape cannot be unbound after conflicts; keeping esc")
		k.Escape.SetKeys("esc")
		for _, c := range keyCommands {
			if c.mode == bindCommand || c.name == "escape" {
				continue
			}
			b := c.field(&k)
			if slices.Contains(b.Keys(), "esc") {
				b.SetKeys(slices.DeleteFunc(slices.Clone(b.Keys()), func(s string) bool { return s == "esc" })...)
				warnings = append(warnings, fmt.Sprintf("%s: key %q conflicts with escape; ignored", c.name, "esc"))
			}
		}
	}
	for _, c := range keyCommands {
		b := c.field(&k)
		defaults := newKeyMap()
		_, overridden := overrides[c.name]
		if overridden || !slices.Equal(b.Keys(), c.field(&defaults).Keys()) {
			label := prettyKeys(b.Keys())
			if c.name == "delete" {
				var labels []string
				for _, value := range b.Keys()[:min(2, len(b.Keys()))] {
					p := prettyKeys([]string{value})
					if utf8.RuneCountInString(value) == 1 {
						labels = append(labels, p+p)
					} else {
						labels = append(labels, p+" "+p)
					}
				}
				label = strings.Join(labels, "/")
			}
			b.SetHelp(label, b.Help().Desc)
		}
	}
	return k, warnings
}

// KeyCommand describes a rebindable command for 'gopyter keys'.
type KeyCommand struct {
	Name, Mode, Desc string
	Keys, Defaults   []string
}

// KeyCommands lists commands in stable help order and reports ignored entries.
func KeyCommands(overrides map[string][]string) ([]KeyCommand, []string) {
	k, warnings := newKeyMapFrom(overrides)
	defaults := newKeyMap()
	var result []KeyCommand
	for _, c := range keyCommands {
		b := c.field(&k)
		result = append(result, KeyCommand{c.name, []string{"both", "command", "edit"}[c.mode], b.Help().Desc, slices.Clone(b.Keys()), slices.Clone(c.field(&defaults).Keys())})
	}
	return result, warnings
}

func (k keyMap) commandFor(msg tea.KeyPressMsg) (string, bool) {
	for _, c := range keyCommands {
		if c.mode == bindCommand && key.Matches(msg, *c.field(&k)) {
			return c.name, true
		}
	}
	return "", false
}
