package markdown

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

func testRenderer() *Renderer {
	c := lipgloss.Color
	return New(Colors{
		Heading: c("#00ADD8"), Info: c("#5DC9E2"), Accent: c("#8B5CF6"),
		Warning: c("#FDDD00"), Success: c("#4ADE80"), Error: c("#F87171"),
		Code: c("#FB923C"), Text: c("#E4E4E7"), Muted: c("#71717A"),
		Subtle: c("#3F3F46"), CodeBg: c("#101014"),
	}, styles.Get("catppuccin-mocha"))
}

func plain(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func TestRenderWrapsToWidth(t *testing.T) {
	src := "# Title\n\nSome words `code span` and more words that go on well past the width of the line.\n\n" +
		"- a list item that is long enough to wrap onto a second line\n\n" +
		"> a quote that is long enough to wrap onto a second line too\n"
	lines := testRenderer().Reflowing().Render(src, 30, nil)
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 30 {
			t.Errorf("line %q is %d wide", ansi.Strip(l), w)
		}
	}
	got := strings.Join(plain(lines), "\n")
	for _, want := range []string{
		"• a list item that is long\n  enough to wrap onto a second\n  line",
		"┃ a quote that is long enough\n┃ to wrap onto a second line",
		" code span ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.HasPrefix(plain(lines)[0], " ") || lines[len(lines)-1] == "" {
		t.Errorf("blank edges should be trimmed:\n%s", got)
	}
}

func TestRenderKeepsLineBreaks(t *testing.T) {
	r := testRenderer()
	got := plain(r.Render("one\ntwo\nthree", 40, nil))
	if strings.Join(got, "|") != "one|two|three" {
		t.Fatalf("line breaks should be kept, got %q", got)
	}
	got = plain(r.Render("- a\n  more a\n  - b\n    more b\n- c", 40, nil))
	want := []string{"• a", "  more a", "  ◦ b", "    more b", "• c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("list lines should stay under their item's text, got %q", got)
	}
	got = plain(r.Render("> one\n> two", 40, nil))
	if strings.Join(got, "|") != "┃ one|┃ two" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderReflowing(t *testing.T) {
	r := testRenderer().Reflowing()
	got := plain(r.Render("one\ntwo\nthree", 40, nil))
	if len(got) != 1 || got[0] != "one two three" {
		t.Fatalf("soft breaks should reflow, got %q", got)
	}
	got = plain(r.Render("one  \ntwo", 40, nil))
	if len(got) != 2 {
		t.Fatalf("hard breaks should stay, got %q", got)
	}
}

func TestRenderKeepsCodeSpansWhole(t *testing.T) {
	// The span's padding has a background: it must not be a break point.
	got := plain(testRenderer().Reflowing().Render("aaaa `b c` d", 9, nil))
	if strings.Join(got, "|") != "aaaa| b c  d" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderCutsTablesAndRules(t *testing.T) {
	src := "| a | b |\n|---|---|\n| a long cell value | another long cell value |\n\n---\n"
	for _, l := range testRenderer().Render(src, 20, nil) {
		if w := ansi.StringWidth(l); w > 20 {
			t.Errorf("line %q is %d wide", ansi.Strip(l), w)
		}
	}
}

func TestRenderFillsBackground(t *testing.T) {
	bg := lipgloss.Color("#27272A")
	lines := testRenderer().Render("# Hi\n\ntext `code`\n\n```go\nx := 1\n```", 24, bg)
	for _, l := range lines {
		if w := ansi.StringWidth(l); w != 24 {
			t.Errorf("line %q is %d wide, want 24", ansi.Strip(l), w)
		}
		// Every run of cells carries a background: the panel's or the code's.
		if !strings.Contains(l, "48;2;39;39;42") && !strings.Contains(l, "48;2;16;16;20") {
			t.Errorf("line %q has no background", l)
		}
	}
}

func TestRenderHighlightsCode(t *testing.T) {
	lines := testRenderer().Render("```go\nfunc main() {}\n```", 40, nil)
	if len(lines) != 1 || !strings.Contains(ansi.Strip(lines[0]), "func main() {}") {
		t.Fatalf("got %q", plain(lines))
	}
	// catppuccin-mocha colors keywords red.
	if !strings.Contains(lines[0], "38;2;243;139;168") {
		t.Errorf("keyword not highlighted: %q", lines[0])
	}
}

func TestRenderExpandsTabs(t *testing.T) {
	got := plain(testRenderer().Render("```\ntype T struct {\n\tX int\n}\n```", 40, nil))
	if len(got) != 3 || !strings.Contains(got[1], "    X int") {
		t.Fatalf("got %q", got)
	}
}

func TestRenderBreaksLongWords(t *testing.T) {
	got := plain(testRenderer().Render("see "+strings.Repeat("x", 25)+" and 日本語日本語日本語", 10, nil))
	for _, l := range got {
		if w := ansi.StringWidth(l); w > 10 {
			t.Errorf("line %q is %d wide", l, w)
		}
	}
	want := []string{"see", "xxxxxxxxxx", "xxxxxxxxxx", "xxxxx and", "日本語日本", "語日本語"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %q, want %q", got, want)
	}
}
