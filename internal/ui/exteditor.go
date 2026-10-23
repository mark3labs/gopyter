package ui

import (
	"os"
	"os/exec"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// editorDoneMsg reports that the external editor exited.
type editorDoneMsg struct{ err error }

// editorCommand prefers VISUAL, then EDITOR, then vi. Arguments are space-separated.
func editorCommand(getenv func(string) string, path string) *exec.Cmd {
	args := strings.Fields(getenv("VISUAL"))
	if len(args) == 0 {
		args = strings.Fields(getenv("EDITOR"))
	}
	if len(args) == 0 {
		args = []string{"vi"}
	}
	return exec.Command(args[0], append(args[1:], path)...)
}

// startExternalEdit suspends the UI while the editor works on a private source file.
func (m *Model) startExternalEdit(i int) tea.Cmd {
	if m.extEdit.path != "" {
		return m.setStatus(statusInfo, "external edit already pending")
	}
	c := m.cells[i]
	ext := ".go"
	if c.kind == notebook.Markdown {
		ext = ".md"
	}
	if c.kind == notebook.Raw {
		ext = ".txt"
	}
	f, err := os.CreateTemp("", "gopyter-*"+ext)
	if err != nil {
		return m.setStatus(statusError, "editor: %v", err)
	}
	path := f.Name()
	_, writeErr := f.WriteString(c.ed.Value())
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path) // Best-effort cleanup of an unusable temporary file.
		if writeErr != nil {
			err = writeErr
		} else {
			err = closeErr
		}
		return m.setStatus(statusError, "editor: %v", err)
	}
	m.extEdit.cellID, m.extEdit.path, m.extEdit.orig = c.id, path, c.ed.Value()
	return tea.ExecProcess(editorCommand(os.Getenv, path), func(err error) tea.Msg { return editorDoneMsg{err} })
}

// finishExternalEdit applies the saved source as one undo step, without running it.
func (m *Model) finishExternalEdit(msg editorDoneMsg) tea.Cmd {
	edit := m.extEdit
	m.extEdit = struct{ cellID, path, orig string }{}
	if edit.path == "" {
		return nil
	}
	if msg.err != nil {
		_ = os.Remove(edit.path) // A failed editor must not change the cell.
		return m.setStatus(statusError, "editor: %v", msg.err)
	}
	_, c := m.cellByID(edit.cellID)
	if c == nil {
		return m.setStatus(statusError, "cell removed · edited source kept at %s", edit.path)
	}
	data, err := os.ReadFile(edit.path)
	if err != nil {
		return m.setStatus(statusError, "editor: %v · source kept at %s", err, edit.path)
	}
	_ = os.Remove(edit.path) // Failure only leaves a stray temporary file.
	src := string(data)
	if !strings.HasSuffix(edit.orig, "\n") {
		src = strings.TrimSuffix(src, "\n")
	}
	if src == edit.orig {
		return m.setStatus(statusInfo, "no changes")
	}
	c.ed.SetValue(src)
	c.ed.breakUndo()
	c.mdOut = ""
	m.dirty = true
	return m.setStatus(statusSuccess, "cell updated from editor")
}
