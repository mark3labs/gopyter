package markdown

import (
	"reflect"
	"testing"
)

func TestHeadings(t *testing.T) {
	got := Headings("# A\n## B *em* `code`\n```go\n# not\n```\n#### D\nE\n===\n")
	want := []Heading{{1, "A"}, {2, "B em code"}, {4, "D"}, {1, "E"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestHeadingsAsRendered is a regression test: soft breaks, autolinks,
// entities and escapes were dropped or kept raw.
func TestHeadingsAsRendered(t *testing.T) {
	got := Headings("Heading\nline two\n===\n# See <https://x.y> now\n# A &amp; B &#35;1\n# Tom\\'s `a\\'b`\n")
	want := []Heading{{1, "Heading line two"}, {1, "See https://x.y now"}, {1, "A & B #1"}, {1, "Tom's a\\'b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
