package runner

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
	"github.com/mark3labs/gopyter/internal/termimg"
)

func imageOutput(t *testing.T, size int) notebook.Output {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, size, size))); err != nil {
		t.Fatal(err)
	}
	return notebook.Output{Kind: notebook.ImageOut, Text: base64.StdEncoding.EncodeToString(b.Bytes())}
}

func TestRenderImageKitty(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	var out bytes.Buffer
	b := newBlocks(&out, true)
	b.gfx = termimg.NewKitty(false, false)
	b.showOutput("id:p", imageOutput(t, 4), true, true)
	s := out.String()
	end := strings.Index(s, "\x1b\\")
	if !strings.HasPrefix(s, "\x1b_Ga=t,") || end < 0 || strings.Contains(s[:end], "\x1b[") || !strings.ContainsRune(s[end:], termimg.Placeholder) {
		t.Fatalf("%q", s)
	}
}

func TestBlocksRedrawDeletesReplacedImages(t *testing.T) {
	var out bytes.Buffer
	b := newBlocks(&out, true)
	b.gfx = termimg.NewKitty(false, false)
	b.showOutput("id:p", imageOutput(t, 4), true, true)
	out.Reset()
	b.showOutput("id:p", imageOutput(t, 6), true, true)
	if !strings.Contains(out.String(), "a=d,d=I,i=1,q=2") {
		t.Fatal(out.String())
	}
	out.Reset()
	b.showOutput("id:q", imageOutput(t, 8), true, true)
	if strings.Contains(out.String(), "a=d") {
		t.Fatal("deleted scrollback")
	}
	b.finish()
	out.Reset()
	// The next cell inherits references, even after its first stream output.
	next := newBlocks(&out, true)
	next.gfx, next.savedImages = b.gfx, b.savedImages
	next.other()
	next.showOutput("id:r", imageOutput(t, 10), true, true)
	if strings.Contains(out.String(), "a=d") {
		t.Fatal("deleted previous cell images")
	}
}

func TestRunWithoutTerminalUsesBlocks(t *testing.T) {
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
	nb := &notebook.Notebook{Cells: []*notebook.Cell{{ID: "a", Type: notebook.Code, Source: `nb.Display(image.NewRGBA(image.Rect(0,0,4,4)))`}}}
	var out bytes.Buffer
	if failed := RunWith(t.Context(), k, nb, &out, nil, Options{Graphics: termimg.NewKitty(false, false)}); failed != 0 {
		t.Fatal(out.String())
	}
	if strings.Contains(out.String(), "\x1b_G") || strings.ContainsRune(out.String(), termimg.Placeholder) {
		t.Fatal("graphics leaked to redirected output")
	}
}
