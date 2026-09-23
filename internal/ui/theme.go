package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// The active palette. These are package-level so that every renderer, including
// ones without access to the Model (editor, highlighter, completion popup),
// shares one source of truth. They are only reassigned by setPalette, which
// runs on the Bubble Tea goroutine (at startup and from the theme picker),
// the same goroutine that renders, so they need no locking.
var (
	colPrimary  color.Color // selection, focus, links: Go gopher blue by default
	colInfo     color.Color // results, tooltips
	colAccent   color.Color // logo gradient end, vim visual mode
	colSuccess  color.Color
	colWarning  color.Color // running state, section titles
	colError    color.Color
	colErrorBar color.Color // gutter bar beside error output
	colOrange   color.Color // stderr, interrupted, vim insert mode

	colSelection color.Color // text selection and highlighted list rows
	colRaised    color.Color // hovered dialog buttons

	colText   color.Color
	colDim    color.Color
	colMuted  color.Color
	colSubtle color.Color
	colFaint  color.Color
	colInk    color.Color // text drawn on colored backgrounds; the theme background
)

func init() { setPalette(defaultTheme().palette(true)) }

// palette is a resolved set of hex colors a UI theme is built from.
type palette struct {
	primary, info, accent, success, warning, error, errorBar, orange string
	selection, raised                                                string
	text, dim, muted, subtle, faint, ink                             string

	// Syntax highlighting: syntax names a chroma style; when empty a style
	// is derived from the keyword/str/number/comment/name colors.
	syntax                              string
	keyword, str, number, comment, name string

	// markdown names a glamour standard style; when empty one is derived
	// from the palette.
	markdown string
}

// setPalette makes p the active palette.
func setPalette(p palette) {
	c := lipgloss.Color
	colPrimary, colInfo, colAccent = c(p.primary), c(p.info), c(p.accent)
	colSuccess, colWarning, colError = c(p.success), c(p.warning), c(p.error)
	colErrorBar, colOrange = c(p.errorBar), c(p.orange)
	colSelection, colRaised = c(p.selection), c(p.raised)
	colText, colDim, colMuted = c(p.text), c(p.dim), c(p.muted)
	colSubtle, colFaint, colInk = c(p.subtle), c(p.faint), c(p.ink)
}

type theme struct {
	text, dim, muted, subtle lipgloss.Style

	borderIdle, borderCmd, borderEdit, borderRun color.Color

	lineNo, lineNoActive lipgloss.Style

	stdout, stderr, result, info, errorText, errorBar lipgloss.Style
	resultLabel, inLabel                              lipgloss.Style

	modeCmd, modeEdit          lipgloss.Style
	modeInsert, modeVisual     lipgloss.Style
	helpKey, helpDesc, helpSep lipgloss.Style

	statusOK, statusErr, statusRun lipgloss.Style

	// Clickable elements.
	btn, btnHover, btnDanger, btnDisabled    lipgloss.Style
	dlgBtn, dlgBtnHover, dlgFocus, dlgDanger lipgloss.Style
	link, linkHover                          lipgloss.Style
}

// newTheme builds the styles from the active palette.
func newTheme() theme {
	s := lipgloss.NewStyle
	return theme{
		text:   s().Foreground(colText),
		dim:    s().Foreground(colDim),
		muted:  s().Foreground(colMuted),
		subtle: s().Foreground(colSubtle),

		borderIdle: colSubtle,
		borderCmd:  colPrimary,
		borderEdit: colSuccess,
		borderRun:  colWarning,

		lineNo:       s().Foreground(colSubtle),
		lineNoActive: s().Foreground(colDim),

		stdout:      s().Foreground(colText),
		stderr:      s().Foreground(colOrange),
		result:      s().Foreground(colInfo),
		info:        s().Foreground(colMuted).Italic(true),
		errorText:   s().Foreground(colError),
		errorBar:    s().Foreground(colErrorBar),
		resultLabel: s().Foreground(colWarning),
		inLabel:     s().Foreground(colMuted),

		modeCmd:  s().Foreground(colInk).Background(colPrimary).Bold(true).Padding(0, 1),
		modeEdit: s().Foreground(colInk).Background(colSuccess).Bold(true).Padding(0, 1),

		modeInsert: s().Foreground(colInk).Background(colOrange).Bold(true).Padding(0, 1),
		modeVisual: s().Foreground(colInk).Background(colAccent).Bold(true).Padding(0, 1),

		helpKey:  s().Foreground(colDim).Bold(true),
		helpDesc: s().Foreground(colMuted),
		helpSep:  s().Foreground(colSubtle),

		statusOK:  s().Foreground(colSuccess),
		statusErr: s().Foreground(colError),
		statusRun: s().Foreground(colWarning),

		btn:         s().Foreground(colDim),
		btnHover:    s().Foreground(colInk).Background(colPrimary).Bold(true),
		btnDanger:   s().Foreground(colInk).Background(colError).Bold(true),
		btnDisabled: s().Foreground(colSubtle),
		dlgBtn:      s().Foreground(colDim).Background(colSubtle).Padding(0, 2),
		dlgBtnHover: s().Foreground(colText).Background(colRaised).Padding(0, 2),
		dlgFocus:    s().Foreground(colInk).Background(colPrimary).Bold(true).Padding(0, 2),
		dlgDanger:   s().Foreground(colInk).Background(colError).Bold(true).Padding(0, 2),
		link:        s().Foreground(colMuted),
		linkHover:   s().Foreground(colPrimary).Underline(true),
	}
}
