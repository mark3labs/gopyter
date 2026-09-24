package notebook

import (
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
