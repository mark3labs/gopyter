// Package nb is gopyter's notebook API: rich output and caching for cells.
//
//	nb.Display(img)                     // values, images
//	nb.DisplayMarkdown("**done**")
//	nb.DisplayID("progress", "3/10")    // replaced by the next call with this id
//	resp := nb.Cache("resp", fetch)     // computed once per kernel
//
// Outside gopyter, output falls back to plain text on stdout.
package nb

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mark3labs/gopyter/internal/kernel/runtime/gopyter/nb/wire"
)

// Display shows the given values as the cell's output. Images
// (image.Image) are drawn; other values are shown as text.
func Display(vs ...any) {
	var parts []string
	emitted := false
	flush := func() {
		if len(parts) > 0 {
			s := strings.Join(parts, ", ")
			wire.Display("text/plain", s, s, "")
			parts, emitted = nil, true
		}
	}
	for _, v := range vs {
		if img, ok := v.(image.Image); ok && !isNil(v) {
			flush()
			displayImage(img, "")
			emitted = true
			continue
		}
		parts = append(parts, fmt.Sprintf("%+v", v))
	}
	flush()
	if !emitted {
		wire.Display("text/plain", "", "", "")
	}
}

// DisplayID is like Display, but the output can be updated: calling it
// again with the same id replaces what the earlier call showed, e.g. to
// animate an image or show progress. A single image is drawn; other values
// are shown as text.
func DisplayID(id string, vs ...any) {
	if len(vs) == 1 {
		if img, ok := vs[0].(image.Image); ok && !isNil(vs[0]) {
			displayImage(img, id)
			return
		}
	}
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = fmt.Sprintf("%+v", v)
	}
	s := strings.Join(parts, ", ")
	wire.Display("text/plain", s, s, id)
}

// DisplayMarkdown renders markdown in the cell's output.
func DisplayMarkdown(s string) { wire.Display("text/markdown", s, s, "") }

// DisplayMarkdownID is like DisplayMarkdown, but calling it again with
// the same id replaces the earlier output (see DisplayID).
func DisplayMarkdownID(id, s string) { wire.Display("text/markdown", s, s, id) }

// DisplayPNG shows PNG encoded image data.
func DisplayPNG(data []byte) {
	wire.Display("image/png", base64.StdEncoding.EncodeToString(data), "[image/png]", "")
}

// isNil reports a nil pointer stored in an interface, like a nil
// *image.RGBA, whose methods would panic.
func isNil(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func displayImage(img image.Image, id string) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: can't display image: %v\n", err)
		return
	}
	wire.Display("image/png", base64.StdEncoding.EncodeToString(buf.Bytes()), "[image/png]", id)
}

// Cache returns the value stored under key, calling fn and storing its
// result (gob-encoded, in the kernel workspace) only the first time. Every
// cell is a fresh process, so this is how an expensive global is computed
// once: var resp = nb.Cache("resp", func() T { ... }).
// Unexported struct fields are not saved. Use %cache clear to recompute.
func Cache[T any](key string, fn func() T) T {
	var v T
	if cacheLoad(key, &v) {
		return v
	}
	v = fn()
	cacheStore(key, v)
	return v
}

// CacheErr is like Cache for functions that can fail: a result is stored
// only when fn returns a nil error, so failures are retried next time.
func CacheErr[T any](key string, fn func() (T, error)) (T, error) {
	var v T
	if cacheLoad(key, &v) {
		return v, nil
	}
	v, err := fn()
	if err == nil {
		cacheStore(key, v)
	}
	return v, err
}

func cachePath(key string) string {
	dir := os.Getenv("GOPYTER_CACHE_DIR")
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, url.PathEscape(key)+".gob")
}

func cacheLoad(key string, v any) bool {
	path := cachePath(key)
	if path == "" {
		return false
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	if err := gob.NewDecoder(f).Decode(v); err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: cache %q unreadable, recomputing: %v\n", key, err)
		return false
	}
	return true
}

func cacheStore(key string, v any) {
	path := cachePath(key)
	if path == "" {
		return
	}
	var buf bytes.Buffer
	err := gob.NewEncoder(&buf).Encode(v)
	if err == nil {
		err = writeFile(path, buf.Bytes())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gopyter: cache %q not saved: %v\n", key, err)
	}
}

// writeFile writes then renames, so an interrupted run never leaves a
// torn file.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
