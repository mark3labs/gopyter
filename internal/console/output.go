package console

import (
	"context"
	"io"
	"strings"
	"sync"
)

// lineWriter leaves only the unfinished tail in the live view. Complete lines
// are drained in order into scrollback; cancellation unblocks a full queue.
type lineWriter struct {
	mu   sync.Mutex
	tail string
	ch   chan string
	ctx  context.Context
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	var lines []string
	for _, r := range p {
		switch r {
		case '\r':
			w.tail = ""
		case '\n':
			lines = append(lines, w.tail)
			w.tail = ""
		default:
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
func (w *lineWriter) partial() string { w.mu.Lock(); defer w.mu.Unlock(); return w.tail }
func (w *lineWriter) flush() {
	w.mu.Lock()
	tail := w.tail
	w.tail = ""
	w.mu.Unlock()
	if tail != "" {
		select {
		case w.ch <- tail:
		case <-w.ctx.Done():
		}
	}
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
