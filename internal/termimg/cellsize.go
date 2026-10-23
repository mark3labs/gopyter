package termimg

import (
	"github.com/creack/pty"
	"os"
)

// CellSize reads pixel dimensions from the OS, never from terminal stdin.
// Terminals which omit pixel dimensions use the portable 2:1 box.
func CellSize(f *os.File) (w, h int) {
	size, err := pty.GetsizeFull(f)
	if err != nil || size.Cols == 0 || size.Rows == 0 {
		return 0, 0
	}
	return int(size.X) / int(size.Cols), int(size.Y) / int(size.Rows)
}
