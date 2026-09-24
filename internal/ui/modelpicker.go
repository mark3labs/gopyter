package ui

import (
	"cmp"
	"context"
	"image"
	"slices"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/ai"
)

// AIConfig configures the optional AI features. A nil AIConfig (the noai
// build) hides all of them, including the model picker.
type AIConfig struct {
	// Model is the selected "provider/model". It is kept while AI is off,
	// so turning AI back on returns to it.
	Model string
	// On says whether AI features are on.
	On bool
	// New returns a Fixer using model.
	New func(model string) Fixer
	// Models lists the models the picker offers.
	Models func() []ai.ModelEntry
	// LocalModels lists the models installed in the local Ollama server.
	LocalModels func(ctx context.Context) ([]string, error)
	// Save persists a choice made in the picker (optional).
	Save func(model string, on bool) error
}

type pickKind int

const (
	pickOff    pickKind = iota // turn AI off
	pickModel                  // use a model
	pickOllama                 // open the list of installed Ollama models
)

// pickRow is a row of the model picker.
type pickRow struct {
	kind   pickKind
	id     string // "provider/model", for pickModel
	label  string
	desc   string
	needs  string // why it can't be picked (missing API key); "" if it can
	active bool   // the current state: the model in use, or Off
	entry  ai.ModelEntry
}

// modelPicker is the AI model picker: a filterable list of every model,
// like kit's /model selector, plus a second stage listing the models
// installed in Ollama.
type modelPicker struct {
	ollama  bool      // showing installed Ollama models
	all     []pickRow // rows of the current stage
	rows    []pickRow // all, filtered
	idx     int
	top     int
	visible int // rows shown at the last render
	filter  textinput.Model
	query   string // filter the rows were computed for
	loading bool   // listing Ollama models
	err     string
	seq     int // matches Ollama listings to their request
}

type ollamaModelsMsg struct {
	seq    int
	models []string
	err    error
}

func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.Placeholder = "type to filter"
	ti.SetWidth(40)
	return ti
}

// openModelPicker shows the picker. The row selected at first is the
// other state (Off while AI is on, the last model while it's off), so M
// then enter toggles AI.
func (m *Model) openModelPicker() tea.Cmd {
	if m.ai == nil {
		return nil
	}
	m.closeCompletion()
	p := &m.picker
	p.ollama, p.loading, p.err = false, false, ""
	p.seq++
	p.all = m.pickerRows()
	p.filter.SetValue("")
	p.query = "\x00" // force refiltering
	m.refilterPicker()
	p.idx = 0
	if m.fixer == nil {
		usable := func(k pickKind) func(pickRow) bool {
			return func(r pickRow) bool { return r.kind == k && r.needs == "" }
		}
		if i := slices.IndexFunc(p.rows, usable(pickModel)); i >= 0 {
			p.idx = i
		} else if i := slices.IndexFunc(p.rows, usable(pickOllama)); i >= 0 {
			p.idx = i
		}
	}
	p.top = 0
	m.overlay = overlayModel
	return p.filter.Focus()
}

