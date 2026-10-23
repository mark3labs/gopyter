package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"charm.land/log/v2"
	"charm.land/ssh"
	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/serve"
	"github.com/mark3labs/gopyter/internal/ui"
	"github.com/spf13/cobra"
	gossh "golang.org/x/crypto/ssh"
)

func serveCmd(workdir *string) *cobra.Command {
	var listen, hostKey, authorized, theme, model string
	var light, vim, noComplete bool
	cmd := &cobra.Command{
		Use: "serve [dir]", Short: "Serve the notebook TUI over SSH to trusted key holders",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if *workdir != "" || cmd.Flags().Changed("workdir") {
				return fmt.Errorf("--workdir can't be used with serve: each session gets its own workspace")
			}
			if authorized == "" {
				return fmt.Errorf("--authorized-keys is required: gopyter serve only accepts the SSH keys listed there")
			}
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			root, err := serve.Root(dir)
			if err != nil {
				return err
			}
			if hostKey == "" {
				p, err := config.Path()
				if err != nil {
					return err
				}
				hostKey = filepath.Join(filepath.Dir(p), "ssh_host_ed25519")
			}
			settings := loadSettings(cmd)
			name, err := resolveTheme(cmd, theme, settings)
			if err != nil {
				return err
			}
			if err := ui.UseTheme(name, !light); err != nil {
				return err
			}
			aiModel, aiOn, err := resolveModel(cmd, model, settings)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("vim") {
				vim = settings.Vim
			}
			logger := log.New(cmd.ErrOrStderr())
			var sessions sync.WaitGroup
			var sessionMu sync.Mutex
			stopping := false
			srv, err := serve.New(serve.Config{
				Root: root, Listen: listen, HostKey: hostKey, AuthorizedKeys: authorized, Log: logger,
				Session: func(_ ssh.Session, path string) (ui.Options, func(), error) {
					sessionMu.Lock()
					if stopping {
						sessionMu.Unlock()
						return ui.Options{}, nil, fmt.Errorf("server is stopping")
					}
					sessions.Add(1)
					sessionMu.Unlock()
					opts, cleanup, err := sessionOptions(path, "", noComplete, vim, aiModel, aiOn)
					if err != nil {
						sessions.Done()
						return opts, cleanup, err
					}
					opts.Theme, opts.LightBackground = name, light
					opts.Kernel.RunDir = root
					if path != "" {
						opts.Kernel.RunDir = filepath.Dir(path)
					}
					return opts, func() { defer sessions.Done(); cleanup() }, nil
				},
			})
			if err != nil {
				return err
			}
			l, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return err
			}
			defer func() { _ = srv.Close() }() // Also close any clients after a serve error.
			username := os.Getenv("USER")
			if u, err := user.Current(); err == nil {
				username = u.Username
			}
			warning := fmt.Sprintf("cells run as %s on this machine: only give access to people you would give a shell to", username)
			logger.Info("gopyter serve", "listen", l.Addr(), "host-key", gossh.FingerprintSHA256(srv.HostSigners[0].PublicKey()), "authorized-keys", authorized)
			logger.Warn(warning)
			host, _, _ := net.SplitHostPort(listen) // net.Listen validated the address.
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				logger.Warn("non-loopback listener: " + warning)
			}
			result := make(chan error, 1)
			go func() { result <- srv.Serve(l) }()
			select {
			case err := <-result:
				if errors.Is(err, ssh.ErrServerClosed) {
					return nil
				}
				return err
			case <-cmd.Context().Done():
				// Shutdown alone does not interrupt active SSH sessions. Close first so
				// session contexts cancel and UI cleanup can drain running cells.
				sessionMu.Lock()
				stopping = true
				sessionMu.Unlock()
				if err := srv.Close(); err != nil {
					return err
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := srv.Shutdown(ctx); err != nil {
					return err
				}
				finished := make(chan struct{})
				go func() { sessions.Wait(); close(finished) }()
				select {
				case <-finished:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "127.0.0.1:2222", "SSH listen address (loopback by default)")
	cmd.Flags().StringVar(&hostKey, "host-key", "", "host private key (default: user config dir/gopyter/ssh_host_ed25519; generated if missing)")
	cmd.Flags().StringVar(&authorized, "authorized-keys", "", "required OpenSSH authorized_keys file (trusted users only)")
	cmd.Flags().StringVar(&theme, "theme", "", "shared color theme (default: saved theme)")
	cmd.Flags().BoolVar(&light, "light", false, "use the light theme variant for every session")
	cmd.Flags().BoolVar(&vim, "vim", false, "use vim bindings (default: saved setting)")
	cmd.Flags().BoolVar(&noComplete, "no-complete", false, "disable per-session gopls completion")
	cmd.Flags().StringVar(&model, "model", "", "AI model, provider/model or off (default: saved model)")
	return cmd
}
