package ui

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"os"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// watchInterval is how often the notebook file is checked for external
// changes. Polling is used instead of fsnotify: it needs no dependency and
// handles editors (and our own Save) that replace the file via rename,
// which breaks watches on the file itself.
const watchInterval = time.Second

// diskState fingerprints the notebook file as last seen on disk.
type diskState struct {
	known   bool // the file has been looked at
	exists  bool
	modTime time.Time
	size    int64
	hash    [sha256.Size]byte
}

// watchState tracks the notebook file for changes made outside gopyter.
type watchState struct {
	state diskState
	// gen is bumped whenever we write or adopt a file version, so poll
	// results read before that are discarded.
	gen int
	// pending is an external version waiting on the reload dialog.
	pending      *notebook.Notebook
	pendingState diskState
}

type watchTickMsg struct{}

type diskPollMsg struct {
	gen   int
	path  string
	state diskState
	nb    *notebook.Notebook // set when the content changed
	err   error              // unreadable or unparsable; retried next tick
}

// readDisk stats and reads path, returning its fingerprint and content.
// A missing file is not an error.
func readDisk(path string) (diskState, []byte, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return diskState{known: true}, nil, nil
	}
	if err != nil {
		return diskState{}, nil, err
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return diskState{known: true}, nil, nil
	}
	if err != nil {
		return diskState{}, nil, err
	}
	return diskState{known: true, exists: true, modTime: fi.ModTime(), size: fi.Size(), hash: sha256.Sum256(b)}, b, nil
}

// snapshotDisk records the file's current state as ours.
func (m *Model) snapshotDisk() {
	m.watch.gen++
	m.watch.state = diskState{}
	if m.path == "" {
		return
	}
	if st, _, err := readDisk(m.path); err == nil {
		m.watch.state = st
	}
}

func watchTick() tea.Cmd {
	return tea.Tick(watchInterval, func(time.Time) tea.Msg { return watchTickMsg{} })
}

// canReload reports whether an external change may be applied now. While
// cells run (their outputs target the current cells) or a dialog or menu
// is open, changes wait for a later tick.
func (m *Model) canReload() bool {
	return m.path != "" && !m.busy() && m.overlay == overlayNone
}

func (m *Model) handleWatchTick() tea.Cmd {
	if !m.canReload() {
		return watchTick()
	}
	return pollDisk(m.path, m.watch.state, m.watch.gen)
}

// pollDisk checks the file off the UI goroutine. The content is only read
// when the size or modification time moved, and only parsed when its hash
// differs from the known one.
func pollDisk(path string, known diskState, gen int) tea.Cmd {
	return func() tea.Msg {
		msg := diskPollMsg{gen: gen, path: path}
		if known.exists {
			fi, err := os.Stat(path)
			if err == nil && fi.ModTime().Equal(known.modTime) && fi.Size() == known.size {
				msg.state = known
				return msg
			}
		}
		st, b, err := readDisk(path)
		if err != nil {
			msg.err = err
			return msg
		}
		msg.state = st
		if !st.exists || (known.exists && st.hash == known.hash) {
			return msg
		}
		// A writer may be mid-way through the file; a parse error is
		// retried on the next tick.
		msg.nb, msg.err = notebook.Parse(b)
		return msg
	}
}

func (m *Model) handleDiskPoll(msg diskPollMsg) tea.Cmd {
	next := watchTick()
	if msg.gen != m.watch.gen || msg.path != m.path || msg.err != nil || !m.canReload() {
		return next
	}
	old := m.watch.state
	switch {
	case !msg.state.exists:
		m.watch.state = msg.state
		if old.exists {
			// Keep the cells; mark them unsaved so quitting offers to
			// write them back.
			m.dirty = true
			return tea.Batch(next, m.setStatus(statusError, "%s was deleted or moved · save to recreate it", m.path))
		}
	case msg.nb == nil:
		// Same content (or just touched): adopt the new stat.
		m.watch.state = msg.state
	case m.reloadConflicts(msg.nb):
		m.watch.pending, m.watch.pendingState = msg.nb, msg.state
		return tea.Batch(next, m.openDialog(overlayReload))
	default:
		return tea.Batch(next, m.applyReload(msg.nb, msg.state))
	}
	return next
}

