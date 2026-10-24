package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/gopyter/internal/kernel"
)

func TestSaveAsResolvePath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "ok.ipynb")
	m := New(Options{ResolvePath: func(p string) (string, error) {
		if p != "ok.ipynb" {
			return "", fmt.Errorf("outside notebook directory")
		}
		return target, nil
	}})
	m.openSaveAs()
	m.input.SetValue("../bad")
	m.saveAsConfirm()
	if m.overlay != overlaySaveAs || m.path != "" || m.statusKind != statusError || !m.input.Focused() {
		t.Fatalf("bad path accepted: %+v", m)
	}
	m.input.SetValue("ok")
	m.saveAsConfirm()
	if m.path != target || m.overlay != overlayNone {
		t.Fatalf("valid path rejected: %s %s", m.path, m.status)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}

func TestFixedThemeDisablesPicker(t *testing.T) {
	if err := UseTheme(DefaultTheme, true); err != nil {
		t.Fatal(err)
	}
	before := colPrimary
	m := New(Options{FixedTheme: true, Theme: "dracula"})
	if colPrimary != before {
		t.Fatal("New changed shared palette")
	}
	m.mode = modeCommand
	m.handleKey(press('T', 0))
	if m.overlay == overlayTheme {
		t.Fatal("keyboard opened theme picker")
	}
	m.doAction(action{kind: actTheme})
	if m.overlay == overlayTheme || colPrimary != before {
		t.Fatal("mouse opened theme picker or changed palette")
	}
	if m.status != "the theme is set by gopyter serve (--theme, --light)" {
		t.Fatal(m.status)
	}
}

func TestShutdownCancelsRunningCell(t *testing.T) {
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	m := New(Options{Kernel: k})
	m.cur().ed.SetValue("fmt.Println(\"started\"); for {}")
	m.queue = []string{m.cur().id}
	m.startNext()
	rs := m.running
	if rs == nil {
		t.Fatal("cell did not start")
	}
	started := time.NewTimer(20 * time.Second)
	defer started.Stop()
	for {
		select {
		case e, ok := <-rs.ch:
			if !ok {
				t.Fatal("cell exited before shutdown")
			}
			if strings.Contains(e.Text, "started") {
				goto running
			}
		case <-started.C:
			rs.cancel()
			t.Fatal("cell did not reach running program")
		}
	}
running:
	done := make(chan struct{})
	go func() { m.shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("shutdown did not cancel execution")
	}
	if _, ok := <-rs.ch; ok {
		t.Fatal("run channel still open")
	}
}

// TestConcurrentSessionsNonDefaultTheme is a regression test: sessions
// built at once with a derived-style theme (most themes) wrote chroma's
// style registry concurrently, crashing gopyter serve. Run with -race.
func TestConcurrentSessionsNonDefaultTheme(t *testing.T) {
	if err := UseTheme("dracula", true); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UseTheme(DefaultTheme, true) })
	done := make(chan struct{})
	for range 8 {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 5 {
				m := New(Options{FixedTheme: true, Theme: "dracula", Kernel: &kernel.Kernel{}})
				m.width, m.height = 80, 24
				m.renderScreen()
			}
		}()
	}
	for range 8 {
		<-done
	}
}

// TestSaveCreatesDirectory is a regression test: a new notebook in a
// missing subdirectory (ssh … sub/new.ipynb) couldn't be saved.
func TestSaveCreatesDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "new.ipynb")
	m := New(Options{Path: path})
	if err := m.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
