// Package console implements an inline REPL on the notebook kernel. Its
// scrollback-first behavior follows euporie's console (https://github.com/joouha/euporie).
package console

import (
	"fmt"
	"os"
	"strings"

	"github.com/mark3labs/gopyter/internal/notebook"
)

type session struct {
	nb    notebook.Notebook
	count int
}

func (s *session) add(src string) *notebook.Cell {
	s.count++
	c := &notebook.Cell{ID: notebook.NewID(), Type: notebook.Code, Source: src}
	s.nb.Cells = append(s.nb.Cells, c)
	return c
}

func (s *session) save(path string, force bool) error {
	if force {
		return s.nb.Save(path)
	}
	// Reserve the destination exclusively, so a concurrent creator is never
	// overwritten. Notebook.Save still performs the actual atomic write.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := s.nb.Save(path); err != nil {
		_ = os.Remove(path) // remove our reservation on failure
		return err
	}
	return nil
}

func saveCommand(src string) (path string, force, ok bool) {
	src = strings.TrimSpace(src)
	if strings.Contains(src, "\n") {
		return "", false, false
	}
	fields := strings.Fields(src)
	if len(fields) == 0 || fields[0] != "%save" {
		return "", false, false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(src, "%save"))
	if strings.HasPrefix(rest, "-f ") {
		force = true
		rest = strings.TrimSpace(rest[3:])
	}
	return rest, force, true
}

func (s *session) command(src string) (string, bool, error) {
	path, force, ok := saveCommand(src)
	if !ok {
		return "", false, nil
	}
	if path == "" {
		return "", true, fmt.Errorf("usage: %%save [-f] file.ipynb")
	}
	if err := s.save(path, force); err != nil {
		return "", true, err
	}
	return "› saved " + path, true, nil
}
