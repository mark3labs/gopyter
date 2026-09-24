package ui

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

func pngBase64(t *testing.T, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestImageOutput(t *testing.T) {
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{
		ID: "a", Type: notebook.Code, Source: "img", ExecutionCount: 1,
		Outputs: []notebook.Output{
			{Kind: notebook.Stdout, Text: "rendering\n"},
			{Kind: notebook.ImageOut, Text: pngBase64(t, 640, 480)},
		},
	}}}
	m := New(Options{Notebook: nb, Kernel: &kernel.Kernel{}})
	m.width, m.height = 100, 60
	c := m.cells[0]

	lines, resultAt := m.renderOutputs(c, 80)
	// 640×480 scaled to 80 columns: 80×60 pixels, two per line.
	if len(lines) != 1+30 || resultAt != 1 {
		t.Fatalf("got %d lines, result at %d", len(lines), resultAt)
	}
	for i, l := range lines[1:] {
		if w := ansi.StringWidth(l); w != 80 {
			t.Fatalf("image line %d is %d wide", i, w)
		}
		if !strings.Contains(l, "▀") || !strings.Contains(l, "\x1b[38;2;") {
			t.Fatalf("image line %d not drawn: %q", i, l)
		}
	}

	// The image is labelled as the cell's result and isn't folded.
	view := ansi.Strip(m.View().Content)
	if !strings.Contains(view, "Out[1]") || strings.Contains(view, "lines hidden") {
		t.Fatalf("view:\n%s", view)
	}
	// Copying outputs doesn't copy base64.
	if got := plainOutput(c); got != "rendering\n[image]" {
		t.Fatalf("plain output %q", got)
	}
}

func TestBrokenImageOutput(t *testing.T) {
	c := &Cell{outputs: []notebook.Output{{Kind: notebook.ImageOut, Text: "!!"}}}
	m := New(Options{Notebook: notebook.New()})
	lines, _ := m.renderOutputs(c, 80)
	if len(lines) != 1 || !strings.Contains(ansi.Strip(lines[0]), "can't show image") {
		t.Fatalf("lines %q", lines)
	}
}
