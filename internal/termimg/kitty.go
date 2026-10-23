package termimg

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"math"
	"strings"
)

// Placeholder is the Kitty Unicode image cell, followed by row/column marks.
const Placeholder = '\U0010EEEE'

// Drawer draws images as lines of text. A nil Drawer selects half blocks.
type Drawer interface {
	Draw(img image.Image, b64 string, maxCols, maxLines int) []string
}

// Draw uses d, or the portable half-block renderer when d is nil.
func Draw(d Drawer, img image.Image, b64 string, maxCols, maxLines int) []string {
	if d == nil {
		return Render(img, maxCols, maxLines)
	}
	return d.Draw(img, b64, maxCols, maxLines)
}

// Key identifies image data and the box of its virtual placement.
type Key struct {
	sum        [sha256.Size]byte
	cols, rows int
}

// Kitty draws images using Unicode placeholders. It is not concurrency safe;
// callers serialize drawing, retention and flushing on their event loop.
type Kitty struct {
	Tmux         bool
	Bits8        bool
	cellW, cellH int
	next         uint32
	images       map[Key]uint32
	pending      strings.Builder
}

// NewKitty creates a drawer using tmux passthrough and/or palette IDs.
func NewKitty(tmux, bits8 bool) *Kitty {
	return &Kitty{Tmux: tmux, Bits8: bits8, images: make(map[Key]uint32)}
}

// SetCellSize sets cell dimensions in pixels; zero means unknown.
// It reports changes so callers can invalidate size-dependent render caches.
func (k *Kitty) SetCellSize(w, h int) bool {
	if w <= 0 || h <= 0 {
		w, h = 0, 0
	}
	changed := w != k.cellW || h != k.cellH
	k.cellW, k.cellH = w, h
	return changed
}

// CellBox fits the image's natural pixel size to a box of terminal cells.
// Unknown cell dimensions use the same 2:1 box as half blocks.
func CellBox(img image.Image, maxCols, maxLines, cellW, cellH int) (cols, rows int) {
	if cellW <= 0 || cellH <= 0 {
		cols, px := Size(img, maxCols, maxLines)
		return cols, (px + 1) / 2
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	if w <= 0 || h <= 0 || maxCols <= 0 || maxLines <= 0 {
		return 0, 0
	}
	maxCols, maxLines = min(maxCols, len(diacritics)), min(maxLines, len(diacritics))
	cw, ch := float64(w)/float64(cellW), float64(h)/float64(cellH)
	scale := min(1, float64(maxCols)/cw, float64(maxLines)/ch)
	return min(maxCols, max(1, int(math.Ceil(cw*scale)))), min(maxLines, max(1, int(math.Ceil(ch*scale))))
}

// Draw implements Drawer, queuing transmission only for new data/box pairs.
func (k *Kitty) Draw(img image.Image, b64 string, maxCols, maxLines int) []string {
	return k.draw(img, b64, maxCols, maxLines, nil)
}

func (k *Kitty) draw(img image.Image, b64 string, maxCols, maxLines int, keys *[]Key) []string {
	cols, rows := CellBox(img, maxCols, maxLines, k.cellW, k.cellH)
	if cols == 0 || rows == 0 {
		return nil
	}
	if cols > len(diacritics) || rows > len(diacritics) {
		return Render(img, maxCols, maxLines)
	}
	data, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(b64), ""))
	if err != nil || !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return Render(img, maxCols, maxLines)
		}
		data = buf.Bytes()
	}
	key := Key{sha256.Sum256(data), cols, rows}
	id, ok := k.images[key]
	if !ok {
		id = k.allocate()
		if id == 0 {
			return Render(img, maxCols, maxLines)
		}
		k.images[key] = id
		payload := base64.StdEncoding.EncodeToString(data)
		first := true
		for len(payload) > 0 {
			n := min(4096, len(payload))
			more := 0
			if n < len(payload) {
				more = 1
			}
			header := fmt.Sprintf("m=%d", more)
			if first {
				header = fmt.Sprintf("a=t,f=100,t=d,i=%d,q=2,m=%d", id, more)
			}
			k.command("\x1b_G" + header + ";" + payload[:n] + "\x1b\\")
			first, payload = false, payload[n:]
		}
		k.command(fmt.Sprintf("\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows))
	}
	if keys != nil {
		*keys = append(*keys, key)
	}
	color := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16, (id>>8)&255, id&255)
	if k.Bits8 {
		color = fmt.Sprintf("\x1b[38;5;%dm", id)
	}
	lines := make([]string, rows)
	for y := range rows {
		var line strings.Builder
		line.WriteString(color)
		for x := range cols {
			line.WriteRune(Placeholder)
			line.WriteRune(diacritics[y])
			line.WriteRune(diacritics[x])
		}
		line.WriteString("\x1b[m")
		lines[y] = line.String()
	}
	return lines
}

func (k *Kitty) allocate() uint32 {
	if !k.Bits8 {
		if k.next >= 0xffffff {
			return 0
		}
		k.next++
		return k.next
	}
	used := [256]bool{}
	for _, id := range k.images {
		used[id] = true
	}
	for id := uint32(16); id <= 255; id++ {
		if !used[id] {
			return id
		}
	}
	return 0
}

type trackingDrawer struct {
	k    *Kitty
	keys *[]Key
}

func (d trackingDrawer) Draw(img image.Image, b64 string, cols, rows int) []string {
	return d.k.draw(img, b64, cols, rows, d.keys)
}

// Track records keys referenced by the returned drawer's lines.
func (k *Kitty) Track(keys *[]Key) Drawer { return trackingDrawer{k, keys} }

// Retain frees every image not referenced by keep.
func (k *Kitty) Retain(keep []Key) {
	live := make(map[Key]bool, len(keep))
	for _, key := range keep {
		live[key] = true
	}
	for key, id := range k.images {
		if !live[key] {
			k.command(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
			delete(k.images, key)
		}
	}
}

// DeleteAll frees all transmitted image data.
func (k *Kitty) DeleteAll() { k.Retain(nil) }

func (k *Kitty) command(cmd string) {
	if k.Tmux {
		cmd = "\x1bPtmux;" + strings.ReplaceAll(cmd, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	k.pending.WriteString(cmd)
}

// Pending reports whether Flush has commands to send.
func (k *Kitty) Pending() bool { return k.pending.Len() != 0 }

// Flush returns queued commands and clears the queue.
func (k *Kitty) Flush() string {
	s := k.pending.String()
	k.pending.Reset()
	return s
}
