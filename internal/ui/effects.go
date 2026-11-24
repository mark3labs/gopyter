package ui

import (
	"image"
	"image/color"
	"math"
	"strings"

	"bloomfx.dev/bloom/canvas"
	"bloomfx.dev/bloom/fx"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/mark3labs/gopyter/internal/termimg"
)

// The cell effects gopyter gives to bloom (https://bloomfx.dev). bloom's own
// effects recolor every cell they are given, which would break Kitty images:
// a placeholder's foreground color is its image ID, not a decorative color.
// The effects here run bloom's and put the images back as they were. They
// are plain structs so that bloom can compare them with == and skip frames
// that did not change.

// isImageCell reports whether c is part of a Kitty image.
func isImageCell(c *uv.Cell) bool {
	return c != nil && strings.HasPrefix(c.Content, string(termimg.Placeholder))
}

// dim fades the cells it is given toward the terminal's background, as the
// backdrop of a dialog or as a shadow: bloom's fx.Desaturate and fx.Dim,
// sparing images.
type dim struct {
	// Amount is how far colors move toward the background, from 0 to 1.
	Amount float64
	// Gray is how far colors move toward gray first, from 0 to 1, so the
	// backdrop recedes without losing which color is which.
	Gray float64
	// Steps is how many strengths the effect takes while its layer animates
	// in or out; zero is as many as there are frames. A backdrop covers the
	// whole screen, so every new strength repaints all of it: a few steps
	// keep that cheap on slow terminals and over SSH.
	Steps int
}

// Apply implements fx.Effect.
func (e dim) Apply(s *canvas.Surface, t float64) {
	if e.Steps > 0 && t < 1 {
		t = math.Ceil(t*float64(e.Steps)) / float64(e.Steps)
	}
	if t <= 0 {
		return
	}
	type kept struct {
		cell  *uv.Cell
		style uv.Style
	}
	var images []kept
	for _, c := range s.Cells() {
		if isImageCell(c) {
			images = append(images, kept{c, c.Style})
		}
	}
	fx.Desaturate{Amount: e.Gray}.Apply(s, t)
	fx.Dim{Amount: e.Amount}.Apply(s, t)
	for _, k := range images {
		k.cell.Style = k.style
	}
}

// fade crossfades a layer in over what is beneath it, as bloom's fx.Fade
// does: until halfway each cell shows the glyph beneath, fading out, and
// after it the layer's own, fading in. A glyph taken from an image beneath
// is blanked instead, as a placeholder in any other color is another image.
type fade struct{}

// Apply implements fx.Effect.
func (fade) Apply(s *canvas.Surface, t float64) {
	if t >= 1 {
		return
	}
	fx.Fade{}.Apply(s, t)
	for at, c := range s.Cells() {
		if isImageCell(c) && isImageCell(s.Under(at.X, at.Y)) {
			c.Content, c.Link = " ", uv.Link{}
			c.Style.Fg, c.Style.Attrs, c.Style.Underline = nil, 0, 0
		}
	}
}

// trailLength is how many cells the tail of a trail fades over.
const trailLength = 12

// trail runs a light around the border of a running cell, clockwise: Rect
// is the cell's box within the surface, and t the phase of the loop. It is
// bloom's fx.Comet for a box that is part of a node, and that may be cut off
// by the node's edges, as a cell scrolled half out of view is: the light
// keeps to the box, and is only drawn where the box is in sight.
type trail struct {
	Rect  image.Rectangle
	Color color.Color
}

// Apply implements fx.Effect.
func (e trail) Apply(s *canvas.Surface, t float64) {
	r := e.Rect
	w, h := r.Dx(), r.Dy()
	if w < 2 || h < 2 {
		return
	}
	n := 2*(w+h) - 4
	head := (t - math.Floor(t)) * float64(n)
	tail := float64(min(trailLength, n))
	light := func(x, y, i int) {
		if x < 0 || y < 0 || x >= s.Width() || y >= s.Height() || !s.Visible(x, y) {
			return
		}
		c := s.Cell(x, y)
		if c == nil || isImageCell(c) {
			return
		}
		behind := math.Mod(head-float64(i)+float64(n), float64(n))
		if k := 1 - behind/tail; k > 0 {
			c.Style.Fg = s.Mix(s.Fg(c), e.Color, k*k*(3-2*k))
		}
	}
	for x := range w {
		light(r.Min.X+x, r.Min.Y, x)
	}
	for y := 1; y < h; y++ {
		light(r.Max.X-1, r.Min.Y+y, w-1+y)
	}
	for x := w - 2; x >= 0; x-- {
		light(r.Min.X+x, r.Max.Y-1, w+h-2+w-1-x)
	}
	for y := h - 2; y > 0; y-- {
		light(r.Min.X, r.Min.Y+y, 2*w+h-3+h-1-y)
	}
}
