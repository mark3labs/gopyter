package ui

import (
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	"bloomfx.dev/bloom"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

// bodyText draws the notebook's part of the screen, the cells with their
// scrollbar and the sidebar, as a frame lays it out.
func bodyText(m *Model) string {
	var s screen
	m.renderBody(&s)
	return bloom.Render(m.width, m.viewHeight(), m.body(s))
}

// footerText draws the footer as a frame lays it out.
func footerText(m *Model) string { return bloom.Render(m.width, 1, m.renderFooter()) }

// stageModel is a notebook with a heading, for tests of the layout, with
// motion on or off.
func stageModel(motion bool) *Model {
	m := New(Options{Motion: motion, Kernel: &kernel.Kernel{}, Notebook: &notebook.Notebook{Cells: []*notebook.Cell{
		{ID: "a", Type: notebook.Markdown, Source: "# Heading"},
		{ID: "b", Type: notebook.Code, Source: "x := 1"},
	}}})
	m.width, m.height, m.mode = 120, 30, modeCommand
	return m
}

// settle moves the animations of m to their ends.
func settle(m *Model) {
	end := time.Now().Add(time.Minute)
	m.fx.overlay.Advance(end)
	m.fx.status.Advance(end)
}

func TestScreenFillsTerminal(t *testing.T) {
	for _, size := range []image.Point{{120, 30}, {61, 12}, {20, 6}} {
		m := stageModel(false)
		m.width, m.height = size.X, size.Y
		s, _ := m.renderScreen()
		lines := strings.Split(s, "\n")
		if len(lines) != size.Y {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size.X {
				t.Fatalf("%v: line %d is %d wide: %q", size, i, w, ansi.Strip(l))
			}
		}
		// The header and the footer keep their rows whatever is between.
		if !strings.Contains(ansi.Strip(lines[0]), "gopyter") || !strings.Contains(ansi.Strip(lines[size.Y-1]), "COMMAND") {
			t.Fatalf("%v: header %q, footer %q", size, ansi.Strip(lines[0]), ansi.Strip(lines[size.Y-1]))
		}
	}
}

// TestHeaderKeepsNotebookName checks which side of the header gives way on a
// narrow terminal: the kernel's state, not the notebook's name.
func TestHeaderKeepsNotebookName(t *testing.T) {
	m := stageModel(false)
	m.path = "analysis.ipynb"
	wide := ansi.Strip(bloom.Render(120, 1, m.renderHeader()))
	if !strings.Contains(wide, "analysis.ipynb") || !strings.HasSuffix(strings.TrimRight(wide, " "), "idle") {
		t.Fatalf("wide header: %q", wide)
	}
	narrow := ansi.Strip(bloom.Render(34, 1, m.renderHeader()))
	if !strings.Contains(narrow, "analysis.ipynb") || strings.Contains(narrow, "idle") {
		t.Fatalf("narrow header: %q", narrow)
	}
}

// TestOverlayPlacedByLayout checks that zones follow where bloom put a
// dialog, which nothing in the model works out anymore.
func TestOverlayPlacedByLayout(t *testing.T) {
	m := stageModel(false)
	m.dirty = true
	m.requestQuit()
	text := strings.Split(screenText(m), "\n")
	r := m.overlayRect
	if r.Empty() || r.Min.X != (m.width-r.Dx())/2 || r.Min.Y != (m.height-r.Dy())/2 {
		t.Fatalf("dialog not centered: %v", r)
	}
	if got := []rune(text[r.Min.Y])[r.Min.X]; got != '╭' {
		t.Fatalf("no dialog corner at %v: %q", r.Min, got)
	}
	var cancel zone
	for _, z := range m.zones {
		if !z.rect.In(r) {
			t.Fatalf("zone outside the dialog: %+v", z)
		}
		if z.act.kind == actDialogCancel {
			cancel = z
		}
	}
	label := string([]rune(text[cancel.rect.Min.Y])[cancel.rect.Min.X:cancel.rect.Max.X])
	if strings.TrimSpace(label) != "Cancel" {
		t.Fatalf("cancel zone is over %q", label)
	}
	m.handleMouseDown(tea.Mouse{X: cancel.rect.Min.X + 1, Y: cancel.rect.Min.Y, Button: tea.MouseLeft})
	if m.overlay != overlayNone {
		t.Fatal("click on Cancel left the dialog open")
	}
}

func TestMenuStaysOnScreen(t *testing.T) {
	var cells []*notebook.Cell
	for i := range 12 {
		cells = append(cells, &notebook.Cell{ID: fmt.Sprint("c", i), Type: notebook.Code, Source: "x := 1"})
	}
	m := New(Options{Kernel: &kernel.Kernel{}, Notebook: &notebook.Notebook{Cells: cells}})
	m.width, m.height, m.mode = 120, 44, modeCommand
	m.renderScreen()
	// On the last cell in view: no room below the pointer, nor beside it.
	x, y := m.bodyWidth()-3, m.height-footerHeight-1
	for m.cellAtScreen(x, y) < 0 {
		y--
	}
	m.openContextMenu(x, y)
	if m.overlay != overlayMenu {
		t.Fatal("no menu")
	}
	m.renderScreen()
	r := m.overlayRect
	if r.Empty() || !r.In(image.Rect(0, 0, m.width, m.height)) {
		t.Fatalf("menu at %v leaves the %dx%d screen", r, m.width, m.height)
	}
	if r.Max.X != m.width || r.Max.Y != y {
		t.Fatalf("menu at %v: want it against the right edge, above the pointer at row %d", r, y)
	}
	// Its rows are where it was drawn.
	for _, z := range m.zones {
		if !z.rect.In(r) {
			t.Fatalf("zone outside the menu: %+v", z)
		}
	}
}

// TestMotionOffDrawsAtOnce is the default for tests and for --no-motion:
// nothing is animated, so a frame always shows the model as it is.
func TestMotionOffDrawsAtOnce(t *testing.T) {
	m := stageModel(false)
	m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if m.overlay != overlayHelp || m.fx.overlay.Visible() {
		t.Fatalf("overlay %v, presence visible %v", m.overlay, m.fx.overlay.Visible())
	}
	if !strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("help not drawn")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("help still drawn after closing")
	}
	if v := m.View(); v.ReportFocus {
		t.Fatal("focus reports are only for slowing animations down")
	}
}

