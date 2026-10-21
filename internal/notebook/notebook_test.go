package notebook

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImageOutputs(t *testing.T) {
	const pngData = "iVBORw0KGgoAAAANSUhEUg==" // PNG signature
	const jpegData = "/9j/4AAQSkZJRg=="        // JPEG SOI
	// Jupyter may split base64 over lines.
	src := `{"cells": [{"cell_type": "code", "id": "a", "metadata": {}, "source": "", "execution_count": 1, "outputs": [
		{"output_type": "display_data", "metadata": {}, "data": {"image/png": ["iVBORw0KGgoAAAAN\n", "SUhEUg==\n"], "text/plain": "<Figure>"}},
		{"output_type": "display_data", "metadata": {}, "data": {"image/jpeg": "` + jpegData + `"}}
	]}], "metadata": {}, "nbformat": 4, "nbformat_minor": 5}`
	nb, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	outs := nb.Cells[0].Outputs
	if len(outs) != 2 || outs[0] != (Output{Kind: ImageOut, Text: pngData}) || outs[1] != (Output{Kind: ImageOut, Text: jpegData}) {
		t.Fatalf("bad outputs: %+v", outs)
	}
	b, err := nb.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"image/png"`, `"image/jpeg"`, `"[image]"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in:\n%s", want, b)
		}
	}
	again, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Cells[0].Outputs; len(got) != 2 || got[0] != outs[0] || got[1] != outs[1] {
		t.Fatalf("bad round trip: %+v", got)
	}
}

func TestRoundTrip(t *testing.T) {
	nb := &Notebook{Cells: []*Cell{
		{ID: "a", Type: Markdown, Source: "# Title\ntext"},
		{ID: "b", Type: Code, Source: "x := 1\nx", ExecutionCount: 3, Outputs: []Output{
			{Kind: Stdout, Text: "hello\nworld\n"},
			{Kind: Result, Text: "1"},
			{Kind: MarkdownOut, Text: "**bold**"},
			{Kind: Error, Text: "In[3]:1:1: boom"},
		}},
		{ID: "c", Type: Code},
	}}
	b, err := nb.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{`"name": "gonb"`, `"execution_count": 3`, `"execution_count": null`, `"text/markdown"`} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %s in:\n%s", want, s)
		}
	}
	got, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Cells) != 3 || got.Cells[0].Source != "# Title\ntext" || got.Cells[1].ExecutionCount != 3 {
		t.Fatalf("bad round trip: %+v", got.Cells)
	}
	outs := got.Cells[1].Outputs
	if len(outs) != 4 || outs[0].Text != "hello\nworld\n" || outs[1].Kind != Result || outs[2].Kind != MarkdownOut || outs[3].Kind != Error {
		t.Fatalf("bad outputs: %+v", outs)
	}
}

func TestSaveKeepsMode(t *testing.T) {
	for _, mode := range []os.FileMode{0o600, 0o640} {
		path := filepath.Join(t.TempDir(), "test.ipynb")
		if err := os.WriteFile(path, []byte("old"), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := New().Save(path); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Errorf("mode = %o, want %o", info.Mode().Perm(), mode)
		}
	}
}

func TestSaveNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ipynb")
	if err := New().Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode & ^os.FileMode(0o644) != 0 || mode&0o600 != 0o600 {
		t.Errorf("unexpected new file mode: %o", mode)
	}
	assertDirectoryEntries(t, dir, "test.ipynb")
}

func TestSaveDoesNotTouchSiblingTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ipynb")
	if err := os.WriteFile(path+".tmp", []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := New().Save(path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path + ".tmp")
	if err != nil || string(data) != "keep" {
		t.Fatalf("sibling = %q, error = %v", data, err)
	}
	assertDirectoryEntries(t, dir, "test.ipynb", "test.ipynb.tmp")
}

func TestSaveFailureCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.ipynb")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := New().Save(path); err == nil {
		t.Fatal("save over directory succeeded")
	}
	assertDirectoryEntries(t, dir, "test.ipynb")
}

func assertDirectoryEntries(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("directory entries = %v, want %v", got, want)
	}
}

