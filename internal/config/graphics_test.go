package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateGraphics(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := UpdateAt(p, func(s *Settings) { s.Graphics = "blocks" }); err != nil {
		t.Fatal(err)
	}
	if s, err := LoadFrom(p); err != nil || s.Graphics != "blocks" {
		t.Fatal(s, err)
	}
	if err := UpdateAt(p, func(s *Settings) { s.Graphics = "" }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "graphics") {
		t.Fatal(string(data))
	}
}
