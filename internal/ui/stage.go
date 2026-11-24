package ui

import (
	"image"
	"math"
	"time"

	"bloomfx.dev/bloom"
	"bloomfx.dev/bloom/anim"
	"bloomfx.dev/bloom/fx"
	tea "charm.land/bubbletea/v2"
)

// The screen is a bloom layout (https://bloomfx.dev): a column of the
// header, the toolbar, the notebook beside its scrollbar and the sidebar,
// and the footer, with popups and dialogs as layers over it. The parts are
// still rendered as strings, with their clickable zones, by the functions in
// view.go; bloom sizes and places them, composites the layers, and runs the
// effects and animations.

// IDs of the nodes that are found again in the frame bloom drew.
const (
	idOverlay    bloom.ID = "overlay"
	idInfo       bloom.ID = "info"
	idCompletion bloom.ID = "completion"
)

const (
	// overlayMotion is how long a dialog, picker or menu takes to open or
	// close, and statusMotion a status message in the footer.
	overlayMotion = 140 * time.Millisecond
	statusMotion  = 160 * time.Millisecond
	// sidebarMotion is how long the sidebar takes to slide open or closed.
	sidebarMotion = 180 * time.Millisecond
	// overlayRise is how many rows a dialog rises as it opens.
	overlayRise = 2
	// trailPeriod is how long the light on a running cell's border takes to
	// go around it.
	trailPeriod = 2400 * time.Millisecond
)

// backdrop dims the screen beneath a dialog, and shadow the cells beside it.
var (
	backdrop = dim{Amount: 0.5, Gray: 0.45, Steps: 3}
	shadow   = dim{Amount: 0.65, Steps: 3}
)

// layer is something drawn over the screen: a popup, a menu or a dialog.
type layer struct {
	content string
	// zones are the layer's clickable zones, relative to its top-left corner.
	zones []zone
	// at is the layer's top-left corner; a nil at centers it on the screen.
	at *image.Point
	// modal layers dim the screen beneath them.
	modal bool
}

// motion is what gopyter animates. Everything in it only changes how a state
// change is shown: the state itself (Model.overlay, Model.status, the
// sidebar) is set as before, and syncMotion starts the animations after the
// fact. With motion off none of it is used.
type motion struct {
	on bool

	// side is the width the sidebar is drawn at, on its way to
	// Model.sidebarWidth, and size the terminal's size when it was last
	// set: a sidebar that changes width with the terminal doesn't slide.
	side anim.Value
	size image.Point

	// overlay fades dialogs, pickers and menus in and out. kind is the
	// overlay it was last started for; the overlay is drawn with it only
	// while that still matches Model.overlay.
	overlay anim.Presence
	kind    overlay
	// ghost is the overlay as last drawn, and ghostAt where: it is shown
	// while the overlay animates out, after Model.overlay was cleared.
	ghost   layer
	ghostAt image.Point

	// status brings a status message into the footer and takes it out again.
	// statusID is the Model.statusID it was last started for, and the text
	// and kind are those last shown, for while the message animates out.
	status     anim.Presence
	statusID   int
	statusText string
	statusKind statusKind

	// termColors is set once the terminal reported its own colors, which
	// the effects that blend toward the background then use instead of the
	// theme's. retheme is set when the theme changed since bloom was last
	// given its colors.
	termColors, retheme bool
}

func newPresence(d time.Duration) anim.Presence { return anim.NewPresence(anim.Duration(d)) }

// initStage makes the bloom root. The palette must be set already.
func (m *Model) initStage(on bool) {
	m.ui = bloom.New(bloom.WithColors(colText, colInk))
	m.fx.on = on
	m.fx.overlay = newPresence(overlayMotion)
	m.fx.status = newPresence(statusMotion)
	m.fx.side = anim.NewValue(0, anim.Duration(sidebarMotion))
}

