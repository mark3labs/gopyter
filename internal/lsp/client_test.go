package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestHelperServer(t *testing.T) {
	if os.Getenv("GOPYTER_LSP_HELPER") != "1" {
		return
	}
	send := func(m message) {
		b := mustJSON(m)
		_, _ = fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	send(message{Method: "textDocument/publishDiagnostics", Params: json.RawMessage(`{"version":1}`)})
	id := json.RawMessage(`"config"`)
	send(message{ID: &id, Method: "workspace/configuration"})
	r := bufio.NewReader(os.Stdin)
	for {
		b, err := readFrame(r)
		if err != nil {
			os.Exit(0)
		}
		var m message
		if json.Unmarshal(b, &m) != nil {
			os.Exit(2)
		}
		if m.ID != nil && string(*m.ID) == `"config"` {
			if string(m.Result) != `"configured"` {
				os.Exit(3)
			}
		} else if m.Method == "exit" {
			os.Exit(0)
		} else if m.ID != nil {
			send(message{ID: m.ID, Result: json.RawMessage(`true`)})
		}
	}
}

func TestNotificationsReachHandler(t *testing.T) {
	testNotifications(t, true)
}
func TestNilNotifyHandler(t *testing.T) {
	testNotifications(t, false)
}
func testNotifications(t *testing.T, withNotify bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperServer$")
	cmd.Env = append(os.Environ(), "GOPYTER_LSP_HELPER=1")
	notifications := make(chan string, 1)
	requests := make(chan string, 1)
	var notify NotifyHandler
	if withNotify {
		notify = func(method string, params json.RawMessage) { notifications <- method + ":" + string(params) }
	}
	c, err := Start(cmd, func(method string, _ json.RawMessage) any { requests <- method; return "configured" }, notify)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Error(err)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ok bool
	if err := c.Call(ctx, "test", nil, &ok); err != nil || !ok {
		t.Fatalf("call: %v, %v", ok, err)
	}
	select {
	case method := <-requests:
		if method != "workspace/configuration" {
			t.Fatal(method)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if withNotify {
		select {
		case n := <-notifications:
			if n != `textDocument/publishDiagnostics:{"version":1}` {
				t.Fatal(n)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
