package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/ai"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

type pickerSetup struct {
	saved   [][2]any // (model, on) per Save call
	ollama  []string
	ollamaE error
}

var testCatalog = []ai.ModelEntry{
	{Provider: "anthropic", Model: "claude-sonnet-5", Name: "Claude Sonnet 5", Ready: true},
	{Provider: "openai", Model: "gpt-5.6", Name: "GPT-5.6", Ready: true},
	{Provider: "xai", Model: "grok-4.6", Name: "Grok 4.6", Missing: "XAI_API_KEY"},
}

func newPickerModel(t *testing.T, model string, on bool) (*Model, *pickerSetup) {
	t.Helper()
	s := &pickerSetup{ollama: []string{"gpt-oss:20b", "qwen3:1.7b"}}
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: "x := 1"}}}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}, AI: &AIConfig{
		Model: model, On: on,
		New:    func(model string) Assistant { return &fakeAssistant{name: model} },
		Models: func() []ai.ModelEntry { return testCatalog },
		LocalModels: func(context.Context) ([]string, error) {
			return s.ollama, s.ollamaE
		},
		Save: func(model string, on bool) error {
			s.saved = append(s.saved, [2]any{model, on})
			return nil
		},
	}})
	m.width, m.height = 100, 30
	m.leaveEdit()
	return m, s
}

// runPickerCmds runs cmd synchronously, delivering Ollama listings.
func runPickerCmds(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			runPickerCmds(m, c)
		}
	case ollamaModelsMsg:
		m.handleOllamaModels(msg)
	}
}

var keyM = tea.KeyPressMsg{Code: 'M', Text: "M"}

