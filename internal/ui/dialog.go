package ui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Dialogs have a focus ring made of their buttons and, for the save-as and
// ask dialogs, the text input. dlgFocus indexes the buttons; focusInput
// (-1) means the text input has focus.
const focusInput = -1

type dlgButton struct {
	act    action
	label  string
	tip    string
	danger bool
}

// dialogButtons returns the buttons of the active dialog. It is the single
// source of truth for both rendering and keyboard navigation.
func (m *Model) dialogButtons() []dlgButton {
	switch m.overlay {
	case overlayQuit:
		return []dlgButton{
			{action{kind: actDialogYes}, "Save & quit", "save and quit · y", false},
			{action{kind: actDialogNo}, "Discard", "quit without saving · n", true},
			{action{kind: actDialogCancel}, "Cancel", "keep editing · esc", false},
		}
	case overlaySaveAs:
		return []dlgButton{
			{action{kind: actDialogConfirm}, "Save", "save", false},
			{action{kind: actDialogCancel}, "Cancel", "cancel · esc", false},
		}
	case overlayReload:
		return []dlgButton{
			{action{kind: actReload}, "Reload", "load the file, discarding your changes · r", true},
			{action{kind: actReloadKeep}, "Keep mine", "keep your version; saving overwrites the file · esc", false},
		}
	case overlayReview:
		if m.task.err != "" {
			return []dlgButton{{action{kind: actReviewDiscard}, "Close", "close · esc", false}}
		}
		return []dlgButton{
			{action{kind: actReviewApply}, "Apply", "replace the cell with the " + m.task.noun(), false},
			{action{kind: actReviewDiscard}, "Discard", "keep the cell as it is · esc", false},
		}
	case overlayAsk:
		return []dlgButton{
			{action{kind: actAskSend}, "Ask", "send the request to " + m.assistant.Model(), false},
			{action{kind: actDialogCancel}, "Cancel", "cancel · esc", false},
		}
	}
	return nil
}

func (m *Model) dialogHasInput() bool { return m.overlay == overlaySaveAs || m.overlay == overlayAsk }

// submitInput confirms the dialog's text input.
func (m *Model) submitInput() tea.Cmd {
	if m.overlay == overlayAsk {
		return m.sendAsk()
	}
	return m.saveAsConfirm()
}

// openDialog shows a dialog with the default element focused.
func (m *Model) openDialog(o overlay) tea.Cmd {
	m.overlay = o
	m.dlgFocus = 0
	if m.dialogHasInput() {
		return m.setDialogFocus(focusInput)
	}
	return nil
}

// setDialogFocus moves focus, keeping the text input's cursor in sync.
func (m *Model) setDialogFocus(i int) tea.Cmd {
	m.dlgFocus = i
	if i == focusInput {
		return m.input.Focus()
	}
	m.input.Blur()
	return nil
}

// cycleDialogFocus moves focus by d positions around the ring, wrapping.
func (m *Model) cycleDialogFocus(d int) tea.Cmd {
	n := len(m.dialogButtons())
	first := 0
	if m.dialogHasInput() {
		first = focusInput
	}
	size := n - first
	pos := (m.dlgFocus - first + d%size + size) % size
	return m.setDialogFocus(pos + first)
}

// handleDialogKey handles keys for the quit and save-as dialogs.
func (m *Model) handleDialogKey(msg tea.KeyPressMsg) tea.Cmd {
	buttons := m.dialogButtons()
	onInput := m.dialogHasInput() && m.dlgFocus == focusInput
	ks := msg.String()

	switch ks {
	case "tab":
		return m.cycleDialogFocus(1)
	case "shift+tab":
		return m.cycleDialogFocus(-1)
	case "esc":
		return m.dialogCancel()
	case "enter":
		if onInput {
			return m.submitInput()
		}
		return m.doAction(buttons[m.dlgFocus].act)
	}

	if onInput {
		// The input keeps left/right for cursor movement; down jumps to
		// the buttons.
		if ks == "down" {
			return m.setDialogFocus(0)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return cmd
	}

	switch ks {
	case "right", "l":
		return m.cycleDialogFocus(1)
	case "left", "h":
		return m.cycleDialogFocus(-1)
	case "space":
		return m.doAction(buttons[m.dlgFocus].act)
	case "up":
		if m.dialogHasInput() {
			return m.setDialogFocus(focusInput)
		}
	}

	switch m.overlay {
	case overlayQuit:
		switch ks {
		case "y", "Y":
			return m.dialogYes()
		case "n", "N":
			return m.quit()
		case "q", "ctrl+c":
			return m.dialogCancel()
		}
	case overlayReload:
		switch ks {
		case "r", "R":
			return m.reloadConfirm()
		case "k", "K":
			return m.reloadKeep()
		}
	case overlayReview:
		switch ks {
		case "up", "k":
			m.scrollReview(-1)
		case "down", "j":
			m.scrollReview(1)
		case "pgup", "ctrl+u":
			m.scrollReview(-max(m.task.rows-1, 1))
		case "pgdown", "ctrl+d":
			m.scrollReview(max(m.task.rows-1, 1))
		}
	case overlaySaveAs, overlayAsk:
		// Typing while a button is focused goes back to the input.
		if msg.Text != "" && msg.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			cmd := m.setDialogFocus(focusInput)
			var icmd tea.Cmd
			m.input, icmd = m.input.Update(msg)
			return tea.Batch(cmd, icmd)
		}
	}
	return nil
}

// dialogYes saves (asking for a filename if needed) and quits.
func (m *Model) dialogYes() tea.Cmd {
	if m.path == "" {
		m.quitAfter = true
		return m.openSaveAs()
	}
	if err := m.save(); err != nil {
		m.overlay = overlayNone
		return m.setStatus(statusError, "save failed: %v", err)
	}
	return m.quit()
}

func (m *Model) dialogCancel() tea.Cmd {
	if m.overlay == overlayReload {
		// Dismissing the reload dialog keeps the local version.
		return m.reloadKeep()
	}
	if m.overlay == overlayReview {
		m.discardProposal()
		return nil
	}
	if m.overlay == overlayAsk {
		// Keep what was typed for the next time the dialog opens.
		m.ask.draft = m.input.Value()
	}
	m.overlay = overlayNone
	m.quitAfter = false
	m.input.Blur()
	return nil
}

// saveAsConfirm saves under the typed filename.
func (m *Model) saveAsConfirm() tea.Cmd {
	p := strings.TrimSpace(m.input.Value())
	if p == "" {
		return m.setDialogFocus(focusInput)
	}
	if filepath.Ext(p) == "" {
		p += ".ipynb"
	}
	m.path = p
	m.overlay = overlayNone
	m.input.Blur()
	if err := m.save(); err != nil {
		m.quitAfter = false
		// Watch the new path from its current state, not the old file's.
		m.snapshotDisk()
		return m.setStatus(statusError, "save failed: %v", err)
	}
	if m.quitAfter {
		return m.quit()
	}
	return m.setStatus(statusSuccess, "saved %s", m.path)
}
