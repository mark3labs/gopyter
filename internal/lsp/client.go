// Package lsp is a minimal Language Server Protocol client, sufficient to
// drive gopls for code completion.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClosed is returned for calls on a closed client.
var ErrClosed = errors.New("lsp: connection closed")

// Error is a JSON-RPC error returned by the server.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("lsp: %s (%d)", e.Message, e.Code) }

type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

type response struct {
	result json.RawMessage
	err    error
}

// Handler answers requests initiated by the server. It returns the result
// to send back (nil means JSON null).
type Handler func(method string, params json.RawMessage) any

// Client is a connection to a language server subprocess.
type Client struct {
	cmd     *exec.Cmd
	w       io.WriteCloser
	wmu     sync.Mutex
	nextID  atomic.Int64
	pmu     sync.Mutex
	pending map[int64]chan response
	handler Handler
	done    chan struct{}
	closed  atomic.Bool
}

// Start launches a language server and begins reading its messages.
func Start(cmd *exec.Cmd, handler Handler) (*Client, error) {
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{
		cmd: cmd, w: stdin, handler: handler,
		pending: map[int64]chan response{},
		done:    make(chan struct{}),
	}
	go c.readLoop(bufio.NewReaderSize(stdout, 64*1024))
	return c, nil
}

func (c *Client) readLoop(r *bufio.Reader) {
	defer func() {
		c.closed.Store(true)
		c.pmu.Lock()
		for id, ch := range c.pending {
			ch <- response{err: ErrClosed}
			delete(c.pending, id)
		}
		c.pmu.Unlock()
		close(c.done)
	}()
	for {
		body, err := readFrame(r)
		if err != nil {
			return
		}
		var msg message
		if json.Unmarshal(body, &msg) != nil {
			continue
		}
		switch {
		case msg.Method != "" && msg.ID != nil:
			// Server-to-client request.
			var result any
			if c.handler != nil {
				result = c.handler(msg.Method, msg.Params)
			}
			_ = c.send(message{ID: msg.ID, Result: mustJSON(result)})
		case msg.Method != "":
			// Notification (logs, diagnostics, progress): ignored.
		case msg.ID != nil:
			id, err := strconv.ParseInt(strings.Trim(string(*msg.ID), `"`), 10, 64)
			if err != nil {
				continue
			}
			c.pmu.Lock()
			ch, ok := c.pending[id]
			delete(c.pending, id)
			c.pmu.Unlock()
			if ok {
				if msg.Error != nil {
					ch <- response{err: msg.Error}
				} else {
					ch <- response{result: msg.Result}
				}
			}
		}
	}
}

func readFrame(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Content-Length") {
			if length, err = strconv.Atoi(strings.TrimSpace(v)); err != nil {
				return nil, err
			}
		}
	}
	if length < 0 {
		return nil, errors.New("lsp: missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(r, body)
	return body, err
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

func (c *Client) send(msg message) error {
	if c.closed.Load() {
		return ErrClosed
	}
	msg.JSONRPC = "2.0"
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = c.w.Write(body)
	return err
}

// Call sends a request and decodes its result into result (if non-nil).
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	id := c.nextID.Add(1)
	raw := json.RawMessage(strconv.FormatInt(id, 10))
	ch := make(chan response, 1)
	c.pmu.Lock()
	c.pending[id] = ch
	c.pmu.Unlock()
	if err := c.send(message{ID: &raw, Method: method, Params: mustJSON(params)}); err != nil {
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			return json.Unmarshal(r.result, result)
		}
		return nil
	case <-ctx.Done():
		c.pmu.Lock()
		delete(c.pending, id)
		c.pmu.Unlock()
		// Let the server stop working on it.
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	}
}

// Notify sends a notification.
func (c *Client) Notify(method string, params any) error {
	return c.send(message{Method: method, Params: mustJSON(params)})
}

// Done is closed when the connection terminates.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close shuts the server down gracefully, killing it if it doesn't exit.
func (c *Client) Close() error {
	if !c.closed.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Call(ctx, "shutdown", nil, nil)
		cancel()
		_ = c.Notify("exit", nil)
	}
	_ = c.w.Close()
	select {
	case <-c.done:
	case <-time.After(2 * time.Second):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	}
	return c.cmd.Wait()
}
