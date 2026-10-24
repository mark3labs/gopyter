package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"charm.land/fang/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/config"
	"github.com/mark3labs/gopyter/internal/console"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/runner"
	"github.com/mark3labs/gopyter/internal/termimg"
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

// appVersion is the version reported by %version: the -ldflags one, else
// the module version from the build info, like fang does.
func appVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "dev"
}

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
		graphics   string
		workdir    string
		theme      string
		syntax     string
		noComplete bool
		vim        bool
		model      string
	)
	cmd := &cobra.Command{
		Use:   "gopyter [notebook.ipynb]",
		Short: "A slick terminal notebook for Go",
		Long: "gopyter is a Jupyter-style notebook for Go that runs entirely in your terminal.\n\n" +
			"Declarations (func, type, var, const, import) persist across cells, statements run\n" +
			"inside main(), and a trailing expression is displayed as the cell's result.\n" +
			"Notebooks are stored as .ipynb files compatible with the GoNB Jupyter kernel\n" +
			"(https://github.com/janpfeifer/gonb), which inspired gopyter's execution model,\n" +
			"cell commands and widgets.",
		Example: "  # start a new notebook\n  gopyter\n\n" +
			"  # open (or create) a notebook\n  gopyter analysis.ipynb\n\n" +
			"  # execute a notebook headlessly and store the outputs\n  gopyter run analysis.ipynb --save\n\n" +
			"  # start an inline Go REPL\n  gopyter console",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := ""
			if len(args) == 1 {
				path = args[0]
			}
			settings := loadSettings(cmd)
			name, err := resolveTheme(cmd, theme, settings)
			if err != nil {
				return err
			}
			aiModel, aiOn, err := resolveModel(cmd, model, settings)
			if err != nil {
				return err
			}
			// An explicit --vim / --vim=false wins for this session only.
			if !cmd.Flags().Changed("vim") {
				vim = settings.Vim
			}
			gfx, err := resolveGraphics(cmd, graphics, settings)
			if err != nil {
				return err
			}
			_, warnings := ui.KeyCommands(settings.Keys)
			for _, w := range warnings {
				cmd.PrintErrf("gopyter: key bindings: %s\n", w)
			}
			opts, cleanup, err := sessionOptions(path, workdir, noComplete, vim, aiModel, aiOn)
			if err != nil {
				return err
			}
			opts.Graphics = gfx
			opts.Keys = settings.Keys
			defer cleanup()
			opts.Theme, opts.SyntaxTheme, opts.SaveTheme = name, syntax, saveTheme
			// Probe before the program owns stdin.
			opts.LightBackground = !lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
			return ui.Run(cmd.Context(), opts)
		},
	}
	cmd.PersistentFlags().StringVar(&graphics, "graphics", "", "how to draw images: auto, kitty or blocks; defaults to the saved setting")
	cmd.PersistentFlags().StringVar(&workdir, "workdir", "", "persistent kernel workspace (Go module) directory; a temporary one is used by default")
	cmd.Flags().StringVar(&theme, "theme", "", "color theme for this session (see 'gopyter themes'); the saved theme is used by default")
	cmd.Flags().StringVar(&syntax, "syntax-theme", "", "chroma syntax highlighting style, overriding the theme's")
	cmd.Flags().BoolVar(&noComplete, "no-complete", false, "disable code completion (gopls)")
	cmd.Flags().BoolVar(&vim, "vim", false, "use vim key bindings in edit mode for this session (toggle and save with V); the saved setting is used by default")
	cmd.Flags().StringVar(&model, "model", "", "AI model for this session, as provider/model or a provider name, or off (see 'gopyter model'); the saved one is used by default")

	cmd.AddCommand(consoleCmd(&workdir), runCmd(&workdir, &graphics), formatCmd(), themesCmd(), modelCmd(), keysCmd(), serveCmd(&workdir))
	return cmd
}