// themeStage notes that the theme changed. bloom is given the theme's
// colors to blend with, for a terminal that never reports its own, with the
// next message (see animate): its Update may return the command of an
// animation frame, which applyTheme has no way to hand back.
func (m *Model) themeStage() { m.fx.retheme = true }

// animate passes msg to bloom and to the animations, and starts the
// animations for what the message changed. Every message must come through
// here, as the frames of a running animation are messages too.
func (m *Model) animate(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(tea.BackgroundColorMsg); ok {
		m.fx.termColors = true
	}
	cmds := []tea.Cmd{m.ui.Update(msg)}
	if m.fx.retheme {
		m.fx.retheme = false
		// gopyter draws on the terminal's background, so once that is known
		// it is the one to fade toward, whatever the theme.
		if !m.fx.termColors {
			cmds = append(cmds,
				m.ui.Update(tea.ForegroundColorMsg{Color: colText}),
				m.ui.Update(tea.BackgroundColorMsg{Color: colInk}))
		}
	}
	if m.fx.on {
		cmds = append(cmds, anim.Update(msg, &m.fx.overlay, &m.fx.status, &m.fx.side), m.syncMotion())
	}
	return tea.Batch(cmds...)
}

// syncMotion starts the animations for the state Update left the model in.
// Overlays and status messages are opened and closed all over the UI by
// setting Model.overlay and Model.status; watching for the change here means
// none of those places needs to know about motion.
func (m *Model) syncMotion() tea.Cmd {
	var cmds []tea.Cmd
	if was := m.fx.kind; m.overlay != was {
		m.fx.kind = m.overlay
		switch {
		case m.overlay == overlayNone:
			cmds = append(cmds, m.fx.overlay.Hide())
		case was == overlayNone && m.fx.overlay.Visible():
			// Reopened while still closing: carry on from where it is.
			cmds = append(cmds, m.fx.overlay.Show())
		case was == overlayNone || was == overlayMenu || m.overlay == overlayMenu:
			m.fx.overlay = newPresence(overlayMotion)
			cmds = append(cmds, m.fx.overlay.Show())
		default:
			// One dialog replaced another: the screen stays dimmed, and
			// the new one takes the old one's place.
		}
	}
	switch {
	case m.status == "":
		if m.fx.status.Shown() {
			cmds = append(cmds, m.fx.status.Hide())
		}
	case m.statusID != m.fx.statusID || !m.fx.status.Shown():
		m.fx.statusID = m.statusID
		m.fx.statusText, m.fx.statusKind = m.status, m.statusKind
		m.fx.status = newPresence(statusMotion)
		cmds = append(cmds, m.fx.status.Show())
	}
	size := image.Pt(m.width, m.height)
	switch width := float64(m.sidebarWidth()); {
	case size != m.fx.size:
		m.fx.size = size
		m.fx.side.Set(width)
	case width != m.fx.side.Target():
		cmds = append(cmds, m.fx.side.To(width))
	}
	return tea.Batch(cmds...)
}

// sidebarDrawn is the width the sidebar is drawn at: the one it has, or one
// on the way there while it slides open or shut. The notebook is laid out
// for the width it is heading for from the start (see Model.sidebarWidth).
func (m *Model) sidebarDrawn() int {
	width := m.sidebarWidth()
	if m.fx.on && m.fx.side.Target() == float64(width) {
		return m.fx.side.Int()
	}
	return width
}

// overlayMoving reports whether the overlay presence animates the overlay
// that is open, or, with none open, the one that is closing.
func (m *Model) overlayMoving() bool {
	return m.fx.on && m.fx.kind == m.overlay && m.fx.overlay.Visible()
}

// statusMoving reports whether the status presence animates the message in
// the footer: the one set, or the last one on its way out.
func (m *Model) statusMoving() bool {
	return m.fx.on && m.fx.status.Visible() && (m.status == "" || m.fx.statusID == m.statusID)
}

// rise is how far below its place the overlay is drawn while it opens.
func (m *Model) rise() int {
	return int(math.Round((1 - m.fx.overlay.Progress()) * overlayRise))
}