func TestOverlayFadesInAndOut(t *testing.T) {
	m := stageModel(true)
	_, cmd := m.Update(tea.KeyPressMsg{Code: '?', Text: "?"})
	if cmd == nil || !m.fx.overlay.Shown() || m.fx.kind != overlayHelp {
		t.Fatalf("opening didn't start the presence: shown %v, kind %v", m.fx.overlay.Shown(), m.fx.kind)
	}
	settle(m)
	if !strings.Contains(screenText(m), "Keyboard shortcuts") || m.overlayRect.Empty() {
		t.Fatal("help not drawn once it has opened")
	}
	at := m.overlayRect

	// Closing clears the overlay at once; only what is drawn lingers.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.overlay != overlayNone || m.fx.overlay.Shown() || !m.fx.overlay.Visible() {
		t.Fatalf("overlay %v, shown %v, visible %v", m.overlay, m.fx.overlay.Shown(), m.fx.overlay.Visible())
	}
	if !strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("help vanished instead of fading out")
	}
	if r, ok := m.ui.Frame().Rect(idOverlay); !ok || r.Min.X != at.Min.X {
		t.Fatalf("closing help drawn at %v, was at %v", r, at)
	}
	for _, z := range m.zones {
		if z.rect.In(at) && z.act.kind != actNone && z.rect.Min.Y > headerHeight && z.rect.Dx() > m.width/2 {
			t.Fatalf("zone of the closed dialog: %+v", z)
		}
	}
	// The notebook is interactive again while the dialog fades.
	if len(m.zones) == 0 {
		t.Fatal("no zones while the dialog closes")
	}
	settle(m)
	if strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("help still drawn after fading out")
	}
}