func typePicker(m *Model, s string) {
	for _, r := range s {
		m.handleKey(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func selectedRow(m *Model) pickRow { return m.picker.rows[m.picker.idx] }

// TestPickerToggle: M then enter turns AI off, and M then enter again
// turns the same model back on.
func TestPickerToggle(t *testing.T) {
	m, s := newPickerModel(t, "anthropic/claude-sonnet-5", true)
	m.handleKey(keyM)
	if m.overlay != overlayModel || selectedRow(m).kind != pickOff {
		t.Fatalf("picker: overlay=%v row=%+v", m.overlay, selectedRow(m))
	}
	screen := screenText(m)
	for _, want := range []string{"AI model", "Off", "● anthropic/claude-sonnet-5", "in use", "Ollama", "local models", "enter turn AI off"} {
		if !strings.Contains(screen, want) {
			t.Errorf("picker lacks %q:\n%s", want, screen)
		}
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone || m.assistant != nil || m.aiModel != "anthropic/claude-sonnet-5" {
		t.Fatalf("off: overlay=%v assistant=%v model=%q", m.overlay, m.assistant, m.aiModel)
	}
	m.handleKey(keyF) // no fix key while off
	if m.task.active {
		t.Fatal("f works while AI is off")
	}

	m.handleKey(keyM)
	if r := selectedRow(m); r.id != "anthropic/claude-sonnet-5" || r.desc != "last used" {
		t.Fatalf("reopened on %+v", r)
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.assistant == nil || m.assistant.Model() != "anthropic/claude-sonnet-5" {
		t.Fatalf("not back on: %v", m.assistant)
	}
	want := [][2]any{{"anthropic/claude-sonnet-5", false}, {"anthropic/claude-sonnet-5", true}}
	if len(s.saved) != 2 || s.saved[0] != want[0] || s.saved[1] != want[1] {
		t.Errorf("saved %v, want %v", s.saved, want)
	}
}

func TestPickerFilterAndChoose(t *testing.T) {
	m, s := newPickerModel(t, "", false)
	m.handleKey(keyM)
	// Off with no model yet: the first usable model is selected.
	if r := selectedRow(m); r.id != "anthropic/claude-sonnet-5" {
		t.Fatalf("initial row %+v", r)
	}
	typePicker(m, "gpt")
	if len(m.picker.rows) == 0 || selectedRow(m).id != "openai/gpt-5.6" {
		t.Fatalf("filter gpt: %+v", m.picker.rows)
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.assistant == nil || m.aiModel != "openai/gpt-5.6" || len(s.saved) != 1 {
		t.Fatalf("chose: model=%q saved=%v", m.aiModel, s.saved)
	}
	if !strings.Contains(m.status, "openai/gpt-5.6") {
		t.Errorf("status %q", m.status)
	}
}

func TestPickerModelWithoutKey(t *testing.T) {
	m, s := newPickerModel(t, "", false)
	m.handleKey(keyM)
	typePicker(m, "grok")
	if r := selectedRow(m); r.id != "xai/grok-4.6" || r.needs == "" {
		t.Fatalf("row %+v", r)
	}
	if !strings.Contains(screenText(m), "needs XAI_API_KEY to use xai/grok-4.6") {
		t.Errorf("no hint:\n%s", screenText(m))
	}
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayModel || m.assistant != nil || len(s.saved) != 0 {
		t.Fatal("a model without a key was chosen")
	}
}

func TestPickerOllama(t *testing.T) {
	m, s := newPickerModel(t, "", false)
	m.handleKey(keyM)
	typePicker(m, "ollama")
	if selectedRow(m).kind != pickOllama {
		t.Fatalf("row %+v", selectedRow(m))
	}
	runPickerCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	screen := screenText(m)
	if !m.picker.ollama || !strings.Contains(screen, "Ollama models") || !strings.Contains(screen, "gpt-oss:20b") {
		t.Fatalf("ollama stage:\n%s", screen)
	}
	if m.picker.filter.Value() != "" {
		t.Error("filter kept across stages")
	}
	typePicker(m, "qwen")
	m.handleKey(press(tea.KeyEnter, 0))
	if m.overlay != overlayNone || m.aiModel != "ollama/qwen3:1.7b" || m.assistant == nil || len(s.saved) != 1 {
		t.Fatalf("chose: overlay=%v model=%q", m.overlay, m.aiModel)
	}

	// Reopened, the Ollama model is the current one, and esc steps back
	// from the Ollama list before closing.
	m.handleKey(keyM)
	if !strings.Contains(screenText(m), "● ollama/qwen3:1.7b") {
		t.Errorf("current ollama model not listed:\n%s", screenText(m))
	}
	typePicker(m, "ollama")
	m.picker.idx = 0
	for m.picker.idx < len(m.picker.rows) && selectedRow(m).kind != pickOllama {
		m.picker.idx++
	}
	runPickerCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	if r := selectedRow(m); r.id != "ollama/qwen3:1.7b" || !r.active {
		t.Errorf("ollama stage opened on %+v", r)
	}
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayModel || m.picker.ollama || selectedRow(m).kind != pickOllama {
		t.Fatalf("esc should go back: overlay=%v ollama=%v", m.overlay, m.picker.ollama)
	}
	m.handleKey(press(tea.KeyEscape, 0))
	if m.overlay != overlayNone {
		t.Fatal("esc should close")
	}
}

func TestPickerOllamaUnavailable(t *testing.T) {
	m, s := newPickerModel(t, "", false)
	s.ollamaE = errors.New("can't reach Ollama at http://localhost:11434: is it running?")
	m.handleKey(keyM)
	typePicker(m, "ollama")
	runPickerCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	if !strings.Contains(screenText(m), "is it running?") {
		t.Fatalf("no error shown:\n%s", screenText(m))
	}

	s.ollamaE, s.ollama = nil, nil
	m.handleKey(press(tea.KeyEscape, 0))
	runPickerCmds(m, m.handleKey(press(tea.KeyEnter, 0)))
	if !strings.Contains(screenText(m), "ollama pull") {
		t.Fatalf("no hint for an empty Ollama:\n%s", screenText(m))
	}
}

// TestPickerSwitchCancelsFix: a fix belongs to the model it was asked of.
func TestPickerSwitchCancelsFix(t *testing.T) {
	m, _ := newPickerModel(t, "anthropic/claude-sonnet-5", true)
	f := &fakeAssistant{block: true}
	m.assistant = f
	m.cells[0].status = statusFailed
	m.cells[0].outputs = []notebook.Output{{Kind: notebook.Error, Text: "boom"}}
	batch := m.handleKey(keyF)
	if !m.task.active {
		t.Fatal("fix not started")
	}
	m.handleKey(keyM)
	m.handleKey(press(tea.KeyEnter, 0)) // Off
	if m.task.active {
		t.Fatal("switching AI off left the fix running")
	}
	runTaskCmds(m, batch)
	if m.task.pending() || m.overlay != overlayNone {
		t.Fatal("the cancelled fix surfaced")
	}
}

func TestPickerMouse(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.handleKey(keyM)
	screenText(m)
	for _, z := range m.zones {
		if z.act.kind == actModelItem && m.picker.rows[z.act.cell].id == "openai/gpt-5.6" {
			m.doAction(z.act)
			if m.aiModel != "openai/gpt-5.6" || m.overlay != overlayNone {
				t.Fatalf("click: model=%q overlay=%v", m.aiModel, m.overlay)
			}
			return
		}
	}
	t.Fatal("no clickable row for openai/gpt-5.6")
}

func TestPickerHelp(t *testing.T) {
	m, _ := newPickerModel(t, "", false)
	m.overlay = overlayHelp
	screen := screenText(m)
	if !strings.Contains(screen, "AI model / on-off") || strings.Contains(screen, "AI fix error") {
		t.Errorf("help while off:\n%s", screen)
	}
	// Nothing else mentions AI while it's off.
	m.overlay = overlayNone
	if s := screenText(m); strings.Contains(s, "AI") || strings.Contains(s, "fix") {
		t.Errorf("AI shown outside help:\n%s", s)
	}
}

func TestScoreRow(t *testing.T) {
	row := func(p, mdl string) pickRow {
		return pickRow{kind: pickModel, id: p + "/" + mdl, entry: ai.ModelEntry{Provider: p, Model: mdl}}
	}
	exact := scoreRow("gpt-5.6", row("openai", "gpt-5.6"))
	prefix := scoreRow("gpt", row("openai", "gpt-5.6"))
	fuzzy := scoreRow("gp56", row("openai", "gpt-5.6"))
	if exact <= prefix || prefix <= fuzzy || fuzzy <= 0 {
		t.Errorf("exact=%d prefix=%d fuzzy=%d", exact, prefix, fuzzy)
	}
	if scoreRow("zzz", row("openai", "gpt-5.6")) != 0 {
		t.Error("unrelated query matched")
	}
	// Regression: "20b" picked gpt-oss:120b, alphabetically first.
	if a, b := scoreRow("20b", row("ollama", "gpt-oss:20b")), scoreRow("20b", row("ollama", "gpt-oss:120b")); a <= b {
		t.Errorf("20b: gpt-oss:20b=%d, gpt-oss:120b=%d", a, b)
	}
}
