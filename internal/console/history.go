package console

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const historyLimit = 1000

type history struct {
	entries     []string
	pos         int
	draft, path string
}

func loadHistory(path string) (*history, error) {
	h := &history{path: path}
	if path == "" {
		return h, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return h, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // read-only file
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 16*1024*1024)
	for scan.Scan() {
		var src string
		if err := json.Unmarshal(scan.Bytes(), &src); err != nil {
			return nil, err
		}
		if len(h.entries) == 0 || src != h.entries[len(h.entries)-1] {
			h.entries = append(h.entries, src)
		}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	if len(h.entries) > historyLimit {
		h.entries = h.entries[len(h.entries)-historyLimit:]
		if err := h.rewrite(); err != nil {
			return nil, err
		}
	}
	h.pos = len(h.entries)
	return h, nil
}

func (h *history) rewrite() error {
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // explicit close below reports errors
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	for _, src := range h.entries {
		if err := enc.Encode(src); err != nil {
			return err
		}
	}
	return f.Close()
}

func (h *history) add(src string) error {
	h.pos = len(h.entries)
	h.draft = ""
	if len(h.entries) > 0 && h.entries[len(h.entries)-1] == src {
		return nil
	}
	h.entries = append(h.entries, src)
	h.pos = len(h.entries)
	if h.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(h.path), 0o700); err != nil {
		return err
	}
	if len(h.entries) > historyLimit {
		h.entries = h.entries[len(h.entries)-historyLimit:]
		h.pos = len(h.entries)
		return h.rewrite()
	}
	f, err := os.OpenFile(h.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // explicit close below reports errors
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	if err := json.NewEncoder(f).Encode(src); err != nil {
		return err
	}
	return f.Close()
}

func (h *history) prev(cur string) (string, bool) {
	if h.pos == 0 {
		return cur, false
	}
	if h.pos == len(h.entries) {
		h.draft = cur
	}
	h.pos--
	return h.entries[h.pos], true
}
func (h *history) next() (string, bool) {
	if h.pos >= len(h.entries) {
		return "", false
	}
	h.pos++
	if h.pos == len(h.entries) {
		return h.draft, true
	}
	return h.entries[h.pos], true
}