func consoleCmd(workdir *string) *cobra.Command {
	var save string
	var noComplete bool
	cmd := &cobra.Command{
		Use: "console", Short: "Run Go interactively on a notebook kernel", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			k, err := kernel.New(*workdir)
			if err != nil {
				return err
			}
			defer closeKernel(k)
			if dir, err := os.Getwd(); err == nil {
				k.RunDir = dir
			}
			opts := console.Options{Kernel: k, SavePath: save}
			if path, err := config.HistoryPath(); err == nil {
				opts.HistoryPath = path
			}
			if f, ok := cmd.InOrStdin().(*os.File); !noComplete && ok && term.IsTerminal(f.Fd()) {
				engine := complete.New(k)
				defer func() { _ = engine.Close() }() // best-effort cleanup, as in the notebook
				opts.Completer = engine
			}
			return console.Run(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&save, "save", "", "write the session to this notebook on exit")
	cmd.Flags().BoolVar(&noComplete, "no-complete", false, "disable code completion (gopls)")
	return cmd
}

func runCmd(workdir, graphics *string) *cobra.Command {
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
			gfx, err := resolveGraphics(cmd, *graphics, loadSettings(cmd))
			if err != nil {
				return err
			}
			if f, ok := cmd.OutOrStdout().(*os.File); !ok || !term.IsTerminal(f.Fd()) {
				gfx = nil
			}
			failed := runner.RunWith(cmd.Context(), k, nb, cmd.OutOrStdout(), cmd.InOrStdin(), runner.Options{FailFast: failFast, Graphics: gfx})
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

// loadSettings reads the saved settings. Errors are only reported, so a
// broken config never prevents gopyter from starting.
func loadSettings(cmd *cobra.Command) config.Settings {
	s, err := config.Load()
	if err != nil {
		cmd.PrintErrf("gopyter: reading settings: %v\n", err)
		return config.Settings{}
	}
	return s
}

// resolveTheme picks the UI theme: the --theme flag, else the saved one,
// else the default. A bad flag is an error; a bad saved value only a warning,
// so a stale config never prevents gopyter from starting.
func resolveTheme(cmd *cobra.Command, flag string, s config.Settings) (string, error) {
	if flag != "" {
		if !ui.ValidTheme(flag) {
			return "", fmt.Errorf("unknown theme %q (available: %s)", flag, strings.Join(ui.ThemeNames(), ", "))
		}
		return flag, nil
	}
	if s.Theme == "" {
		return ui.DefaultTheme, nil
	}
	if !ui.ValidTheme(s.Theme) {
		cmd.PrintErrf("gopyter: unknown saved theme %q, using %s\n", s.Theme, ui.DefaultTheme)
		return ui.DefaultTheme, nil
	}
	return s.Theme, nil
}

// resolveGraphics gives flags precedence over saved settings. Invalid saved
// values warn rather than preventing startup; detection never reads stdin.
func resolveGraphics(cmd *cobra.Command, flag string, s config.Settings) (*termimg.Kitty, error) {
	value := flag
	if value == "" {
		value = s.Graphics
	}
	mode, err := termimg.ParseMode(value)
	if err != nil {
		if flag != "" {
			return nil, err
		}
		cmd.PrintErrf("gopyter: %v; using auto\n", err)
		mode = termimg.Auto
	}
	environ := os.Environ()
	profile := colorprofile.Detect(os.Stdout, environ)
	mode, tmux, warn := termimg.Resolve(mode, environ, profile)
	if warn != "" {
		cmd.PrintErrf("gopyter: %s\n", warn)
	}
	if mode != termimg.KittyMode {
		return nil, nil
	}
	return termimg.NewKitty(tmux, profile < colorprofile.TrueColor), nil
}

// saveTheme persists the theme picked in the UI.
func saveTheme(name string) error {
	return config.Update(func(s *config.Settings) { s.Theme = name })
}

// saveVim persists the vim bindings setting toggled in the UI.
func saveVim(on bool) error {
	return config.Update(func(s *config.Settings) { s.Vim = on })
}

func themesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "themes",
		Short: "List the available color themes",
		Long: "List the available color themes. The active one is marked with *.\n\n" +
			"Pick a theme inside gopyter with T (or the ◐ theme button); the choice is\n" +
			"saved to the user config directory. --theme overrides it for one session.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			current, err := resolveTheme(cmd, "", loadSettings(cmd))
			if err != nil {
				return err
			}
			var b strings.Builder
			for _, n := range ui.ThemeNames() {
				mark := "  "
				if n == current {
					mark = "* "
				}
				b.WriteString(mark)
				b.WriteString(n)
				b.WriteByte('\n')
			}
			if p, err := config.Path(); err == nil {
				b.WriteString("\nsettings: ")
				b.WriteString(p)
				b.WriteByte('\n')
			}
			_, err = io.WriteString(cmd.OutOrStdout(), b.String())
			return err
		},
	}
}

// sessionOptions shares notebook, kernel, completion and AI setup between local
// and SSH frontends. Cleanup must run only after the UI has cancelled its work.
func sessionOptions(path, workdir string, noComplete, vim bool, model string, aiOn bool) (ui.Options, func(), error) {
	nb, err := loadNotebook(path)
	if err != nil {
		return ui.Options{}, nil, err
	}
	k, err := kernel.New(workdir)
	if err != nil {
		return ui.Options{}, nil, err
	}
	opts := ui.Options{Path: path, Notebook: nb, Kernel: k, Vim: vim, SaveVim: saveVim, AI: newAI(model, aiOn)}
	var engine *complete.Engine
	if !noComplete {
		engine = complete.New(k)
		opts.Completer = engine
	}
	return opts, func() {
		if engine != nil {
			_ = engine.Close()
		} // Completion cleanup is best effort.
		closeKernel(k)
	}, nil
}

// closeKernel removes the kernel workspace, reporting (but not failing on)
// cleanup errors.
func closeKernel(k *kernel.Kernel) {
	if err := k.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: cleaning up kernel workspace: %v\n", err)
	}
}

// signalError is the cause of the context main cancels on a signal.
type signalError struct{ sig os.Signal }

func (e signalError) Error() string { return e.sig.String() }

// exitCode is the status for a command's error: 128+n when signal n
// stopped it, as shells do, so that scripts see the interruption.
func exitCode(err, cause error) int {
	if err == nil {
		return 0 // e.g. serve shutting down cleanly
	}
	if s, ok := errors.AsType[signalError](cause); ok {
		if n, ok := s.sig.(syscall.Signal); ok {
			return 128 + int(n)
		}
	}
	return 1
}

func main() {
	kernel.Version = appVersion()
	// Like signal.NotifyContext, but the cause says which signal it was.
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() { cancel(signalError{<-sigs}) }()
	// Being stopped by a signal isn't an error to report ("context
	// canceled"); the exit status says what happened.
	quiet := func(w io.Writer, styles fang.Styles, err error) {
		if _, ok := errors.AsType[signalError](context.Cause(ctx)); !ok {
			fang.DefaultErrorHandler(w, styles, err)
		}
	}
	err := fang.Execute(ctx, rootCmd(), fang.WithVersion(version), fang.WithCommit(commit), fang.WithErrorHandler(quiet))
	if code := exitCode(err, context.Cause(ctx)); code != 0 {
		os.Exit(code)
	}
}
