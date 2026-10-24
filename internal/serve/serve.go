// Package serve runs one notebook TUI and kernel per SSH session, inspired by
// euporie-hub (https://github.com/joouha/euporie). Every authorized key can run
// arbitrary code as the server's OS user; this is not a sandbox.
package serve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	"charm.land/wish/v2/activeterm"
	bm "charm.land/wish/v2/bubbletea"
	"charm.land/wish/v2/logging"
	"github.com/mark3labs/gopyter/internal/ui"
	gossh "golang.org/x/crypto/ssh"
)

// Config configures the server. Session owns creation and cleanup of each
// session's kernel and completion engine. UseTheme must be called before Serve.
type Config struct {
	Root, Listen, HostKey, AuthorizedKeys string
	Session                               func(ssh.Session, string) (ui.Options, func(), error)
	Log                                   *log.Logger
}

type server struct {
	cfg  Config
	root string
	mu   sync.Mutex
	open map[string]int
}

// Root resolves and validates the notebook directory, including symlinks.
func Root(dir string) (string, error) {
	p, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("not a directory: %s", dir)
	}
	return p, nil
}

// Resolve confines notebook commands and save-as paths to root. This guards
// against mistakes, not hostile code: cells can write anywhere. The symlink
// check has a check-then-use window and deliberately is not a sandbox.
func Resolve(root, name string) (string, error) {
	if !filepath.IsLocal(name) || filepath.Ext(name) != ".ipynb" {
		return "", fmt.Errorf("expected a relative .ipynb path inside the notebook directory")
	}
	root, err := Root(root)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, name)
	ancestor := p
	var tail []string
	for {
		resolved, err := filepath.EvalSymlinks(ancestor)
		if err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			rel, err := filepath.Rel(root, resolved)
			if err != nil || !filepath.IsLocal(rel) {
				return "", fmt.Errorf("notebook path leaves the notebook directory")
			}
			return resolved, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// A dangling symlink must not be mistaken for a missing file.
		if st, e := os.Lstat(ancestor); e == nil && st.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("dangling notebook symlink")
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", err
		}
		tail = append(tail, filepath.Base(ancestor))
		ancestor = parent
	}
}

// New validates authentication before generating a host key or listening.
// Only public-key authentication is installed, with no anonymous mode.
func New(cfg Config) (*ssh.Server, error) {
	if cfg.AuthorizedKeys == "" {
		return nil, fmt.Errorf("--authorized-keys is required: gopyter serve only accepts the SSH keys listed there")
	}
	data, err := os.ReadFile(cfg.AuthorizedKeys)
	if err != nil {
		return nil, err
	}
	valid := false
	// Follow wish's rules, which re-read the file on every login: only a
	// "#" in the first column starts a comment, and a line that doesn't
	// parse rejects every key.
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			return nil, fmt.Errorf("invalid authorized_keys: comment %q must start in the first column", strings.TrimSpace(line))
		}
		if _, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line)); err != nil {
			return nil, fmt.Errorf("invalid authorized_keys: %w", err)
		}
		valid = true
	}
	if !valid {
		return nil, fmt.Errorf("authorized_keys holds no valid key")
	}
	root, err := Root(cfg.Root)
	if err != nil {
		return nil, err
	}
	if cfg.HostKey == "" {
		return nil, fmt.Errorf("host key path is required")
	}
	if cfg.Session == nil {
		return nil, fmt.Errorf("session factory is required")
	}
	if cfg.Log == nil {
		cfg.Log = log.New(os.Stderr)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.HostKey), 0o700); err != nil {
		return nil, err
	}
	s := &server{cfg: cfg, root: root, open: make(map[string]int)}
	return wish.NewServer(
		wish.WithAddress(cfg.Listen), wish.WithHostKeyPath(cfg.HostKey), wish.WithAuthorizedKeys(cfg.AuthorizedKeys),
		wish.WithMiddleware(s.handle, activeterm.Middleware(), s.terminal, logging.MiddlewareWithLogger(cfg.Log)),
	)
}

// terminal adds the actionable diagnostic missing from activeterm v2.0.5.
func (s *server) terminal(next ssh.Handler) ssh.Handler {
	return func(sess ssh.Session) {
		if _, _, ok := sess.Pty(); !ok {
			wish.Fatalln(sess, "gopyter serve needs a terminal: use ssh -t")
			return
		}
		next(sess)
	}
}

func (s *server) handle(next ssh.Handler) ssh.Handler {
	return func(sess ssh.Session) {
		path := ""
		args := sess.Command()
		if len(args) > 1 || (len(args) == 0 && strings.TrimSpace(sess.RawCommand()) != "") {
			wish.Fatalln(sess, "gopyter: expected one notebook path")
			return
		}
		if len(args) == 1 {
			var err error
			path, err = Resolve(s.root, args[0])
			if err != nil {
				wish.Fatalln(sess, "gopyter: "+err.Error())
				return
			}
		}
		opts, closeSession, err := s.cfg.Session(sess, path)
		if err != nil {
			wish.Fatalln(sess, "gopyter: "+err.Error())
			return
		}
		defer closeSession()
		if path != "" {
			s.mu.Lock()
			if s.open[path] > 0 {
				opts.Notice = "also open in another session: saves from either one reload the other"
			}
			s.open[path]++
			s.mu.Unlock()
			defer func() {
				s.mu.Lock()
				s.open[path]--
				if s.open[path] == 0 {
					delete(s.open, path)
				}
				s.mu.Unlock()
			}()
		}
		fields := []any{"user", sess.User(), "remote", sess.RemoteAddr(), "key", gossh.FingerprintSHA256(sess.PublicKey()), "notebook", path}
		s.cfg.Log.Info("notebook connected", fields...)
		defer s.cfg.Log.Info("notebook disconnected", fields...)
		opts.Terminal = bm.MakeOptions(sess)
		opts.ResolvePath = func(p string) (string, error) { return Resolve(s.root, p) }
		opts.FixedTheme = true
		opts.SaveTheme = nil
		ctx, cancel := context.WithCancel(sess.Context())
		defer cancel()
		p, cleanup := ui.NewProgram(ctx, opts)
		defer cleanup()
		_, winch, _ := sess.Pty()
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				select {
				case <-ctx.Done():
					return
				case w, ok := <-winch:
					if !ok {
						// Session channels can close while the SSH connection stays open.
						cancel()
						return
					}
					p.Send(tea.WindowSizeMsg{Width: w.Width, Height: w.Height})
				}
			}
		}()
		_, err = p.Run()
		p.Kill()
		cancel()
		<-done
		if err != nil && sess.Context().Err() == nil {
			s.cfg.Log.Error("session ended", "err", err)
		}
		next(sess)
	}
}
