package console

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/complete"
)

func (m *model) complete() tea.Cmd {
	if len(m.comp.items) > 1 {
		m.comp.index = (m.comp.index + 1) % len(m.comp.items)
		m.applyCompletion(m.comp.inserted)
		return nil
	}
	line := []rune(strings.Split(m.ta.Value(), "\n")[m.ta.Line()])
	prefix := string(line[:min(m.ta.Column(), len(line))])
	if strings.TrimSpace(prefix) == "" {
		m.ta.InsertString("\t")
		return nil
	}
	if m.opts.Completer == nil {
		return nil
	}
	m.clearCompletion()
	ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
	m.comp.cancel = cancel
	seq := m.comp.seq
	req := complete.Request{CellID: m.nextID, Src: m.ta.Value(), Row: m.ta.Line(), Col: m.ta.Column(), Manual: true}
	return func() tea.Msg {
		defer cancel()
		result, err := m.opts.Completer.Complete(ctx, req)
		return completionMsg{seq: seq, result: result, err: err}
	}
}
func (m *model) applyCompletion(replace int) {
	it := m.comp.items[m.comp.index]
	for range replace {
		m.ta, _ = m.ta.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	m.ta.InsertString(it.Insert)
	m.comp.inserted = utf8.RuneCountInString(it.Insert)
}
