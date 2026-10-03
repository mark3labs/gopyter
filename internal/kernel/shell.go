package kernel

// Shell commands ("!cmd", "%%bash") run in a pseudo-terminal when the
// front end gives them one (SetTerminal), so programs see the terminal
// a user expects: they colorize their output and draw progress bars and
// spinners with carriage returns and cursor motion. The front end
// replays that (see internal/ui/termout.go), which is what makes a long
// command show its progress while it runs. This follows what crush does
// for its bang mode (https://github.com/charmbracelet/crush).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// ptyHeight is the height reported to programs. The output isn't cut
	// to it, but programs that page or divide by it expect a plausible
	// one.
	ptyHeight = 24
	// ptyMinWidth is the narrowest terminal worth giving a command.
	ptyMinWidth = 20
)

// colorVars ask for colored output. A terminal is enough for most
// programs, these cover the ones that only read the environment, and
// systems without a pseudo-terminal (Windows).
var colorVars = []string{
	"TERM=xterm-256color",
	"COLORTERM=truecolor",
	"CLICOLOR_FORCE=1",
	"FORCE_COLOR=1",
}

// errNoPTY reports that this system has no pseudo-terminal to give the
// command, which then runs on pipes instead.
var errNoPTY = errors.New("no pseudo-terminal")

// SetTerminal makes shell commands run as if they had a terminal: they
// get a pseudo-terminal width columns wide and the environment that asks
// for color, so their output arrives as the ANSI a program writes for a
// terminal. The UI calls it with the width it draws outputs at, so
// programs wrap where the output is shown. A width below ptyMinWidth
// turns it off, which is what a front end writing to a file wants
// (`gopyter run`): its output would only be full of escape codes.
func (k *Kernel) SetTerminal(width int) {
	if width < ptyMinWidth {
		width = 0
	}
	k.termWidth.Store(int64(min(width, math.MaxUint16)))
}

// terminalWidth is the width of the terminal shell commands run in, or 0
// when they run on pipes.
func (k *Kernel) terminalWidth() int { return int(k.termWidth.Load()) }

// shellEnv is the environment of a shell command: the kernel's, plus the
// variables that ask for color when the front end has a terminal.
func (k *Kernel) shellEnv() []string {
	env := append(k.environ(), "GOWORK=off")
	if k.terminalWidth() == 0 {
		return env
	}
	// os/exec keeps the last value of a repeated variable, so a value
	// from the user (or %env) wins over the ones added here.
	set := make(map[string]bool, len(env))
	for _, e := range env {
		name, _, _ := strings.Cut(e, "=")
		set[name] = true
	}
	if set["NO_COLOR"] {
		return env
	}
	for _, v := range colorVars {
		if name, _, _ := strings.Cut(v, "="); !set[name] {
			env = append(env, v)
		}
	}
	return env
}

// runShell runs script with the system shell in dir, in a terminal when
// there is one.
func (k *Kernel) runShell(ctx context.Context, script, dir string, stdin io.Reader, emit func(Event)) error {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", script)
	}
	cmd.Dir = dir
	cmd.Env = k.shellEnv()
	cmd.Stdin = stdin
	cmd.WaitDelay = 2 * time.Second
	if w := k.terminalWidth(); w > 0 {
		// Only a missing pseudo-terminal falls through to the pipes;
		// any other error is the command's.
		if err := runShellPTY(ctx, cmd, w, emit); !errors.Is(err, errNoPTY) {
			return err
		}
	}
	return runShellPipes(ctx, cmd, emit)
}

// runShellPipes runs cmd with its output on pipes, one per stream.
func runShellPipes(ctx context.Context, cmd *exec.Cmd, emit func(Event)) error {
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pump(stdout, Stdout, emit) }()
	go func() { defer wg.Done(); pump(stderr, Stderr, emit) }()
	wg.Wait()
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ErrInterrupted
		}
		return err
	}
	return nil
}

// runShellPTY runs cmd attached to a pseudo-terminal, streaming what it
// writes as the single output stream a terminal shows. Standard input
// stays the pipe the front end feeds: what the user types is echoed by
// the front end and not by the terminal, and a command that reads a
// prompt doesn't have to cope with a terminal's line editing.
func runShellPTY(ctx context.Context, cmd *exec.Cmd, width int, emit func(Event)) error {
	master, slave, err := pty.Open()
	if err != nil {
		return errors.Join(errNoPTY, err)
	}
	closeMaster := sync.OnceFunc(func() { _ = master.Close() })
	defer closeMaster()
	if err := pty.Setsize(master, &pty.Winsize{Rows: ptyHeight, Cols: uint16(width)}); err != nil {
		_ = slave.Close()
		return errors.Join(errNoPTY, err)
	}
	cmd.Stdout, cmd.Stderr = slave, slave
	if cmd.Stdin == nil {
		// Without this the command would read from the terminal, where
		// nothing is ever typed and reads would block.
		cmd.Stdin = strings.NewReader("")
	}
	if err := cmd.Start(); err != nil {
		_ = slave.Close()
		return err
	}
	// Dropping the parent's terminal makes the read below end when the
	// command and everything it left behind are gone.
	_ = slave.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		pumpTerminal(master, Stdout, emit)
	}()
	err = cmd.Wait()
	// Drain the terminal before closing the master: the shell may exit
	// while its output is still buffered, and closing here loses it.
	// Reading ends when the last process holding the slave is gone;
	// background jobs keep the cell waiting, just as they do on pipes.
	<-done
	if err != nil && ctx.Err() != nil {
		return ErrInterrupted
	}
	return err
}

// pump streams r as one output stream.
func pump(r io.Reader, kind EventKind, emit func(Event)) {
	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			emit(Event{Kind: kind, Text: string(bytes.Clone(buf[:n]))})
		}
		if err != nil {
			return
		}
	}
}

// pumpTerminal is pump for a terminal, which ends every line with
// "\r\n". The carriage returns are dropped: after a newline they mean
// nothing, and they'd be saved with the output.
func pumpTerminal(r io.Reader, kind EventKind, emit func(Event)) {
	buf := make([]byte, 32*1024)
	held := false // the last chunk ended with a carriage return
	for {
		n, err := r.Read(buf)
		if n > 0 {
			s := string(bytes.Clone(buf[:n]))
			if held {
				held = false
				if !strings.HasPrefix(s, "\n") {
					emit(Event{Kind: kind, Text: "\r"}) // it was not part of a pair
				}
			}
			if strings.HasSuffix(s, "\r") {
				// Hold it back: the next chunk may start with the
				// newline of the pair it belongs to.
				s, held = s[:len(s)-1], true
			}
			s = strings.ReplaceAll(s, "\r\n", "\n")
			if s != "" {
				emit(Event{Kind: kind, Text: s})
			}
		}
		if err != nil {
			if held {
				emit(Event{Kind: kind, Text: "\r"}) // it was not part of a pair
			}
			return
		}
	}
}
