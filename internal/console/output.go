package console

import (
	"context"
	"io"
	"strings"
	"sync"
)

// lineWriter leaves only the unfinished tail in the live view. Complete lines
// are drained in order into scrollback; cancellation unblocks a full queue.
//
// A carriage return takes effect when more text follows, like a terminal:
// "50%\r" stays visible until it's overwritten, and "\r\n" ends the line.
// Escape sequences print nothing, so the styles the runner wraps around
// each line ("50%\r\x1b[m") don't overwrite it either.
type lineWriter struct {
	mu   sync.Mutex
	tail string
	cr   bool   // a \r is pending: the next text replaces tail
	esc  string // escape sequences since the pending \r
	seq  []byte // an unfinished escape sequence
	ch   chan string
	ctx  context.Context
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var lines []string
	for _, r := range p {
		if len(w.seq) > 0 || r == 0x1b {
			w.seq = append(w.seq, r)
			if !seqDone(w.seq) {
				continue
			}
			if w.cr {
				w.esc += string(w.seq)
			} else {
				w.tail += string(w.seq)
			}
			w.seq = w.seq[:0]
			continue
		}
		switch r {
		case '\r':
			w.cr = true
		case '\n':
			lines = append(lines, w.tail+w.esc)
			w.tail, w.esc, w.cr = "", "", false
		default:
			if w.cr {
				w.tail, w.esc, w.cr = w.esc, "", false
			}
			w.tail += string([]byte{r})
		}
	}
	w.mu.Unlock()
	for _, line := range lines {
		select {
		case w.ch <- line:
		case <-w.ctx.Done():
			return len(p), nil
		}
	}
	return len(p), nil
}
func (w *lineWriter) partial() string { w.mu.Lock(); defer w.mu.Unlock(); return w.tail + w.esc }
func (w *lineWriter) flush() {
	w.mu.Lock()
	tail := w.tail + w.esc
	w.tail, w.esc, w.cr = "", "", false
	w.mu.Unlock()
	if tail != "" {
		select {
		case w.ch <- tail:
		case <-w.ctx.Done():
		}
	}
}

// seqDone reports whether seq, starting with ESC, is a complete escape
// sequence: CSI ends with a final byte, OSC/DCS/APC with BEL or ST, others
// after one byte.
func seqDone(seq []byte) bool {
	if len(seq) < 2 {
		return false
	}
	last := seq[len(seq)-1]
	switch seq[1] {
	case '[':
		return len(seq) > 2 && last >= 0x40 && last <= 0x7e
	case ']', 'P', '_', '^', 'X':
		return last == 0x07 || len(seq) > 3 && last == '\\' && seq[len(seq)-2] == 0x1b
	}
	return true
}

// terminalWriter preserves terminal detection through the output queue.
// It never reads or closes the actual output stream.
type terminalWriter struct {
	*lineWriter
	fd uintptr
}

func (w *terminalWriter) Fd() uintptr              { return w.fd }
func (w *terminalWriter) Read([]byte) (int, error) { return 0, io.EOF }
func (w *terminalWriter) Close() error             { return nil }

// stdinQueue is an in-memory, bounded pipe: queuing never blocks the UI and
// closing it delivers EOF after pending lines. It needs no writer goroutine.
type stdinQueue struct {
	mu      sync.Mutex
	pending string
	closed  bool
	wake    chan struct{}
}

func newStdin() *stdinQueue { return &stdinQueue{wake: make(chan struct{}, 1)} }
func (q *stdinQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}
func (q *stdinQueue) send(s string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed || len(q.pending)+len(s) > 1024*1024 {
		return false
	}
	q.pending += s
	q.signal()
	return true
}
func (q *stdinQueue) close() { q.mu.Lock(); q.closed = true; q.signal(); q.mu.Unlock() }
func (q *stdinQueue) Read(p []byte) (int, error) {
	for {
		q.mu.Lock()
		if q.pending != "" {
			n := copy(p, q.pending)
			q.pending = q.pending[n:]
			q.mu.Unlock()
			return n, nil
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return strings.NewReader("").Read(p)
		}
		<-q.wake
	}
}
