package console

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/mark3labs/gopyter/internal/complete"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/runner"
)

// Completer provides notebook-aware code completion.
type Completer interface {
	Complete(context.Context, complete.Request) (complete.Result, error)
}

// Options configure a console session.
type Options struct {
	Kernel      *kernel.Kernel
	Completer   Completer
	HistoryPath string // empty disables persistent history
	SavePath    string // notebook written on exit, overwriting the destination
}

// Run starts an inline terminal console, or reads complete inputs line by line
// when in is not a terminal. Script input is never passed to cell programs.
func Run(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	if opts.Kernel == nil {
		return errors.New("console requires a kernel")
	}
	sess := &session{}
	var err error
	if f, ok := in.(*os.File); ok && term.IsTerminal(f.Fd()) {
		var h *history
		h, err = loadHistory(opts.HistoryPath)
		if err == nil {
			m := newModel(ctx, opts, sess, h)
			m.profile = colorprofile.Detect(out, os.Environ())
			// Keep runner color/image detection tied to the caller's real terminal.
			m.outputFD = ^uintptr(0)
			if f, ok := out.(interface{ Fd() uintptr }); ok {
				m.outputFD = f.Fd()
			}
			// ctx ends on SIGINT/SIGTERM (main's signal.NotifyContext);
			// Bubble Tea's own handler would race it and can block Run
			// forever, leaking the kernel workspace.
			p := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(out), tea.WithContext(ctx), tea.WithoutSignalHandler())
			_, err = p.Run()
			// A signal ends the console like ctrl+d, as it does the notebook.
			if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
				err = nil
			}
			m.stop() // join the worker before reading cells for --save
		}
	} else {
		err = runLines(ctx, opts, sess, in, out)
	}
	if opts.SavePath != "" {
		err = errors.Join(err, sess.save(opts.SavePath, true))
	}
	return err
}

func banner() string {
	return fmt.Sprintf("gopyter console · Go %s · enter runs complete input, alt+enter new line, ctrl+d exits · %%save [-f] file.ipynb", strings.TrimPrefix(runtime.Version(), "go"))
}

func runLines(ctx context.Context, opts Options, sess *session, in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 16*1024*1024)
	src := ""
	failed := 0
	tty := colorprofile.Detect(out, os.Environ()) >= colorprofile.ANSI
	submit := func() {
		if strings.TrimSpace(src) == "" {
			src = ""
			return
		}
		if text, ok, err := sess.command(src); ok {
			if err != nil {
				failed++
				text = "✗ " + err.Error()
			}
			_, _ = fmt.Fprintln(out, text) // output is best effort, as in runner
		} else {
			c := sess.add(strings.TrimSuffix(src, "\n"))
			_, _ = fmt.Fprintln(out, echo(c.Source, sess.count, colorprofile.NoTTY))
			if err := runner.RunCell(ctx, opts.Kernel, c, sess.count, out, nil, runner.Options{Images: tty, Redraw: tty}); err != nil {
				failed++
				_, _ = fmt.Fprintln(out, "✗ "+err.Error())
			}
		}
		src = ""
	}
	for scan.Scan() {
		if ctx.Err() != nil {
			break
		}
		// Retain the final newline: a blank line terminates %% cell magics.
		src += scan.Text() + "\n"
		if kernel.InputComplete(src) {
			submit()
		}
	}
	if ctx.Err() == nil && src != "" {
		submit()
	}
	if failed > 0 {
		return errors.Join(fmt.Errorf("%d input%s failed", failed, strings.Repeat("s", min(failed-1, 1))), scan.Err(), ctx.Err())
	}
	return errors.Join(scan.Err(), ctx.Err())
}
