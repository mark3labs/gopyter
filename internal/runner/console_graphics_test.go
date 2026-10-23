package runner

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

// Console images must remain width-limited half blocks without redraws,
// even after notebook runs gain Kitty image tracking.
func TestRunCellImagesWithoutRedraw(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	k, err := kernel.New("")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := k.Close(); err != nil {
			t.Error(err)
		}
	})
	c := &notebook.Cell{ID: "image", Type: notebook.Code, Source: `
img := image.NewRGBA(image.Rect(0, 0, 80, 40))
nb.DisplayID("p", img)
nb.DisplayID("p", img)`}
	var out bytes.Buffer
	if err := RunCell(t.Context(), k, c, 1, &out, nil, Options{Images: true, Width: 20}); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if strings.Contains(s, "\x1b_G") || strings.ContainsRune(s, termimg.Placeholder) || strings.Contains(s, "\x1b[J") {
		t.Fatalf("console emitted graphics or redraw controls: %q", s)
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	if len(lines) != 5 {
		t.Fatalf("expected one final 20x5 image, got %d lines: %q", len(lines), s)
	}
	for _, line := range lines {
		if width := ansi.StringWidth(line); width != 20 {
			t.Fatalf("image width = %d, want 20: %q", width, line)
		}
	}
}
