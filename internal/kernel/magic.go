package kernel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cellMagic runs a cell magic: the cell (after its first line) is c.Input.
// Like programs, cell magics run in RunDir, so a program can read a file
// written with %%writefile by its relative path.
func (k *Kernel) cellMagic(ctx context.Context, c Command, emit func(Event)) error {
	fields, err := splitArgs(c.Text)
	if err != nil {
		return fmt.Errorf("%%%%%s: %w", c.Text, err)
	}
	name, args := fields[0], fields[1:]
	switch name {
	case "writefile":
		appendTo := len(args) > 0 && args[0] == "-a"
		if appendTo {
			args = args[1:]
		}
		if len(args) != 1 {
			return errors.New("%%writefile: expected [-a] <file>")
		}
		path := k.expandPath(args[0])
		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		verb := "wrote"
		if appendTo {
			flags, verb = os.O_WRONLY|os.O_CREATE|os.O_APPEND, "appended"
		}
		if err := writeFile(path, flags, c.Input); err != nil {
			return fmt.Errorf("%%%%writefile: %w", err)
		}
		emit(Event{Kind: Info, Text: fmt.Sprintf("%s %d bytes to %s", verb, len(c.Input), path)})
		return nil
	case "bash", "sh":
		return k.runScript(ctx, name, c.Input, emit)
	case "script":
		if len(args) == 0 {
			return errors.New("%%script: expected the command to run, e.g. %%script python3")
		}
		// The command is passed as written, so it can have its own flags.
		return k.runScript(ctx, strings.TrimSpace(strings.TrimPrefix(c.Text, name)), c.Input, emit)
	}
	return fmt.Errorf("unknown cell magic %%%%%s", name)
}

// runScript runs command with script as its standard input.
func (k *Kernel) runScript(ctx context.Context, command, script string, emit func(Event)) error {
	err := k.runShell(ctx, command, k.RunDir, strings.NewReader(script), emit)
	if err != nil && !errors.Is(err, ErrInterrupted) {
		return fmt.Errorf("%%%%%s: %w", command, err)
	}
	return err
}

func writeFile(path string, flags int, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// expandPath expands ~ and $VARS (including %env ones) in a path given to
// a magic, and makes it relative to RunDir.
func (k *Kernel) expandPath(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = home + p[1:]
		}
	}
	k.mu.Lock()
	p = os.Expand(p, func(name string) string {
		if v, ok := k.env[name]; ok {
			return v
		}
		return os.Getenv(name)
	})
	k.mu.Unlock()
	if !filepath.IsAbs(p) && k.RunDir != "" {
		p = filepath.Join(k.RunDir, p)
	}
	return p
}

// openCapture opens the file of a "%capture [-a] <file>" magic. ok
// reports whether c is one.
func (k *Kernel) openCapture(c Command) (f *os.File, ok bool, err error) {
	if !c.Magic || c.Cell {
		return nil, false, nil
	}
	args, err := splitArgs(c.Text)
	if err != nil || len(args) == 0 || args[0] != "capture" {
		return nil, false, nil // other magics report their own errors
	}
	args = args[1:]
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if len(args) > 0 && args[0] == "-a" {
		flags, args = os.O_WRONLY|os.O_CREATE|os.O_APPEND, args[1:]
	}
	if len(args) != 1 {
		return nil, true, errors.New("%capture: expected [-a] <file>")
	}
	path := k.expandPath(args[0])
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, true, fmt.Errorf("%%capture: %w", err)
	}
	f, err = os.OpenFile(path, flags, 0o644)
	if err != nil {
		return nil, true, fmt.Errorf("%%capture: %w", err)
	}
	return f, true, nil
}

// writeCapture copies an output event to a %capture file, as text.
func writeCapture(f *os.File, e Event) {
	text := e.Text
	switch e.Kind {
	case Stdout, Stderr:
	case Result, Markdown, Error:
		if !strings.HasSuffix(text, "\n") {
			text += "\n"
		}
	case Image:
		text = "[image]\n"
	default:
		return // kernel notes aren't the cell's output
	}
	// Best effort: a failing capture file must not fail the cell.
	_, _ = f.WriteString(text)
}

// resetGoMod recreates the workspace's go.mod, dropping every required
// module (for %reset go.mod).
func (k *Kernel) resetGoMod(ctx context.Context, emit func(Event)) error {
	for _, name := range []string{"go.mod", "go.sum"} {
		if err := os.Remove(filepath.Join(k.Dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if out, err := k.goCmd(ctx, "mod", "init", "gopyter.local/kernel").CombinedOutput(); err != nil {
		return fmt.Errorf("go mod init: %v: %s", err, out)
	}
	emit(Event{Kind: Info, Text: "go.mod reset"})
	return nil
}
