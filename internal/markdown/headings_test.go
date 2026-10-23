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
