package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"
	"unicode"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func source(t *testing.T, img image.Image) string {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(b.Bytes())
}

func TestKittyPlaceholders(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	k := NewKitty(false, false)
	lines := k.Draw(img, source(t, img), 80, 40)
	if len(lines) != 3 {
		t.Fatal(len(lines))
	}
	for y, line := range lines {
		want := "\x1b[38;2;0;0;1m"
		for x := range 4 {
			want += string([]rune{Placeholder, diacritics[y], diacritics[x]})
		}
		want += "\x1b[m"
		if line != want || ansi.StringWidth(line) != 4 {
			t.Fatalf("line %q", line)
		}
	}
	cmds := k.Flush()
	if strings.Count(cmds, "a=t,") != 1 || !strings.Contains(cmds, "a=p,U=1,i=1,c=4,r=3,q=2") {
		t.Fatal(cmds)
	}
	k.Draw(img, source(t, img), 80, 40)
	if k.Flush() != "" {
		t.Fatal("retransmission")
	}
	k.Draw(img, source(t, img), 2, 40)
	if !strings.Contains(k.Flush(), "a=t,f=100,t=d,i=2,q=2") {
		t.Fatal("new box not sent")
	}
}

func TestKittyChunks(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 300))
	rng := rand.New(rand.NewPCG(1, 2))
	for y := range 300 {
		for x := range 300 {
			img.SetRGBA(x, y, color.RGBA{uint8(rng.Uint32()), uint8(rng.Uint32()), uint8(rng.Uint32()), 255})
		}
	}
	b64 := source(t, img)
	k := NewKitty(false, false)
	k.Draw(img, b64, 80, 40)
	var payload strings.Builder
	chunks := strings.Split(k.Flush(), "\x1b_G")
	count := 0
	for _, cmd := range chunks[1:] {
		if strings.HasPrefix(cmd, "a=p") {
			continue
		}
		header, data, ok := strings.Cut(strings.TrimSuffix(cmd, "\x1b\\"), ";")
		if !ok || len(data) > 4096 || len(data)%4 != 0 {
			t.Fatalf("bad chunk %s (%d)", header, len(data))
		}
		if count > 0 && header != "m=1" && header != "m=0" {
			t.Fatal(header)
		}
		payload.WriteString(data)
		if strings.HasSuffix(header, "m=0") && payload.Len() != len(b64) {
			t.Fatal("early final chunk")
		}
		count++
	}
	if count < 2 || payload.String() != b64 {
		t.Fatal("payload changed")
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	k = NewKitty(false, false)
	k.Draw(img, base64.StdEncoding.EncodeToString(jpg.Bytes()), 80, 40)
	_, data, _ := strings.Cut(k.Flush(), ";")
	decoded, err := base64.StdEncoding.DecodeString(strings.Split(data, "\x1b\\")[0])
	if err != nil || !bytes.HasPrefix(decoded, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatal("JPEG not converted")
	}
}

func TestKitty8BitIDs(t *testing.T) {
	k := NewKitty(false, true)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	for i := range 240 {
		img.SetRGBA(0, 0, color.RGBA{uint8(i), 0, 0, 255})
		line := k.Draw(img, source(t, img), 1, 1)[0]
		if !strings.HasPrefix(line, fmt.Sprintf("\x1b[38;5;%dm", 16+i)) {
			t.Fatal(line)
		}
	}
	img.SetRGBA(0, 0, color.RGBA{255, 0, 0, 255})
	if strings.Contains(k.Draw(img, source(t, img), 1, 1)[0], string(Placeholder)) {
		t.Fatal("exhausted ids did not fall back")
	}
	k.Retain(nil)
	if !strings.HasPrefix(k.Draw(img, source(t, img), 1, 1)[0], "\x1b[38;5;16m") {
		t.Fatal("no recycling")
	}
}

func TestKittyRetainDeletes(t *testing.T) {
	k := NewKitty(false, false)
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	var keep []Key
	k.Track(&keep).Draw(img, source(t, img), 4, 3)
	k.Draw(img, source(t, img), 2, 3)
	k.Flush()
	k.Retain(keep)
	if got := k.Flush(); got != "\x1b_Ga=d,d=I,i=2,q=2\x1b\\" {
		t.Fatal(got)
	}
	k.Draw(img, source(t, img), 2, 3)
	if !strings.Contains(k.Flush(), "i=3,q=2") {
		t.Fatal("reused 24-bit id")
	}
}

func TestKittyTmuxPassthrough(t *testing.T) {
	k := NewKitty(true, false)
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	lines := k.Draw(img, source(t, img), 4, 3)
	if strings.Contains(lines[0], "tmux") {
		t.Fatal("wrapped text")
	}
	s := k.Flush()
	if strings.Count(s, "\x1bPtmux;") != 2 || !strings.Contains(s, "\x1b\x1b_Ga=p") || strings.Count(s, "\x1b\x1b\\\x1b\\") != 2 {
		t.Fatalf("%q", s)
	}
}

func TestCellBox(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for _, tc := range []struct{ cols, rows, cw, ch, w, h int }{
		{80, 40, 8, 16, 8, 4}, {4, 40, 8, 16, 4, 2}, {80, 2, 8, 16, 4, 2}, {80, 40, 0, 0, 64, 32},
	} {
		w, h := CellBox(img, tc.cols, tc.rows, tc.cw, tc.ch)
		if w != tc.w || h != tc.h {
			t.Fatalf("%+v: %dx%d", tc, w, h)
		}
	}
	k := NewKitty(false, false)
	if lines := k.Draw(image.NewRGBA(image.Rect(0, 0, 0, 0)), "", 80, 40); len(lines) != 0 || k.Pending() {
		t.Fatal("empty image")
	}
	big := image.NewRGBA(image.Rect(0, 0, 400, 400))
	if strings.Contains(k.Draw(big, "", 400, 400)[0], string(Placeholder)) {
		t.Fatal("oversized box")
	}
	w, h := CellBox(big, 1000, 1000, 1, 1)
	if w > 297 || h > 297 {
		t.Fatal(w, h)
	}
}

func TestDiacritics(t *testing.T) {
	if len(diacritics) != 297 || diacritics[0] != 0x305 || diacritics[296] != 0x1d244 {
		t.Fatal("incorrect table")
	}
	for _, r := range diacritics {
		if !unicode.Is(unicode.Mn, r) {
			t.Fatalf("%U", r)
		}
	}
}

func TestPlaceholderCellsInUV(t *testing.T) {
	k := NewKitty(false, false)
	img := image.NewRGBA(image.Rect(0, 0, 4, 6))
	line := k.Draw(img, source(t, img), 4, 3)[0]
	for _, width := range []int{4, 2} {
		buf := uv.NewScreenBuffer(width, 1)
		uv.NewStyledString(line).Draw(buf, buf.Bounds())
		for x := range width {
			c := buf.CellAt(x, 0)
			if c.Width != 1 || c.Content != string([]rune{Placeholder, diacritics[0], diacritics[x]}) {
				t.Fatalf("%+v", c)
			}
			r, g, b, _ := c.Style.Fg.RGBA()
			if r != 0 || g != 0 || b != 257 {
				t.Fatalf("id color %v", c.Style.Fg)
			}
		}
	}
}