// popupNode places a popup that takes no part in motion.
func popupNode(l layer, id bloom.ID) bloom.LayerNode {
	return bloom.Layer(bloom.Text(l.content).NoWrap()).ID(id).
		Anchor(bloom.Start, bloom.Start).Offset(l.at.X, l.at.Y)
}

// overlayNode places a dialog, picker or menu, dimming the screen beneath a
// modal one. moving draws it with the overlay presence.
func (m *Model) overlayNode(l layer, moving bool) bloom.LayerNode {
	n := bloom.Layer(bloom.Text(l.content).NoWrap()).ID(idOverlay)
	dy := 0
	if moving && l.modal {
		dy = m.rise()
	}
	switch {
	case l.at != nil:
		n = n.Anchor(bloom.Start, bloom.Start).Offset(l.at.X, l.at.Y+dy)
	case dy != 0:
		n = n.Offset(0, dy)
	}
	if l.modal {
		n = n.Backdrop(backdrop)
	}
	n = n.Shadow(2, 1, shadow)
	if moving {
		n = n.Presence(m.fx.overlay, effects(fade{})...)
	}
	return n
}

// effects is a list of effects, for a call that takes them variadically.
func effects(e ...fx.Effect) []fx.Effect { return e }

// screen is what a frame is made of.
type screen struct {
	header, footer bloom.Child
	toolbar        string
	cells          string
	// total, visible and offset are the scrollbar's: lines of the notebook,
	// lines in view, and the first line in view.
	total, visible, offset int
	sidebar                string
	// sidebarW is the width the sidebar is drawn at (see sidebarDrawn).
	sidebarW int
	// running is the box of the running cell within cells, if it is in view.
	running image.Rectangle
	started time.Time

	info, completion, overlay *layer
}

// tree builds the bloom layout of s.
func (m *Model) tree(s screen) bloom.Child {
	base := bloom.Column(
		bloom.Fixed(1, s.header),
		bloom.Fixed(1, bloom.Text(s.toolbar).NoWrap()),
		bloom.Fill(m.body(s)),
		bloom.Fixed(1, s.footer),
	)

	var layers []bloom.LayerNode
	if s.info != nil {
		layers = append(layers, popupNode(*s.info, idInfo))
	}
	if s.completion != nil {
		layers = append(layers, popupNode(*s.completion, idCompletion))
	}
	switch {
	case s.overlay != nil:
		layers = append(layers, m.overlayNode(*s.overlay, m.overlayMoving()))
	case m.overlay == overlayNone && m.overlayMoving() && m.fx.ghost.content != "":
		g := m.fx.ghost
		g.at, g.zones = &m.fx.ghostAt, nil
		layers = append(layers, m.overlayNode(g, true))
	}
	return bloom.Overlay(base, layers...)
}

// body lays out the notebook: the cells, which take the width that is
// left, the scrollbar, and the sidebar at the width it has slid to.
func (m *Model) body(s screen) bloom.Child {
	cells := bloom.Child(bloom.Text(s.cells).NoWrap())
	if m.fx.on && !s.running.Empty() {
		// The light is placed by the clock, and moves with the frames the
		// spinner of a running cell already asks for.
		_, phase := math.Modf(float64(time.Since(s.started)) / float64(trailPeriod))
		cells = bloom.Transition(phase, cells, trail{Rect: s.running, Color: colText})
	}

	var side bloom.Child
	if s.sidebar != "" {
		side = bloom.Text(s.sidebar).NoWrap()
	}
	return bloom.Row(
		bloom.Fill(cells),
		bloom.Scrollbar(s.total, s.visible, s.offset).Style(bloom.ScrollbarStyle{
			ThumbStyle: m.theme.muted,
			Track:      "│",
			TrackStyle: m.theme.faint,
		}),
		bloom.Fixed(s.sidebarW, side),
	)
}
