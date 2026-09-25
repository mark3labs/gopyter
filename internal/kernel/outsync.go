package kernel

import (
	"bytes"
	"io"
	"strconv"
	"sync"
)

// A cell's program writes stdout and rich messages (file descriptor 3) to
// separate pipes, read by separate goroutines, so on their own they would
// be emitted in whichever order the reads happen. The runtime (package
// wire) therefore precedes every message with a sync marker on stdout
// carrying the message's sequence number. syncPump strips the markers and
// outSync lines the two streams up: message n is emitted after the stdout
// written before marker n, and stdout written after it waits until message
// n was emitted.
//
// Keep the marker in sync with runtime/gopyter/nb/wire.
const (
	syncPrefix = "\x1b_gopyter-sync:"
	syncSuffix = "\x1b\\"
	// syncMaxLen bounds a marker: longer candidates are plain output.
	syncMaxLen = len(syncPrefix) + 20 + len(syncSuffix)
)

// outSync tracks how far each stream got.
type outSync struct {
	mu     sync.Mutex
	cond   *sync.Cond
	stdout uint64 // last marker reached on stdout
	rich   uint64 // last message emitted
	// Set when a stream ends, so the other one never waits for it.
	stdoutDone, richDone bool
}

func newOutSync() *outSync {
	s := &outSync{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// marker records that stdout reached marker n, then waits until message n
// was emitted (or can't be anymore).
func (s *outSync) marker(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stdout = max(s.stdout, n)
	s.cond.Broadcast()
	for s.rich < n && !s.richDone {
		s.cond.Wait()
	}
}

// beforeMessage waits until stdout reached marker n (or ended).
func (s *outSync) beforeMessage(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for s.stdout < n && !s.stdoutDone {
		s.cond.Wait()
	}
}

// afterMessage records that message n was emitted.
func (s *outSync) afterMessage(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rich = max(s.rich, n)
	s.cond.Broadcast()
}

// closeStdout and closeRich mark a stream as ended.
func (s *outSync) closeStdout() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stdoutDone = true
	s.cond.Broadcast()
}

func (s *outSync) closeRich() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.richDone = true
	s.cond.Broadcast()
}

// syncPump is pump for the stdout of a cell's program: it emits the output
// without the sync markers, and calls s.marker at each of them.
func syncPump(r io.Reader, s *outSync, emit func(Event)) {
	defer s.closeStdout()
	var pending []byte
	flush := func(b []byte) {
		if len(b) > 0 {
			emit(Event{Kind: Stdout, Text: string(b)})
		}
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		pending = append(pending, buf[:n]...)
		pending = splitMarkers(pending, flush, s.marker)
		if err != nil {
			flush(pending)
			return
		}
	}
}

// splitMarkers passes the text of b to flush and the sequence numbers of
// complete markers to marker, in order. It returns the tail that may be the
// start of a marker, to be completed by the next read.
func splitMarkers(b []byte, flush func([]byte), marker func(uint64)) []byte {
	for {
		i := bytes.Index(b, []byte(syncPrefix))
		if i < 0 {
			// Keep a suffix that may be a cut marker prefix.
			keep := 0
			for k := min(len(b), len(syncPrefix)-1); k > 0; k-- {
				if bytes.HasPrefix([]byte(syncPrefix), b[len(b)-k:]) {
					keep = k
					break
				}
			}
			flush(b[:len(b)-keep])
			return bytes.Clone(b[len(b)-keep:])
		}
		rest := b[i+len(syncPrefix):]
		j := bytes.Index(rest, []byte(syncSuffix))
		if j < 0 {
			if len(b)-i < syncMaxLen {
				flush(b[:i])
				return bytes.Clone(b[i:]) // wait for the rest
			}
			j = len(rest) // too long: not a marker
		}
		var n uint64
		err := strconv.ErrSyntax
		if j < len(rest) {
			n, err = strconv.ParseUint(string(rest[:j]), 10, 64)
		}
		if err != nil {
			// Not a marker after all: emit the prefix as text.
			flush(b[:i+len(syncPrefix)])
			b = rest
			continue
		}
		flush(b[:i])
		marker(n)
		b = rest[j+len(syncSuffix):]
	}
}