// pickerRows builds the first stage: Off, the current or last model,
// Ollama, then the catalog.
func (m *Model) pickerRows() []pickRow {
	on := m.fixer != nil
	rows := []pickRow{{kind: pickOff, label: "Off", desc: "no AI features", active: !on}}
	if m.aiModel != "" {
		desc := "last used"
		if on {
			desc = "in use"
		}
		provider, model, _ := strings.Cut(m.aiModel, "/")
		rows = append(rows, pickRow{
			kind: pickModel, id: m.aiModel, label: m.aiModel, desc: desc, active: on,
			entry: ai.ModelEntry{Provider: provider, Model: model},
		})
	}
	rows = append(rows, pickRow{kind: pickOllama, label: "Ollama", desc: "local models ›"})
	if m.ai.Models != nil {
		for _, e := range m.ai.Models() {
			if e.ID() == m.aiModel {
				continue // listed above
			}
			r := pickRow{kind: pickModel, id: e.ID(), label: e.ID(), desc: e.Name, entry: e}
			if e.Recommended {
				r.desc = strings.TrimSpace(e.Name + " · recommended")
			}
			if !e.Ready {
				r.needs = "needs " + e.Missing
				r.desc = r.needs
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// refilterPicker applies the filter text to the rows of the current stage.
func (m *Model) refilterPicker() {
	p := &m.picker
	q := strings.ToLower(strings.TrimSpace(p.filter.Value()))
	if q == p.query {
		return
	}
	p.query = q
	p.idx, p.top = 0, 0
	if q == "" {
		p.rows = p.all
		return
	}
	type scored struct {
		row   pickRow
		score int
	}
	var hits []scored
	for _, r := range p.all {
		if s := scoreRow(q, r); s > 0 {
			hits = append(hits, scored{r, s})
		}
	}
	// Like kit: by relevance, then usable models first.
	slices.SortStableFunc(hits, func(a, b scored) int {
		if a.score != b.score {
			return b.score - a.score
		}
		if (a.row.needs == "") != (b.row.needs == "") {
			if a.row.needs == "" {
				return -1
			}
			return 1
		}
		return cmp.Or(len(a.row.id)-len(b.row.id), cmp.Compare(a.row.id, b.row.id))
	})
	p.rows = make([]pickRow, len(hits))
	for i, h := range hits {
		p.rows[i] = h.row
	}
}

// scoreRow ranks a row against a lowercase query; 0 means no match. Models
// are scored like kit's model selector.
func scoreRow(q string, r pickRow) int {
	if r.kind != pickModel {
		if strings.Contains(strings.ToLower(r.label+" "+r.desc), q) {
			return 900 // few special rows: keep them on top when they match
		}
		return 0
	}
	model := strings.ToLower(r.entry.Model)
	combined := strings.ToLower(r.entry.Provider) + "/" + model
	name := strings.ToLower(r.entry.Name)
	switch {
	case combined == q:
		return 1000
	case model == q:
		return 950
	case strings.HasPrefix(model, q):
		return 800 - len(model) + len(q)
	case strings.HasPrefix(combined, q):
		return 750 - len(combined) + len(q)
	case strings.Contains(model, q):
		return 600 + boundaryBonus(model, q)
	case strings.Contains(combined, q):
		return 550 + boundaryBonus(combined, q)
	case name != "" && strings.Contains(name, q):
		return 400
	}
	if s := fuzzyScore(q, model); s > 0 {
		return s
	}
	return max(fuzzyScore(q, combined)-20, 0)
}

// boundaryBonus favors a match starting a segment of s (after : / - . or
// _), so "20b" ranks gpt-oss:20b above gpt-oss:120b.
func boundaryBonus(s, q string) int {
	for i := 0; ; {
		j := strings.Index(s[i:], q)
		if j < 0 {
			return 0
		}
		if at := i + j; at > 0 && strings.ContainsRune(":/-._", rune(s[at-1])) {
			return 30
		}
		i += j + 1
	}
}

// fuzzyScore matches q's characters in order in target, rewarding runs.
func fuzzyScore(q, target string) int {
	qr, tr := []rune(q), []rune(target)
	if len(qr) > len(tr) {
		return 0
	}
	qi, score, run := 0, 100, 0
	for ti := 0; ti < len(tr) && qi < len(qr); ti++ {
		if tr[ti] == qr[qi] {
			qi++
			run++
			score += run * 10
		} else {
			run = 0
			score -= 5
		}
	}
	if qi < len(qr) {
		return 0
	}
	return max(score, 1)
}

// openOllamaStage lists the models installed in Ollama.
func (m *Model) openOllamaStage() tea.Cmd {
	p := &m.picker
	p.ollama, p.loading, p.err = true, true, ""
	p.all, p.rows = nil, nil
	p.filter.SetValue("")
	p.query = ""
	p.idx, p.top = 0, 0
	p.seq++
	seq, list := p.seq, m.ai.LocalModels
	if list == nil {
		p.loading, p.err = false, "listing Ollama models isn't supported"
		return nil
	}
	return tea.Batch(func() tea.Msg {
		models, err := list(context.Background())
		return ollamaModelsMsg{seq: seq, models: models, err: err}
	}, m.startSpinner())
}

func (m *Model) handleOllamaModels(msg ollamaModelsMsg) {
	p := &m.picker
	if msg.seq != p.seq || !p.ollama || m.overlay != overlayModel {
		return
	}
	p.loading = false
	if msg.err != nil {
		p.err = msg.err.Error()
		return
	}
	p.all = nil
	for _, name := range msg.models {
		id := ai.OllamaProvider + "/" + name
		p.all = append(p.all, pickRow{
			kind: pickModel, id: id, label: name,
			active: m.fixer != nil && id == m.aiModel,
			entry:  ai.ModelEntry{Provider: ai.OllamaProvider, Model: name, Ready: true},
		})
	}
	p.query = "\x00"
	m.refilterPicker()
	if i := slices.IndexFunc(p.rows, func(r pickRow) bool { return r.id == m.aiModel }); i >= 0 {
		p.idx = i
	}
}

// pickerBack leaves the Ollama stage, or closes the picker.
func (m *Model) pickerBack() tea.Cmd {
	p := &m.picker
	if !p.ollama {
		m.closeModelPicker()
		return nil
	}
	p.ollama, p.loading, p.err = false, false, ""
	p.seq++
	p.all = m.pickerRows()
	p.filter.SetValue("")
	p.query = "\x00"
	m.refilterPicker()
	p.idx = max(slices.IndexFunc(p.rows, func(r pickRow) bool { return r.kind == pickOllama }), 0)
	return nil
}

func (m *Model) closeModelPicker() {
	m.overlay = overlayNone
	m.picker.filter.Blur()
	m.picker.seq++ // drop an Ollama listing still in flight
}

// movePicker moves the selection by d rows.
func (m *Model) movePicker(d int) {
	p := &m.picker
	if len(p.rows) == 0 {
		return
	}
	p.idx = clamp(p.idx+d, 0, len(p.rows)-1)
	if p.visible > 0 {
		p.top = clamp(p.top, p.idx-p.visible+1, p.idx)
	}
}

// choosePicker acts on row i: keyboard enter and mouse clicks both end
// up here.
func (m *Model) choosePicker(i int) tea.Cmd {
	p := &m.picker
	if i < 0 || i >= len(p.rows) {
		return nil
	}
	p.idx = i
	r := p.rows[i]
	switch {
	case r.needs != "":
		return nil // the hint line says what's missing
	case r.kind == pickOllama:
		return m.openOllamaStage()
	case r.kind == pickOff:
		m.closeModelPicker()
		return m.setAI(m.aiModel, false)
	default:
		m.closeModelPicker()
		return m.setAI(r.id, true)
	}
}

// setAI switches AI on with model, or off (keeping model to return to).
func (m *Model) setAI(model string, on bool) tea.Cmd {
	// A fix in progress or awaiting review belongs to the old setting.
	if m.fix.active {
		m.fix.cancel()
	}
	m.fix = fixState{seq: m.fix.seq + 1}
	m.aiModel = model
	m.fixer = nil
	if on && model != "" && m.ai.New != nil {
		m.fixer = m.ai.New(model)
	}
	on = m.fixer != nil

	if m.ai.Save != nil {
		if err := m.ai.Save(model, on); err != nil {
			return m.setStatus(statusError, "AI setting applied but not saved: %v", err)
		}
	}
	if !on {
		return m.setStatus(statusInfo, "AI off · M to turn it back on")
	}
	if err := m.fixer.Ready(); err != nil {
		return m.setStatus(statusError, "AI model %s: %v", model, err)
	}
	return m.setStatus(statusSuccess, "AI model %s · f fixes a failing cell", model)
}

// handleModelPickerKey handles keys while the picker is open. Letters go
// to the filter, so navigation uses the arrow keys.
func (m *Model) handleModelPickerKey(msg tea.KeyPressMsg) tea.Cmd {
	p := &m.picker
	page := max(p.visible-1, 1)
	switch msg.String() {
	case "up", "ctrl+p", "shift+tab":
		m.movePicker(-1)
	case "down", "ctrl+n", "tab":
		m.movePicker(1)
	case "pgup":
		m.movePicker(-page)
	case "pgdown":
		m.movePicker(page)
	case "enter":
		return m.choosePicker(p.idx)
	case "esc":
		return m.pickerBack()
	case "ctrl+c":
		m.closeModelPicker()
	default:
		var cmd tea.Cmd
		p.filter, cmd = p.filter.Update(msg)
		m.refilterPicker()
		return cmd
	}
	return nil
}

// renderModelPicker renders the picker and its clickable rows, relative to
// the overlay's top-left corner.
func (m *Model) renderModelPicker() (string, []zone) {
	t := m.theme
	p := &m.picker
	width := clamp(m.width-2-2*dlgPadX-4, 30, 88)

	title := "◆ AI model"
	if p.ollama {
		title = "◆ Ollama models"
	}
	head := lipgloss.NewStyle().Foreground(colPrimary).Bold(true).Render(title)
	if len(p.rows) > 0 {
		head += t.muted.Render("    " + itoa(p.idx+1) + "/" + itoa(len(p.rows)))
	}
	p.filter.SetWidth(width - 2)
	lines := []string{head, "", p.filter.View(), ""}

	// Border, padding, the lines above, and the blank + hint lines below.
	p.visible = clamp(m.height-2-2*dlgPadY-len(lines)-2, 3, 20)
	p.top = clamp(p.top, max(p.idx-p.visible+1, 0), p.idx)
	p.top = clamp(p.top, 0, max(len(p.rows)-p.visible, 0))

	var zones []zone
	switch {
	case p.loading:
		lines = append(lines, t.muted.Render(m.spinner.View()+" listing installed models…"))
	case p.err != "":
		for _, l := range wrapLines([]string{p.err}, width) {
			lines = append(lines, t.errorText.Render(l))
		}
	case len(p.rows) == 0 && p.ollama && p.query == "":
		lines = append(lines, t.muted.Render("no models installed · ollama pull <model>"))
	case len(p.rows) == 0:
		lines = append(lines, t.muted.Render("no match"))
	}
	labelW := 0
	end := min(p.top+p.visible, len(p.rows))
	for _, r := range p.rows[p.top:end] {
		labelW = max(labelW, lipgloss.Width(r.label))
	}
	labelW = min(labelW, width*3/5)
	for i := p.top; i < end; i++ {
		r := p.rows[i]
		mark := "  "
		if r.active {
			mark = "● "
		}
		text := mark + padRight(ansi.Truncate(r.label, labelW, "…"), labelW)
		if r.desc != "" {
			text += "  " + r.desc
		}
		text = padRight(ansi.Truncate(" "+text, width, "…"), width)
		switch {
		case i == p.idx:
			text = t.btnHover.Render(text)
		case r.needs != "":
			text = t.subtle.Render(text)
		case r.active:
			text = lipgloss.NewStyle().Foreground(colPrimary).Render(text)
		default:
			text = t.text.Render(text)
		}
		y := 1 + dlgPadY + len(lines)
		zones = append(zones, zone{rect: image.Rect(1+dlgPadX, y, 1+dlgPadX+width, y+1), act: action{kind: actModelItem, cell: i}})
		lines = append(lines, text)
	}

	lines = append(lines, "", m.pickerHint(width))
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colPrimary).Padding(dlgPadY, dlgPadX)
	return box.Render(lipgloss.JoinVertical(lipgloss.Left, lines...)), zones
}

// pickerHint says what enter does on the selected row.
func (m *Model) pickerHint(width int) string {
	t := m.theme
	p := &m.picker
	back := " close"
	if p.ollama {
		back = " back"
	}
	var what string
	if p.idx < len(p.rows) {
		r := p.rows[p.idx]
		switch {
		case r.needs != "":
			return ansi.Truncate(lipgloss.NewStyle().Foreground(colWarning).Render(r.needs+" to use "+r.label), width, "…")
		case r.kind == pickOff:
			what = " turn AI off"
		case r.kind == pickOllama:
			what = " list installed models"
		default:
			what = " use " + r.label
		}
	}
	h := t.helpKey.Render("↑↓") + t.helpDesc.Render(" move  ")
	if what != "" {
		h += t.helpKey.Render("enter") + t.helpDesc.Render(what+"  ")
	}
	h += t.helpKey.Render("esc") + t.helpDesc.Render(back)
	return ansi.Truncate(h, width, "…")
}