func TestRewriteCode(t *testing.T) {
	for _, tt := range []struct {
		name, data, wantSource, wantError string
		unchanged, fail                   bool
	}{
		{name: "string", data: `{"cells":[{"cell_type":"code","source":"old"}]}`, wantSource: `"new\nline\n"`},
		{name: "array", data: `{"cells":[{"cell_type":"code","source":["ol","d"]}]}`, wantSource: `["new\n","line\n"]`},
		{name: "unchanged", data: ` {"cells":[{"cell_type":"code","source":"old"}]} `, unchanged: true},
		{name: "empty cells", data: `{"cells":[]}`, unchanged: true},
		{name: "other cells", data: `{"cells":[{"cell_type":"markdown","source":"old"},{"cell_type":"raw","source":["old"]}]}`, unchanged: true},
		{name: "callback error", data: `{"cells":[{"cell_type":"code","source":"first"},{"cell_type":"code","source":"old"}]}`, fail: true, wantError: "cell 2: boom"},
		{name: "null cells", data: `{"cells":null}`, wantError: "invalid notebook cells"},
		{name: "object cells", data: `{"cells":{}}`, wantError: "invalid notebook cells"},
		{name: "missing cells", data: `{}`, wantError: "invalid notebook cells"},
		{name: "invalid JSON", data: `{`, wantError: "invalid notebook"},
		{name: "null source", data: `{"cells":[{"cell_type":"code","source":null}]}`, wantError: "cell 1: invalid source"},
		{name: "number source", data: `{"cells":[{"cell_type":"code","source":42}]}`, wantError: "cell 1: invalid source"},
		{name: "null array item", data: `{"cells":[{"cell_type":"code","source":["old",null]}]}`, wantError: "cell 1: invalid source"},
		{name: "number array item", data: `{"cells":[{"cell_type":"code","source":[42]}]}`, wantError: "cell 1: invalid source"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			boom := errors.New("boom")
			out, changed, err := RewriteCode([]byte(tt.data), func(src string) (string, error) {
				calls++
				if tt.fail && calls == 2 {
					return "", boom
				}
				if tt.unchanged {
					return src, nil
				}
				return "new\nline\n", nil
			})
			if tt.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantError) || out != nil || changed {
					t.Fatalf("got (%q, %v, %v), want error %q and no result", out, changed, err, tt.wantError)
				}
				if tt.fail && !errors.Is(err, boom) {
					t.Fatal("callback error was not wrapped")
				}
				return
			}
			if err != nil || changed == tt.unchanged {
				t.Fatalf("changed = %v, error = %v", changed, err)
			}
			if tt.unchanged {
				if !bytes.Equal(out, []byte(tt.data)) {
					t.Fatal("unchanged document not returned verbatim")
				}
				if tt.name == "other cells" && calls != 0 {
					t.Fatal("callback called for non-code cells")
				}
				return
			}
			var doc struct {
				Cells []struct{ Source json.RawMessage }
			}
			if err := json.Unmarshal(out, &doc); err != nil {
				t.Fatal(err)
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, doc.Cells[0].Source); err != nil {
				t.Fatal(err)
			}
			if compact.String() != tt.wantSource {
				t.Fatalf("source = %s, want %s", compact.String(), tt.wantSource)
			}
		})
	}
}

func TestRewriteCodePreservesUnknownFields(t *testing.T) {
	data := []byte(`{"extension":{"large":9007199254740993,"text":"\u003ckeep\u003e"},"cells":[{"cell_type":"code","source":"old","custom":[true,null],"outputs":[{"custom":{"n":9007199254740993},"data":{"application/json":{"x":"keep"}}}]},{"cell_type":"markdown","source":["old"],"attachments":{"x":true}},{"cell_type":"raw","source":"old","custom":42}]}`)
	out, changed, err := RewriteCode(data, func(string) (string, error) { return "new", nil })
	if err != nil || !changed {
		t.Fatalf("changed = %v, error = %v", changed, err)
	}
	// Compare decoded raw JSON, compacted only to remove reindentation. This
	// retains exact number and string spellings rather than rounding floats.
	decode := func(data []byte) map[string]json.RawMessage {
		t.Helper()
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			t.Fatal(err)
		}
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(compact.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		return doc
	}
	before, after := decode(data), decode(out)
	var oldCells, newCells []map[string]json.RawMessage
	if err := json.Unmarshal(before["cells"], &oldCells); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after["cells"], &newCells); err != nil {
		t.Fatal(err)
	}
	newCells[0]["source"] = oldCells[0]["source"]
	if !reflect.DeepEqual(oldCells, newCells) {
		t.Fatalf("non-source cell fields changed: %s", out)
	}
	delete(before, "cells")
	delete(after, "cells")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("top-level fields changed: %s", out)
	}
}
