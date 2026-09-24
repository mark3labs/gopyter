// Package termimg draws images as text: each terminal cell shows two
// vertically stacked pixels with the upper half block "▀" (foreground = top
// pixel, background = bottom pixel). It works in any terminal with color
// support and, unlike graphics protocols (kitty, sixel, iTerm2), the result
// is ordinary styled text, so it scrolls, clips and composites with the rest
// of the UI.
package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // registered for image.Decode
	_ "image/jpeg" // registered for image.Decode
	_ "image/png"  // registered for image.Decode
	"strconv"
	"strings"
)

// Decode decodes a base64 encoded image (PNG, JPEG or GIF), the form used
// by notebook outputs.
func Decode(b64 string) (image.Image, error) {
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(b64), ""))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// Size returns the number of columns and pixel rows (two per text line)
// img is drawn at: its natural size, scaled down to fit maxCols columns and
// maxLines lines while keeping its aspect ratio. Terminal cells are about
// twice as tall as wide, so half-block pixels are roughly square.
func Size(img image.Image, maxCols, maxLines int) (cols, px int) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 || maxCols <= 0 || maxLines <= 0 {
		return 0, 0
	}
	cols, px = w, h
	if cols > maxCols {
		cols = maxCols
		px = max(1, (h*cols+w/2)/w)
	}
	if px > 2*maxLines {
		px = 2 * maxLines
		cols = max(1, (w*px+h/2)/h)
	}
	return cols, px
}

// Render draws img in at most maxCols columns and maxLines lines. Each
// line is self-contained: it ends by resetting its attributes. Transparent
// pixels are left at the terminal's default background.
func Render(img image.Image, maxCols, maxLines int) []string {
	cols, px := Size(img, maxCols, maxLines)
	if cols == 0 {
		return nil
	}
	pix := scale(img, cols, px)
	at := func(x, y int) (color.RGBA, bool) {
		if y >= px {
			return color.RGBA{}, false
		}
		c := pix[y*cols+x]
		return c, c.A != 0
	}
	lines := make([]string, 0, (px+1)/2)
	for y := 0; y < px; y += 2 {
		var sb strings.Builder
		var st sgrState
		for x := range cols {
			top, hasTop := at(x, y)
			bot, hasBot := at(x, y+1)
			switch {
			case hasTop && hasBot:
				st.set(&sb, &top, &bot)
				sb.WriteString("▀")
			case hasTop:
				st.set(&sb, &top, nil)
				sb.WriteString("▀")
			case hasBot:
				st.set(&sb, &bot, nil)
				sb.WriteString("▄")
			default:
				st.set(&sb, nil, nil)
				sb.WriteString(" ")
			}
		}
		st.set(&sb, nil, nil)
		lines = append(lines, sb.String())
	}
	return lines
}

// Placeholder describes img in plain text, for outputs that can't show it.
func Placeholder(img image.Image) string {
	b := img.Bounds()
	return fmt.Sprintf("[image %dx%d]", b.Dx(), b.Dy())
}

// sgrState tracks the colors in effect so only changes are written, which
// keeps lines (re-parsed on every frame) short.
type sgrState struct {
	fg, bg *color.RGBA
}

func (s *sgrState) set(sb *strings.Builder, fg, bg *color.RGBA) {
	if same(s.fg, fg) && same(s.bg, bg) {
		return
	}
	if (s.fg != nil && fg == nil) || (s.bg != nil && bg == nil) {
		sb.WriteString("\x1b[m")
		s.fg, s.bg = nil, nil
	}
	var params []string
	if fg != nil && !same(s.fg, fg) {
		params = append(params, "38;2;"+rgb(*fg))
	}
	if bg != nil && !same(s.bg, bg) {
		params = append(params, "48;2;"+rgb(*bg))
	}
	if len(params) > 0 {
		sb.WriteString("\x1b[")
		sb.WriteString(strings.Join(params, ";"))
		sb.WriteString("m")
	}
	s.fg, s.bg = fg, bg
}

func same(a, b *color.RGBA) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func rgb(c color.RGBA) string {
	return strconv.Itoa(int(c.R)) + ";" + strconv.Itoa(int(c.G)) + ";" + strconv.Itoa(int(c.B))
}

// scale resamples img to w×h pixels, averaging the source pixels each
// target pixel covers (a box filter), so shrinking doesn't alias. Mostly
// transparent pixels come back with A == 0, the rest are opaque.
func scale(img image.Image, w, h int) []color.RGBA {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	out := make([]color.RGBA, w*h)
	for y := range h {
		y0 := b.Min.Y + y*sh/h
		y1 := max(b.Min.Y+(y+1)*sh/h, y0+1)
		for x := range w {
			x0 := b.Min.X + x*sw/w
			x1 := max(b.Min.X+(x+1)*sw/w, x0+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					n++
				}
			}
			if a < n*0x8000 {
				continue // mostly transparent
			}
			// The sums are alpha-premultiplied: divide by alpha to get the
			// color itself.
			out[y*w+x] = color.RGBA{
				R: uint8(r * 0xff / a), G: uint8(g * 0xff / a), B: uint8(bl * 0xff / a), A: 0xff,
			}
		}
	}
	return out
}
