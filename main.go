package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"charm.land/fang/v2"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/runner"
	"github.com/mark3labs/gopyter/internal/ui"
	"github.com/spf13/cobra"
)

// version and commit can be set at build time with -ldflags "-X
// main.version=... -X main.commit=...". When empty, fang reports the module
// version from the build info (e.g. for go install ...@vX.Y.Z).
var (
	version = ""
	commit  = ""
)

func loadNotebook(path string) (*notebook.Notebook, error) {
	if path == "" {
		return notebook.New(), nil
	}
	nb, err := notebook.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return notebook.New(), nil
	}
	return nb, err
}

func rootCmd() *cobra.Command {
	var (
		workdir    string
		theme      string
		noComplete bool
		vim        bool
	)
	cmd := &cobra.Command{
		Use:   "gopyter [notebook.ipynb]",
		Short: "A slick terminal notebook for Go",
		Long: "gopyter is a Jupyter-style notebook for Go that runs entirely in your terminal.\n\n" +
			"Declarations (func, type, var, const, import) persist across cells, statements run\n" +
			"inside main(), and a trailing expression is displayed as the cell's result.\n" +
			"Notebooks are stored as .ipynb files compatible with the GoNB Jupyter kernel.",
		Example: "  # start a new notebook\n  gopyter\n\n" +
			"  # open (or create) a notebook\n  gopyter analysis.ipynb\n\n" +
			"  # execute a notebook headlessly and store the outputs\n  gopyter run analysis.ipynb --save",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			nb, err := loadNotebook(path)
			if err != nil {
				return err
			}
			k, err := kernel.New(workdir)
			if err != nil {
				return err
			}
			defer closeKernel(k)
			opts := ui.Options{Path: path, Notebook: nb, Kernel: k, SyntaxTheme: theme, Vim: vim}
			if !noComplete {
				engine := complete.New(k)
				defer func() { _ = engine.Close() }()
				opts.Completer = engine
			}
			return ui.Run(cmd.Context(), opts)
		},
	}
	cmd.PersistentFlags().StringVar(&workdir, "workdir", "", "persistent kernel workspace (Go module) directory; a temporary one is used by default")
	cmd.Flags().StringVar(&theme, "syntax-theme", "catppuccin-mocha", "chroma syntax highlighting theme")
	cmd.Flags().BoolVar(&noComplete, "no-complete", false, "disable code completion (gopls)")
	cmd.Flags().BoolVar(&vim, "vim", false, "use vim key bindings in edit mode")

	cmd.AddCommand(runCmd(&workdir))
	return cmd
}

func runCmd(workdir *string) *cobra.Command {
	var (
		save     bool
		failFast bool
	)
	cmd := &cobra.Command{
		Use:   "run <notebook.ipynb>",
		Short: "Execute every code cell of a notebook and print the outputs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			nb, err := notebook.Load(args[0])
			if err != nil {
				return err
			}
			k, err := kernel.New(*workdir)
			if err != nil {
				return err
			}
			defer closeKernel(k)
			if dir, err := os.Getwd(); err == nil {
				k.RunDir = dir
			}
			failed := runner.Run(cmd.Context(), k, nb, cmd.OutOrStdout(), failFast)
			if save {
				if err := nb.Save(args[0]); err != nil {
					return err
				}
			}
			if failed > 0 {
				return fmt.Errorf("%d cell%s failed", failed, strings.Repeat("s", min(failed-1, 1)))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&save, "save", false, "write the outputs back into the notebook")
	cmd.Flags().BoolVar(&failFast, "fail-fast", false, "stop at the first failing cell")
	return cmd
}

// closeKernel removes the kernel workspace, reporting (but not failing on)
// cleanup errors.
func closeKernel(k *kernel.Kernel) {
	if err := k.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: cleaning up kernel workspace: %v\n", err)
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := fang.Execute(ctx, rootCmd(), fang.WithVersion(version), fang.WithCommit(commit)); err != nil {
		os.Exit(1)
	}
}
