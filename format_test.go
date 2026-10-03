package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func executeFormat(t *testing.T, args ...string) error {
	t.Helper()
	cmd := rootCmd()
	cmd.SetArgs(append([]string{"format"}, args...))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.Execute()
}

func writeFormatFixture(t *testing.T, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.ipynb")
	if err := os.WriteFile(path, []byte(text), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

func readFormatFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeFormatFixture(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestFormatCommandPreservesNotebook(t *testing.T) {
	original := `{
 "nbformat":4,"nbformat_minor":5,
 "metadata":{"custom":{"large":9007199254740993}},"extension":[true,null,{"x":1}],
 "cells":[
 {"cell_type":"markdown","source":["# unchanged  \n"],"attachments":{"a":{"image/png":"abcd"}}},
 {"cell_type":"code","source":["x:=1\n","fmt.Println( x )\n"],"execution_count":null,"id":"first","metadata":{"collapsed":true},"unknown":{"a":2},"outputs":[{"output_type":"display_data","data":{"application/json":{"a":1},"text/plain":["old"]},"metadata":{"x":1},"custom":true}]},
 {"cell_type":"raw","source":" untouched  ","custom":7},
 {"cell_type":"code","source":"var y=2\n","execution_count":42,"outputs":[]}
 ]
}`
	path := writeFormatFixture(t, original)
	if err := executeFormat(t, path); err != nil {
		t.Fatal(err)
	}
	got := decodeFormatFixture(t, readFormatFixture(t, path))
	want := decodeFormatFixture(t, []byte(original))
	cells := got["cells"].([]any)
	first := cells[1].(map[string]any)
	if !reflect.DeepEqual(first["source"], []any{"x := 1\n", "fmt.Println(x)\n"}) {
		t.Fatalf("formatted array source = %#v", first["source"])
	}
	last := cells[3].(map[string]any)
	if last["source"] != "var y = 2\n" {
		t.Fatalf("formatted string source = %#v", last["source"])
	}
	wantCells := want["cells"].([]any)
	first["source"] = wantCells[1].(map[string]any)["source"]
	last["source"] = wantCells[3].(map[string]any)["source"]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("non-source notebook data changed:\ngot %#v\nwant %#v", got, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	before := readFormatFixture(t, path)
	if err := executeFormat(t, path); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readFormatFixture(t, path)) {
		t.Fatal("second format changed the file")
	}
}

func TestFormatCommandFailureLeavesOriginal(t *testing.T) {
	for _, source := range []string{`"func broken("`, `null`, `["x",null]`, `42`} {
		t.Run(source, func(t *testing.T) {
			original := `{"cells":[{"cell_type":"code","source":"var x=1"},{"cell_type":"code","source":` + source + `}]}`
			path := writeFormatFixture(t, original)
			err := executeFormat(t, path)
			if err == nil || !strings.Contains(err.Error(), "cell 2") {
				t.Fatalf("error = %v, want cell 2 failure", err)
			}
			if string(readFormatFixture(t, path)) != original {
				t.Fatal("failed format modified the notebook")
			}
		})
	}
}

func TestFormatCommandDoesNotExecute(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "executed")
	source := "!touch " + marker + "\n%%not-a-command\n"
	// A first-line cell magic is opaque, even if its body is not Go.
	magic := "%%bash\ntouch " + marker + "\n"
	data, err := json.Marshal(map[string]any{"cells": []any{
		map[string]any{"cell_type": "code", "source": source},
		map[string]any{"cell_type": "code", "source": magic},
	}})
	if err != nil {
		t.Fatal(err)
	}
	path := writeFormatFixture(t, string(data))
	if err := executeFormat(t, "--workdir", filepath.Join(t.TempDir(), "unused"), path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("execution marker: %v", err)
	}
	if !bytes.Equal(data, readFormatFixture(t, path)) {
		t.Fatal("opaque command cells changed")
	}
}

func TestFormatCommandInvalidInput(t *testing.T) {
	for _, original := range []string{`{`, `null`, `{}`, `{"cells":null}`, `{"cells":{}}`, `{"cells":[null]}`} {
		t.Run(original, func(t *testing.T) {
			path := writeFormatFixture(t, original)
			if err := executeFormat(t, path); err == nil {
				t.Fatal("expected an error")
			}
			if string(readFormatFixture(t, path)) != original {
				t.Fatal("invalid notebook modified")
			}
		})
	}
	if err := executeFormat(t); err == nil {
		t.Fatal("missing argument accepted")
	}
	if err := executeFormat(t, "one", "two"); err == nil {
		t.Fatal("extra argument accepted")
	}
	if err := executeFormat(t, filepath.Join(t.TempDir(), "missing.ipynb")); err == nil {
		t.Fatal("missing notebook accepted")
	}
}
