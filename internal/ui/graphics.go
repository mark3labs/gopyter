package ui

import "github.com/mark3labs/gopyter/internal/termimg"

// graphicsMsg transfers commands queued by View to Update's raw output path.
// Program.Send runs asynchronously because it waits for the event loop.
type graphicsMsg struct{}

func (m *Model) imageKeys() []termimg.Key {
	var keys []termimg.Key
	for _, c := range m.cells {
		keys = append(keys, c.images...)
	}
	for _, u := range m.undo {
		for _, c := range u.cells {
			keys = append(keys, c.images...)
		}
		keys = append(keys, u.images...)
	}
	return keys
}

func (m *Model) sweepImages() {
	if m.gfx == nil {
		return
	}
	m.gfx.Retain(m.imageKeys())
	if m.gfx.Pending() && m.send != nil && !m.graphicsQueued {
		m.graphicsQueued = true
		go m.send(graphicsMsg{})
	}
}
