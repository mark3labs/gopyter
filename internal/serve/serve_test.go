package serve

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"charm.land/log/v2"
	"charm.land/ssh"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/ui"
	gossh "golang.org/x/crypto/ssh"
)

func TestResolve(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(root, "a.ipynb"), filepath.Join(outside, "x.ipynb")} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{"out": outside, "out-link.ipynb": filepath.Join(outside, "x.ipynb"), "in.ipynb": filepath.Join(root, "a.ipynb"), "dangling.ipynb": filepath.Join(outside, "missing.ipynb")} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"a.ipynb", "sub/b.ipynb", "new.ipynb", "sub/../a.ipynb", "in.ipynb"} {
		if _, err := Resolve(root, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"../x.ipynb", "/etc/passwd", "sub/../../x.ipynb", "", "a.txt", "out/x.ipynb", "out/new/x.ipynb", "out-link.ipynb", "dangling.ipynb"} {
		if p, err := Resolve(root, name); err == nil {
			t.Errorf("accepted %q: %s", name, p)
		}
	}
	link := filepath.Join(t.TempDir(), "root")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if p, err := Resolve(link, "a.ipynb"); err != nil || p != filepath.Join(root, "a.ipynb") {
		t.Fatalf("symlink root: %s %v", p, err)
	}
}

func signer(t *testing.T) gossh.Signer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func testConfig(t *testing.T) (Config, gossh.Signer) {
	t.Helper()
	dir := t.TempDir()
	key := signer(t)
	auth := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(auth, gossh.MarshalAuthorizedKey(key.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	return Config{Root: dir, Listen: "127.0.0.1:0", HostKey: filepath.Join(dir, "keys", "host"), AuthorizedKeys: auth, Log: log.New(io.Discard), Session: func(ssh.Session, string) (ui.Options, func(), error) { return ui.Options{}, func() {}, nil }}, key
}

func TestServeRequiresAuthorizedKeys(t *testing.T) {
	cfg, _ := testConfig(t)
	for _, contents := range []string{"", "# comment\n", "garbage"} {
		p := filepath.Join(t.TempDir(), "auth")
		if err := os.WriteFile(p, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		cfg.AuthorizedKeys = p
		if _, err := New(cfg); err == nil {
			t.Fatalf("accepted %q", contents)
		}
	}
	cfg.AuthorizedKeys = ""
	if _, err := New(cfg); err == nil {
		t.Fatal("accepted no auth")
	}
	cfg.AuthorizedKeys = filepath.Join(t.TempDir(), "missing")
	if _, err := New(cfg); err == nil {
		t.Fatal("accepted missing auth")
	}
	cfg, _ = testConfig(t)
	if _, err := New(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(cfg.HostKey)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("host key mode: %v %v", st, err)
	}
}

func start(t *testing.T, cfg Config) string {
	t.Helper()
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(l) }() // Closing the listener ends Serve.
	t.Cleanup(func() {
		_ = srv.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Serve did not stop")
		}
	})
	return l.Addr().String()
}

func dial(addr string, auth gossh.AuthMethod) (*gossh.Client, error) {
	return gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "test", Auth: []gossh.AuthMethod{auth}, HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 5 * time.Second})
}

func TestServeRejectsPasswordAndUnknownKeys(t *testing.T) {
	cfg, key := testConfig(t)
	addr := start(t, cfg)
	for _, auth := range []gossh.AuthMethod{gossh.Password("x"), gossh.PublicKeys(signer(t))} {
		c, err := dial(addr, auth)
		if err == nil {
			_ = c.Close()
			t.Fatal("authentication unexpectedly succeeded")
		}
	}
	c, err := dial(addr, gossh.PublicKeys(key))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestServeRejectsCommandsAndNoPTY(t *testing.T) {
	cfg, key := testConfig(t)
	var called atomic.Bool
	cfg.Session = func(ssh.Session, string) (ui.Options, func(), error) {
		called.Store(true)
		return ui.Options{}, func() {}, nil
	}
	addr := start(t, cfg)
	for _, tc := range []struct {
		command string
		pty     bool
		want    string
	}{
		{"../x.ipynb", true, "relative .ipynb"}, {"a.ipynb b.ipynb", true, "one notebook"}, {"a.ipynb", false, "use ssh -t"},
	} {
		c, err := dial(addr, gossh.PublicKeys(key))
		if err != nil {
			t.Fatal(err)
		}
		sess, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if tc.pty {
			if err := sess.RequestPty("xterm", 30, 100, nil); err != nil {
				t.Fatal(err)
			}
		}
		var stderr bytes.Buffer
		sess.Stderr = &stderr
		if err := sess.Run(tc.command); err == nil {
			t.Error("expected nonzero exit")
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Errorf("stderr: %q", stderr.String())
		}
		_ = sess.Close()
		_ = c.Close()
	}
	if called.Load() {
		t.Fatal("factory called for refused session")
	}
}

func TestServeConcurrentSessionsLifecycle(t *testing.T) {
	cfg, key := testConfig(t)
	if err := ui.UseTheme(ui.DefaultTheme, true); err != nil {
		t.Fatal(err)
	}
	closed := make(chan string, 2)
	cfg.Session = func(_ ssh.Session, path string) (ui.Options, func(), error) {
		k, err := kernel.New("")
		if err != nil {
			return ui.Options{}, nil, err
		}
		k.RunDir = cfg.Root
		nb := &notebook.Notebook{Cells: []*notebook.Cell{{Type: notebook.Markdown, Source: "# Concurrent **markdown**\n\n- list item"}}}
		return ui.Options{Kernel: k, Path: path, Notebook: nb}, func() {
			dir := k.Dir
			if err := k.Close(); err != nil {
				t.Error(err)
			}
			closed <- dir
		}, nil
	}
	addr := start(t, cfg)
	var sessions []*gossh.Session
	var rendered []<-chan struct{}
	for range 2 {
		c, err := dial(addr, gossh.PublicKeys(key))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		sess, err := c.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.RequestPty("xterm-256color", 30, 100, nil); err != nil {
			t.Fatal(err)
		}
		sessions = append(sessions, sess)
		input, err := sess.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		output, err := sess.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := sess.Start("a.ipynb"); err != nil {
			t.Fatal(err)
		}
		frame := make(chan struct{})
		status := make(chan struct{})
		rendered = append(rendered, status)
		go func() {
			b := make([]byte, 4096)
			n, err := output.Read(b)
			close(frame)
			text := string(b[:n])
			notified := false
			for err == nil {
				if !notified && strings.Contains(text, "the theme is set by gopyter serve") {
					close(status)
					notified = true
				}
				n, err = output.Read(b)
				text += string(b[:n])
			}
		}()
		select {
		case <-frame:
		case <-time.After(10 * time.Second):
			t.Fatal("no TUI frame")
		}
		if err := sess.WindowChange(24, 80); err != nil {
			t.Fatal(err)
		}
		if _, err := input.Write([]byte("T")); err != nil {
			t.Fatal(err)
		}
	}
	for _, status := range rendered {
		select {
		case <-status:
		case <-time.After(10 * time.Second):
			t.Fatal("fixed-theme status was not rendered")
		}
	}
	// Close only the session channels: a multiplexed SSH connection can remain
	// alive, but its notebook and kernel must still be cleaned up.
	for _, sess := range sessions {
		_ = sess.Close()
	}
	for range 2 {
		select {
		case dir := <-closed:
			if _, err := os.Stat(dir); !os.IsNotExist(err) {
				t.Errorf("workspace remains: %s (%v)", dir, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("session cleanup did not run")
		}
	}
}
