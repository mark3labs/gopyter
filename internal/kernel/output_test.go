package kernel

import (
	"testing"

	"github.com/mark3labs/gopyter/internal/notebook"
)

func TestEventOutputKind(t *testing.T) {
	for _, tc := range []struct {
		kind EventKind
		want notebook.OutputKind
		ok   bool
	}{
		{Stdout, notebook.Stdout, true},
		{Stderr, notebook.Stderr, true},
		{Result, notebook.Result, true},
		{Markdown, notebook.MarkdownOut, true},
		{Error, notebook.Error, true},
		{Info, notebook.Info, true},
		{Image, notebook.ImageOut, true},
		{HTML, notebook.HTMLOut, true},
		{Op, "", false},
		{EventKind(-1), "", false},
	} {
		if got, ok := tc.kind.OutputKind(); got != tc.want || ok != tc.ok {
			t.Errorf("kind %d: (%q, %v), want (%q, %v)", tc.kind, got, ok, tc.want, tc.ok)
		}
	}
}
