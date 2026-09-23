package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette based on the Go brand colors.
var (
	colGopher  = lipgloss.Color("#00ADD8")
	colSky     = lipgloss.Color("#5DC9E2")
	colFuchsia = lipgloss.Color("#CE3262")
	colYellow  = lipgloss.Color("#FDDD00")
	colPurple  = lipgloss.Color("#8B5CF6")
	colGreen   = lipgloss.Color("#4ADE80")
	colRed     = lipgloss.Color("#F87171")
	colOrange  = lipgloss.Color("#FB923C")

	colSelection = lipgloss.Color("#1F4E6B")

	colText   = lipgloss.Color("#E4E4E7")
	colDim    = lipgloss.Color("#A1A1AA")
	colMuted  = lipgloss.Color("#71717A")
	colSubtle = lipgloss.Color("#3F3F46")
	colFaint  = lipgloss.Color("#27272A")
	colInk    = lipgloss.Color("#101014")
)

type theme struct {
	text, dim, muted, subtle lipgloss.Style

	borderIdle, borderCmd, borderEdit, borderRun color.Color

	lineNo, lineNoActive lipgloss.Style

	stdout, stderr, result, info, errorText, errorBar lipgloss.Style
	resultLabel, inLabel                              lipgloss.Style

	modeCmd, modeEdit          lipgloss.Style
	helpKey, helpDesc, helpSep lipgloss.Style

	statusOK, statusErr, statusRun lipgloss.Style

	// Clickable elements.
	btn, btnHover, btnDanger, btnDisabled    lipgloss.Style
	dlgBtn, dlgBtnHover, dlgFocus, dlgDanger lipgloss.Style
	link, linkHover                          lipgloss.Style
}

func newTheme() theme {
	s := lipgloss.NewStyle
	return theme{
		text:   s().Foreground(colText),
		dim:    s().Foreground(colDim),
		muted:  s().Foreground(colMuted),
		subtle: s().Foreground(colSubtle),

		borderIdle: colSubtle,
		borderCmd:  colGopher,
		borderEdit: colGreen,
		borderRun:  colYellow,

		lineNo:       s().Foreground(colSubtle),
		lineNoActive: s().Foreground(colDim),

		stdout:      s().Foreground(colText),
		stderr:      s().Foreground(colOrange),
		result:      s().Foreground(colSky),
		info:        s().Foreground(colMuted).Italic(true),
		errorText:   s().Foreground(colRed),
		errorBar:    s().Foreground(colFuchsia),
		resultLabel: s().Foreground(colYellow),
		inLabel:     s().Foreground(colMuted),

		modeCmd:  s().Foreground(colInk).Background(colGopher).Bold(true).Padding(0, 1),
		modeEdit: s().Foreground(colInk).Background(colGreen).Bold(true).Padding(0, 1),
		helpKey:  s().Foreground(colDim).Bold(true),
		helpDesc: s().Foreground(colMuted),
		helpSep:  s().Foreground(colSubtle),

		statusOK:  s().Foreground(colGreen),
		statusErr: s().Foreground(colRed),
		statusRun: s().Foreground(colYellow),

		btn:         s().Foreground(colDim),
		btnHover:    s().Foreground(colInk).Background(colGopher).Bold(true),
		btnDanger:   s().Foreground(colInk).Background(colRed).Bold(true),
		btnDisabled: s().Foreground(colSubtle),
		dlgBtn:      s().Foreground(colDim).Background(colSubtle).Padding(0, 2),
		dlgBtnHover: s().Foreground(colText).Background(lipgloss.Color("#52525B")).Padding(0, 2),
		dlgFocus:    s().Foreground(colInk).Background(colGopher).Bold(true).Padding(0, 2),
		dlgDanger:   s().Foreground(colInk).Background(colRed).Bold(true).Padding(0, 2),
		link:        s().Foreground(colMuted),
		linkHover:   s().Foreground(colGopher).Underline(true),
	}
}
