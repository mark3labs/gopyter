// Package wire connects a cell's program to gopyter: rich output goes to
// file descriptor 3 and events from the UI (widget values, replies, "done")
// come from file descriptor 4, both as JSON lines. It's used by package nb
// and by gopyter's GoNB compatibility packages; notebooks don't need it.
package wire

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Connected reports whether the program runs in gopyter, which shows rich
// output. The kernel sets GOPYTER_RICH when it provides the pipes.
func Connected() bool { return connected }

var (
	connected = os.Getenv("GOPYTER_RICH") == "1"

	outMu  sync.Mutex
	out    *os.File
	stdout = os.Stdout // file descriptor 1, even if a cell reassigns os.Stdout
	seq    uint64
)

func init() {
	if connected {
		out = os.NewFile(3, "gopyter-out")
	}
}

// Messages and stdout travel through separate pipes, so the kernel can't
// tell on its own whether fmt.Println("a") came before or after a Display.
// Each message is therefore preceded by a sync marker on stdout carrying
// its sequence number; the kernel strips the marker and emits the message
// exactly there. The marker is an APC escape sequence, so it stays
// invisible should it ever reach a terminal, and short enough (< PIPE_BUF)
// to be written atomically. Keep in sync with the kernel's syncPrefix.
const (
	syncPrefix = "\x1b_gopyter-sync:"
	syncSuffix = "\x1b\\"
)

// send writes one message, as "<seq>\t<json>\n". seq is 0 when no marker
// could be written, so the kernel doesn't wait for one.
func send(v any) bool {
	if !connected {
		return false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	outMu.Lock()
	defer outMu.Unlock()
	seq++
	n := seq
	if _, err := fmt.Fprintf(stdout, "%s%d%s", syncPrefix, n, syncSuffix); err != nil {
		n = 0
	}
	_, err = fmt.Fprintf(out, "%d\t%s\n", n, b)
	return err == nil
}

// Display shows data of the given MIME type ("text/plain", "text/markdown",
// "text/html" or "image/png" as base64). A later Display with the same
// non-empty id replaces it. Outside gopyter, plain is printed instead.
func Display(mime, data, plain, id string) {
	m := map[string]string{"mime": mime, "data": data}
	if id != "" {
		m["id"] = id
	}
	if !send(m) {
		fmt.Println(plain)
	}
}

// Op sends an operation to the UI, like a DOM change or a widget value.
// It reports whether it was sent.
func Op(op map[string]any) bool { return send(op) }

// NewID returns a new unique id.
func NewID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b) // never fails
	return hex.EncodeToString(b)
}

// Events from the UI.

type subscription struct {
	q    chan json.RawMessage
	stop chan struct{}
}

var (
	startOnce sync.Once
	subMu     sync.Mutex
	subs      = map[string]map[int]*subscription{}
	nextSub   int
	done      = make(chan struct{})
	doneOnce  sync.Once
	eof       = make(chan struct{})
)

func start() { startOnce.Do(func() { go readEvents() }) }

func markDone() { doneOnce.Do(func() { close(done) }) }

func readEvents() {
	defer close(eof)
	defer markDone()
	if !connected {
		return
	}
	in := os.NewFile(4, "gopyter-in")
	if in == nil {
		return
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		var m struct {
			Address string          `json:"address"`
			Value   json.RawMessage `json:"value"`
			Done    bool            `json:"done"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Done {
			markDone()
			continue
		}
		subMu.Lock()
		targets := make([]*subscription, 0, len(subs[m.Address]))
		for _, s := range subs[m.Address] {
			targets = append(targets, s)
		}
		subMu.Unlock()
		for _, s := range targets {
			select {
			case s.q <- m.Value:
			case <-s.stop: // cancelled meanwhile
			}
		}
	}
	// End of file means the UI is done; an error too, but say why.
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: widget events: %v\n", err)
	}
}

// Subscribe calls fn with every value the UI sends to address, in order,
// until the returned function is called.
func Subscribe(address string, fn func(value json.RawMessage)) (cancel func()) {
	start()
	s := &subscription{q: make(chan json.RawMessage, 256), stop: make(chan struct{})}
	subMu.Lock()
	id := nextSub
	nextSub++
	if subs[address] == nil {
		subs[address] = map[int]*subscription{}
	}
	subs[address][id] = s
	subMu.Unlock()
	go func() {
		for {
			select {
			case v := <-s.q:
				fn(v)
			case <-s.stop:
				return
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			subMu.Lock()
			delete(subs[address], id)
			if len(subs[address]) == 0 {
				delete(subs, address)
			}
			subMu.Unlock()
			close(s.stop)
		})
	}
}

// Done returns a channel that is closed when the user ends the program's
// input (the Done button in gopyter), or when there is no UI to send any.
func Done() <-chan struct{} {
	start()
	return done
}

// Request sends op with a "reply" address and waits for the UI's answer.
// ok is false when no answer can come (not in gopyter, or the UI is gone).
func Request(op map[string]any) (value json.RawMessage, ok bool) {
	if !connected {
		return nil, false
	}
	start()
	reply := "#reply/" + NewID()
	ch := make(chan json.RawMessage, 1)
	cancel := Subscribe(reply, func(v json.RawMessage) {
		select {
		case ch <- v:
		default:
		}
	})
	defer cancel()
	op["reply"] = reply
	if !Op(op) {
		return nil, false
	}
	select {
	case v := <-ch:
		return v, true
	case <-eof:
		return nil, false
	}
}
