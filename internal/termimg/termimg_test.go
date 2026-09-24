package termimg

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSize(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for _, tc := range []struct{ maxCols, maxLines, cols, px int }{
		{1000, 1000, 640, 480}, // never scaled up
		{80, 1000, 80, 60},     // fit to width
		{80, 20, 53, 40},       // fit to height
		{0, 10, 0, 0},
	} {
		cols, px := Size(img, tc.maxCols, tc.maxLines)
		if cols != tc.cols || px != tc.px {
			t.Errorf("Size(%d, %d) = %d, %d; want %d, %d", tc.maxCols, tc.maxLines, cols, px, tc.cols, tc.px)
		}
	}
}

func TestRender(t *testing.T) {
	// 2×3: red over blue, green over transparent, then a transparent row.
	img := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	img.Set(0, 0, color.NRGBA{255, 0, 0, 255})
	img.Set(0, 1, color.NRGBA{0, 0, 255, 255})
	img.Set(1, 0, color.NRGBA{0, 255, 0, 255})
	img.Set(0, 2, color.NRGBA{0, 0, 255, 255})

	lines := Render(img, 10, 10)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2: %q", len(lines), lines)
	}
	want0 := "\x1b[38;2;255;0;0;48;2;0;0;255m▀\x1b[m\x1b[38;2;0;255;0m▀\x1b[m"
	if lines[0] != want0 {
		t.Errorf("line 0 = %q, want %q", lines[0], want0)
	}
	want1 := "\x1b[38;2;0;0;255m▀\x1b[m "
	if lines[1] != want1 {
		// The odd last row has no bottom pixel: the top is drawn alone.
		t.Errorf("line 1 = %q, want %q", lines[1], want1)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 2 {
			t.Errorf("line %d is %d columns wide", i, w)
		}
		// The last escape must reset, so nothing leaks past the line.
		if last := l[strings.LastIndex(l, "\x1b["):]; !strings.HasPrefix(last, "\x1b[m") {
			t.Errorf("line %d doesn't reset its attributes: %q", i, l)
		}
	}
}

func TestRenderScalesDown(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))
	for y := range 100 {
		for x := range 400 {
			img.Set(x, y, color.RGBA{uint8(x), 0, 0, 255})
		}
	}
	lines := Render(img, 40, 100)
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 40 {
			t.Errorf("line %d is %d columns wide, want 40", i, w)
		}
	}
}

func TestDecode(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 4, 5))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	b64 := base64.StdEncoding.EncodeToString(buf.Bytes())
	// Notebooks may wrap base64 data over lines.
	got, err := Decode(b64[:10] + "\n" + b64[10:])
	if err != nil {
		t.Fatal(err)
	}
	if Placeholder(got) != "[image 4x5]" {
		t.Fatalf("placeholder %q", Placeholder(got))
	}
	if _, err := Decode("not an image"); err == nil {
		t.Fatal("expected an error")
	}
}