// TestOverlayUnsyncedDrawsAtOnce covers state set outside Update, as tests
// and some handlers do: the frame must show it rather than an animation
// that was never started.
func TestOverlayUnsyncedDrawsAtOnce(t *testing.T) {
	m := stageModel(true)
	m.overlay = overlayHelp
	if !strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("help not drawn")
	}
	m.overlay = overlayNone
	if strings.Contains(screenText(m), "Keyboard shortcuts") {
		t.Fatal("closed help drawn")
	}
}

// TestDialogReplacesDialog keeps the screen dimmed when one dialog opens
// from another, and starts over when a menu opens one.
func TestDialogReplacesDialog(t *testing.T) {
	m := stageModel(true)
	m.dirty, m.path = true, ""
	m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if m.overlay != overlayQuit {
		t.Fatalf("overlay %v", m.overlay)
	}
	settle(m)
	m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"}) // save: asks for a name
	if m.overlay != overlaySaveAs || m.fx.overlay.Progress() != 1 {
		t.Fatalf("overlay %v at %v: the dialog should take the other's place", m.overlay, m.fx.overlay.Progress())
	}

	m = stageModel(true)
	m.renderScreen()
	m.openContextMenu(30, headerHeight+4)
	m.Update(tea.KeyPressMsg{Code: 'x'}) // any message: the menu is noted
	settle(m)
	for i, it := range m.menu.items {
		if it.act.kind == actSaveAs {
			m.menu.idx = i
		}
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.overlay != overlaySaveAs || m.fx.overlay.Progress() == 1 {
		t.Fatalf("overlay %v at %v: a dialog from a menu should fade in", m.overlay, m.fx.overlay.Progress())
	}
}

func TestStatusArrivesAndLeaves(t *testing.T) {
	m := stageModel(true)
	m.setStatus(statusSuccess, "saved it")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if cmd == nil || !m.fx.status.Shown() {
		t.Fatal("status didn't start its presence")
	}
	settle(m)
	if !strings.Contains(ansi.Strip(footerText(m)), "✓ saved it") {
		t.Fatalf("footer: %q", ansi.Strip(footerText(m)))
	}
	m.Update(clearStatusMsg{id: m.statusID})
	if m.status != "" || m.fx.status.Shown() {
		t.Fatalf("status %q, shown %v", m.status, m.fx.status.Shown())
	}
	// It is half faded by now at the earliest, and the keys are not back yet.
	if got := ansi.Strip(footerText(m)); strings.Contains(got, "help") {
		t.Fatalf("key hints drawn while the status leaves: %q", got)
	}
	settle(m)
	if got := ansi.Strip(footerText(m)); strings.Contains(got, "saved it") || !strings.Contains(got, "help") {
		t.Fatalf("footer after the status left: %q", got)
	}

	// A new message while one is up starts over, so it reads as new.
	m.setStatus(statusInfo, "one")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	settle(m)
	m.setStatus(statusInfo, "two")
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.fx.status.Progress() == 1 || m.fx.statusText != "two" {
		t.Fatalf("second status at %v with %q", m.fx.status.Progress(), m.fx.statusText)
	}
}

// TestSidebarSlides checks the sidebar's width: it is drawn on its way
// there while the notebook is laid out for where it is heading, and keeps
// its content, but not its zones, until it has slid shut.
func TestSidebarSlides(t *testing.T) {
	m := stageModel(true)
	sidebar := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	m.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	zones := func() (n int) {
		for _, z := range m.zones {
			if z.act.kind == actSidebarPanel || z.act.kind == actSidebarItem {
				n++
			}
		}
		return n
	}
	if screenText(m); m.sidebarDrawn() != 0 || zones() != 0 {
		t.Fatal("closed sidebar is drawn")
	}

	_, cmd := m.Update(sidebar)
	full := m.sidebarWidth()
	if cmd == nil || full == 0 || !m.fx.side.Animating() || m.bodyWidth() != m.width-full {
		t.Fatalf("sidebar %d, body %d, sliding %v", full, m.bodyWidth(), m.fx.side.Animating())
	}
	m.fx.side.Advance(time.Now().Add(sidebarMotion / 4))
	if w := m.sidebarDrawn(); w <= 0 || w >= full {
		t.Fatalf("sidebar is %d wide on its way open, of %d", w, full)
	}
	// The notebook already has the width it ends up with, so nothing in it
	// is laid out again as the sidebar arrives.
	if text := screenText(m); !strings.Contains(text, "│ Con") || zones() == 0 {
		t.Fatalf("opening sidebar not drawn, or not clickable yet:\n%s", text)
	}
	m.fx.side.Advance(time.Now().Add(time.Minute))
	if w := m.sidebarDrawn(); w != full || !strings.Contains(screenText(m), "Contents  Variables") {
		t.Fatalf("sidebar is %d wide once open, want %d", w, full)
	}

	m.Update(sidebar)
	if m.sidebarWidth() != 0 || m.sidebarDrawn() != full {
		t.Fatalf("closing: width %d, drawn %d", m.sidebarWidth(), m.sidebarDrawn())
	}
	m.fx.side.Advance(time.Now().Add(sidebarMotion / 4))
	if w := m.sidebarDrawn(); w <= 0 || w >= full {
		t.Fatalf("sidebar is %d wide on its way shut, of %d", w, full)
	}
	if text := screenText(m); !strings.Contains(text, "│ Con") || zones() != 0 {
		t.Fatalf("closing sidebar lost its content, or is still clickable:\n%s", text)
	}
	m.fx.side.Advance(time.Now().Add(time.Minute))
	if text := screenText(m); m.sidebarDrawn() != 0 || strings.Contains(text, "Contents") {
		t.Fatalf("shut sidebar still drawn:\n%s", text)
	}

	// A sidebar that changes width with the terminal doesn't slide there.
	m.Update(sidebar)
	m.fx.side.Advance(time.Now().Add(time.Minute))
	m.Update(tea.WindowSizeMsg{Width: 140, Height: m.height})
	if w := m.sidebarWidth(); w == full || m.sidebarDrawn() != w || m.fx.side.Animating() {
		t.Fatalf("after a resize: width %d (was %d), drawn %d", w, full, m.sidebarDrawn())
	}
}

// TestSidebarUnsyncedDrawsAtOnce covers a sidebar toggled outside Update.
func TestSidebarUnsyncedDrawsAtOnce(t *testing.T) {
	m := stageModel(true)
	m.toggleSidebar()
	if m.sidebarDrawn() != m.sidebarWidth() || !strings.Contains(screenText(m), "Contents  Variables") {
		t.Fatal("sidebar not drawn at its width")
	}
}

// cellsOf parses a drawn string into w×h cells.
func cellsOf(s string, w, h int) *uv.ScreenBuffer {
	buf := uv.NewScreenBuffer(w, h)
	uv.NewStyledString(s).Draw(buf, buf.Bounds())
	return &buf
}

func fgOf(c *uv.Cell) string { return fmt.Sprint(c.Style.Fg) }

// TestTrailKeepsToBox checks the light on a running cell: only the box's
// border changes, a box cut off by the view is followed as far as it shows,
// and images are left alone.
func TestTrailKeepsToBox(t *testing.T) {
	const w, h = 30, 8
	row := lipgloss.NewStyle().Foreground(lipgloss.Color("#808080")).Render(strings.Repeat("x", w))
	grid := strings.TrimSuffix(strings.Repeat(row+"\n", h), "\n")
	plain := cellsOf(bloom.Render(w, h, bloom.Text(grid).NoWrap()), w, h)
	box := image.Rect(4, 1, 26, 6)
	lit := 0
	for _, phase := range []float64{0, 0.2, 0.45, 0.7, 0.95} {
		got := cellsOf(bloom.Render(w, h, bloom.Transition(phase, bloom.Text(grid).NoWrap(),
			trail{Rect: box, Color: lipgloss.Color("#ffffff")})), w, h)
		for y := range h {
			for x := range w {
				if fgOf(got.CellAt(x, y)) == fgOf(plain.CellAt(x, y)) {
					continue
				}
				lit++
				p := image.Pt(x, y)
				if !p.In(box) || p.In(box.Inset(1)) {
					t.Fatalf("phase %v lit %v, off the border of %v", phase, p, box)
				}
			}
		}
	}
	if lit == 0 {
		t.Fatal("no cell lit")
	}
	// A box that runs out of the view, and one too small to have a border.
	for _, r := range []image.Rectangle{image.Rect(4, -3, 40, 4), image.Rect(2, 2, 3, 3), {}} {
		bloom.Render(w, h, bloom.Transition(0.3, bloom.Text(grid).NoWrap(), trail{Rect: r, Color: lipgloss.Color("#ffffff")}))
	}
}

// TestEffectsKeepImages checks every effect that recolors what a Kitty
// image is drawn with: a placeholder's color is the image's ID.
func TestEffectsKeepImages(t *testing.T) {
	m := imageModel(t)
	lines, _, _ := m.renderOutputs(m.cells[0], 40, false, "")
	const w, h = 40, 6
	img := strings.Join(lines[1:1+h], "\n")
	plain := cellsOf(img, w, h)
	base := bloom.Text(img).NoWrap()
	dialog := bloom.Text("┌──────┐\n│ here │\n└──────┘").NoWrap()
	isImage := func(c *uv.Cell) bool { return strings.HasPrefix(c.Content, string(termimg.Placeholder)) }

	for name, tree := range map[string]bloom.Child{
		"backdrop and shadow": bloom.Overlay(base, bloom.Layer(dialog).Backdrop(backdrop).Shadow(2, 1, shadow)),
		"fading in":           bloom.Overlay(base, bloom.Layer(dialog).Transition(0.25, fade{})),
		"fading out":          bloom.Overlay(base, bloom.Layer(dialog).Backdrop(backdrop).Transition(0.75, fade{})),
		"trail":               bloom.Transition(0.1, base, trail{Rect: image.Rect(0, 0, w, h), Color: lipgloss.Color("#ffffff")}),
	} {
		// On a light terminal: the first image's ID is all but black, which
		// fading toward a dark background would leave as it is.
		ui := bloom.New(bloom.WithColors(lipgloss.Color("#000000"), lipgloss.Color("#ffffff")))
		got := cellsOf(ui.Render(w, h, tree), w, h)
		kept := 0
		for y := range h {
			for x := range w {
				c := got.CellAt(x, y)
				if !isImage(c) {
					continue
				}
				kept++
				if was := plain.CellAt(x, y); c.Content != was.Content || fgOf(c) != fgOf(was) {
					t.Fatalf("%s: image cell at %d,%d is now %q in %s, was %q in %s", name, x, y, c.Content, fgOf(c), was.Content, fgOf(was))
				}
			}
		}
		if kept == 0 {
			t.Fatalf("%s: no image cell left to check", name)
		}
	}
}

// TestBackdropSteps checks that a backdrop takes few strengths while its
// dialog opens: each one repaints the whole screen.
func TestBackdropSteps(t *testing.T) {
	text := lipgloss.NewStyle().Foreground(lipgloss.Color("#c0c0c0")).Render("backdrop")
	color := func(t float64) string {
		tree := bloom.Overlay(bloom.Text(text), bloom.Layer(bloom.Text("x")).Anchor(bloom.End, bloom.End).
			Backdrop(backdrop).Transition(t))
		return fgOf(cellsOf(bloom.Render(12, 3, tree), 12, 3).CellAt(0, 0))
	}
	seen := map[string]bool{}
	for i := range 101 {
		seen[color(float64(i)/100)] = true
	}
	if len(seen) != backdrop.Steps+1 {
		t.Fatalf("%d strengths while opening, want %d and none", len(seen), backdrop.Steps)
	}
	if color(0) != fgOf(cellsOf(text, 12, 3).CellAt(0, 0)) || color(0.99) != color(1) {
		t.Fatal("the backdrop should start from nothing and end at its full strength")
	}
}
