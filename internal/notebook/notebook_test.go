package notebook

import (
	"strings"
	"testing"
)

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