// matchCells pairs each cell of nb with the existing cell it updates, or
// nil for new cells. Cells match by ID; files without IDs get fresh random
// ones on every parse, so an unmatched cell also matches the old cell at
// the same position if its kind and source are unchanged.
func (m *Model) matchCells(nb *notebook.Notebook) []*Cell {
	byID := make(map[string]*Cell, len(m.cells))
	for _, c := range m.cells {
		byID[c.id] = c
	}
	used := make(map[*Cell]bool, len(m.cells))
	out := make([]*Cell, len(nb.Cells))
	for i, nc := range nb.Cells {
		c := byID[nc.ID]
		if c == nil && i < len(m.cells) {
			if o := m.cells[i]; o.kind == nc.Type && o.ed.Value() == nc.Source {
				c = o
			}
		}
		if c != nil && !used[c] {
			used[c] = true
			out[i] = c
		}
	}
	return out
}

// reloadConflicts reports whether reloading nb would lose work: unsaved
// changes, or replacing the text of the cell being edited.
func (m *Model) reloadConflicts(nb *notebook.Notebook) bool {
	if m.dirty {
		return true
	}
	if m.mode != modeEdit {
		return false
	}
	cur := m.cur()
	for i, c := range m.matchCells(nb) {
		if c == cur {
			return nb.Cells[i].Type != cur.kind || nb.Cells[i].Source != cur.ed.Value()
		}
	}
	return true // the edited cell was removed
}

// applyReload replaces the cells with nb, reusing matching cells so their
// undo history and render caches survive, and keeps the selection on the
// same cell where possible.
func (m *Model) applyReload(nb *notebook.Notebook, st diskState) tea.Cmd {
	matched := m.matchCells(nb)
	cur := m.cur()
	m.closeCompletion()
	if m.mode == modeEdit && !slices.Contains(matched, cur) {
		m.leaveEdit()
	}

	cells := make([]*Cell, 0, len(nb.Cells))
	for i, nc := range nb.Cells {
		c := matched[i]
		if c == nil {
			c = fromNotebook(nc)
		} else {
			c.reload(nc)
		}
		cells = append(cells, c)
		m.counter = max(m.counter, nc.ExecutionCount)
	}
	if len(cells) == 0 {
		cells = []*Cell{newCell(notebook.Code, "")}
	}
	m.cells = cells
	m.meta = nb.Metadata
	if i := slices.Index(cells, cur); i >= 0 {
		m.sel = i
	} else {
		m.sel = clamp(m.sel, 0, len(cells)-1)
	}
	if m.mode == modeEdit && m.vimActive() {
		m.cur().ed.clampNormal()
	}
	m.hoverCell = -1
	m.drag = dragState{}

	m.dirty = false
	m.watch.state = st
	m.watch.gen++
	m.watch.pending = nil
	// Declarations from cells already run stay in the kernel.
	return m.setStatus(statusInfo, "reloaded %s from disk · kernel state kept", m.path)
}

// reload updates the cell in place from its on-disk version.
func (c *Cell) reload(nc *notebook.Cell) {
	if c.kind != nc.Type {
		c.setKind(nc.Type)
	}
	if c.ed.Value() != nc.Source {
		// Undoable, so an unwanted external change can be reverted.
		c.ed.SetValue(nc.Source)
		c.ed.breakUndo()
	}
	c.metadata = nc.Metadata
	if c.count != nc.ExecutionCount || !slices.Equal(c.outputs, nc.Outputs) {
		c.outputs, c.count = nc.Outputs, nc.ExecutionCount
		c.outRev++
		c.status, c.errMsg, c.duration = diskStatus(nc), "", 0
		c.outKey, c.outLines = outputKey{}, nil
	}
}

// reloadConfirm discards local changes in favour of the pending version.
func (m *Model) reloadConfirm() tea.Cmd {
	m.overlay = overlayNone
	nb, st := m.watch.pending, m.watch.pendingState
	if nb == nil {
		return nil
	}
	return m.applyReload(nb, st)
}

// reloadKeep keeps the local version. The on-disk version is adopted as
// seen so we don't ask again; the next save overwrites it.
func (m *Model) reloadKeep() tea.Cmd {
	m.overlay = overlayNone
	if m.watch.pending != nil {
		m.watch.state = m.watch.pendingState
		m.watch.gen++
		m.watch.pending = nil
		m.dirty = true
	}
	return m.setStatus(statusInfo, "kept your version · saving will overwrite %s", m.path)
}
