package notebook

import (
	"reflect"
	"strings"
	"testing"
)

func TestAddOutput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outs     []Output
		kind     OutputKind
		text, id string
		want     []Output
		index    int
	}{
		{"stdout merges", []Output{{Kind: Stdout, Text: "a"}}, Stdout, "b", "", []Output{{Kind: Stdout, Text: "ab"}}, 0},
		{"stderr merges", []Output{{Kind: Stderr, Text: "a"}}, Stderr, "b", "", []Output{{Kind: Stderr, Text: "ab"}}, 0},
		{"streams differ", []Output{{Kind: Stdout, Text: "a"}}, Stderr, "b", "", []Output{{Kind: Stdout, Text: "a"}, {Kind: Stderr, Text: "b"}}, 1},
		{"info stays separate", []Output{{Kind: Info, Text: "a"}}, Info, "b", "", []Output{{Kind: Info, Text: "a"}, {Kind: Info, Text: "b"}}, 1},
		{"id replaces in place", []Output{{Kind: Result, Text: "1", ID: "x"}, {Kind: Info, Text: "i"}}, MarkdownOut, "2", "x", []Output{{Kind: MarkdownOut, Text: "2", ID: "x"}, {Kind: Info, Text: "i"}}, 0},
		{"new id appends", []Output{{Kind: Result, Text: "1", ID: "x"}}, Result, "2", "y", []Output{{Kind: Result, Text: "1", ID: "x"}, {Kind: Result, Text: "2", ID: "y"}}, 1},
		{"new stream id appends", []Output{{Kind: Stdout, Text: "a"}}, Stdout, "b", "x", []Output{{Kind: Stdout, Text: "a"}, {Kind: Stdout, Text: "b", ID: "x"}}, 1},
		{"stream preserves earlier id", []Output{{Kind: Stdout, Text: "a", ID: "x"}}, Stdout, "b", "", []Output{{Kind: Stdout, Text: "a", ID: "x"}, {Kind: Stdout, Text: "b"}}, 1},
		{"cap keeps newest", []Output{{Kind: Stdout, Text: strings.Repeat("a", 4<<20)}}, Stdout, "newest", "", []Output{{Kind: Stdout, Text: strings.Repeat("a", (2<<20)-6) + "newest"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, index := AddOutput(tc.outs, tc.kind, tc.text, tc.id)
			if index != tc.index || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("index = %d, want %d; outputs match = %v", index, tc.index, reflect.DeepEqual(got, tc.want))
			}
		})
	}
}
