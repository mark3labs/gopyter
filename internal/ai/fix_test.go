//go:build !noai

package ai

import (
	"context"
	"encoding/json"
	"errors"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"charm.land/fantasy"
	"github.com/mark3labs/gopyter/internal/kernel"
	kit "github.com/mark3labs/kit/pkg/kit"
)

// scriptedModel is a language model that replays canned replies, one per
// call, and records the prompts it was sent. It lets the tests drive a
// whole agent turn without a network or an API key.
type scriptedModel struct {
	mu      sync.Mutex
	replies [][]fantasy.StreamPart
	prompts []fantasy.Prompt
}

func (m *scriptedModel) Stream(_ context.Context, call fantasy.Call) (fantasy.StreamResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prompts = append(m.prompts, call.Prompt)
	// Out of script: a plain reply. kit v0.113.0 asks the model once more
	// after a tool halts the turn (ToolOutput.Halt doesn't set fantasy's
	// StopTurn), so tests must tolerate that extra call.
	parts := textReply("Done.")
	if len(m.replies) > 0 {
		parts = m.replies[0]
		m.replies = m.replies[1:]
	}
	return iter.Seq[fantasy.StreamPart](func(yield func(fantasy.StreamPart) bool) {
		for _, p := range parts {
			if !yield(p) {
				return
			}
		}
	}), nil
}

func (m *scriptedModel) Generate(context.Context, fantasy.Call) (*fantasy.Response, error) {
	return nil, errors.New("scriptedModel: only streaming is scripted")
}

func (m *scriptedModel) GenerateObject(context.Context, fantasy.ObjectCall) (*fantasy.ObjectResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *scriptedModel) StreamObject(context.Context, fantasy.ObjectCall) (fantasy.ObjectStreamResponse, error) {
	return nil, errors.New("not implemented")
}

func (m *scriptedModel) Provider() string { return "scripted" }
func (m *scriptedModel) Model() string    { return "test" }

// firstPrompt returns the text of the user message of the first call.
func (m *scriptedModel) firstPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.prompts) == 0 {
		return ""
	}
	var b strings.Builder
	for _, msg := range m.prompts[0] {
		if msg.Role != fantasy.MessageRoleUser {
			continue
		}
		for _, p := range msg.Content {
			if t, ok := p.(fantasy.TextPart); ok {
				b.WriteString(t.Text)
			}
		}
	}
	return b.String()
}

// submitReply is a model turn that calls submit_fix.
func submitReply(id, source, explanation string) []fantasy.StreamPart {
	in, _ := json.Marshal(submitInput{Source: source, Explanation: explanation})
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeToolInputStart, ID: id, ToolCallName: "submit_fix"},
		{Type: fantasy.StreamPartTypeToolInputDelta, ID: id, Delta: string(in)},
		{Type: fantasy.StreamPartTypeToolInputEnd, ID: id},
		{Type: fantasy.StreamPartTypeToolCall, ID: id, ToolCallName: "submit_fix", ToolCallInput: string(in)},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonToolCalls},
	}
}

// textReply is a model turn that answers without calling a tool.
func textReply(text string) []fantasy.StreamPart {
	return []fantasy.StreamPart{
		{Type: fantasy.StreamPartTypeTextStart, ID: "t"},
		{Type: fantasy.StreamPartTypeTextDelta, ID: "t", Delta: text},
		{Type: fantasy.StreamPartTypeTextEnd, ID: "t"},
		{Type: fantasy.StreamPartTypeFinish, FinishReason: fantasy.FinishReasonStop},
	}
}

// fakeChecker compiles nothing: a source containing "BAD" fails.
type fakeChecker struct {
	mu      sync.Mutex
	checked []string
	missing []string
}

func (c *fakeChecker) Check(_ context.Context, _, name, src string) (kernel.CheckResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checked = append(c.checked, src)
	if strings.Contains(src, "BAD") {
		return kernel.CheckResult{Errors: name + ":1:1: undefined: BAD"}, nil
	}
	return kernel.CheckResult{Missing: c.missing}, nil
}

func newScripted(t *testing.T, replies ...[]fantasy.StreamPart) (*Assistant, *scriptedModel) {
	t.Helper()
	m := &scriptedModel{replies: replies}
	a := New("scripted/test", kit.WithProvider("scripted",
		func(context.Context, *kit.ProviderConfig, string) (*kit.ProviderResult, error) {
			return &kit.ProviderResult{Model: m}, nil
		}))
	return a, m
}

var failing = FixRequest{
	CellID: "c3", Name: "In[3]",
	Source:       "fmt.Println(nme)",
	Error:        "In[3]:1:13: undefined: nme",
	Before:       []Cell{{Name: "In[1]", Source: "name := \"gopher\""}},
	Declarations: []string{"name"},
	GoVersion:    "go1.27.1",
}

func TestFixRetriesUntilItCompiles(t *testing.T) {
	a, m := newScripted(t,
		submitReply("1", "fmt.Println(BAD)", "first try"),
		submitReply("2", "fmt.Println(name)", "Typo: nme should be name."),
	)
	check := &fakeChecker{}
	var progress []string
	var pmu sync.Mutex
	fix, err := a.Fix(context.Background(), failing, check, func(s string) {
		pmu.Lock()
		progress = append(progress, s)
		pmu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if fix.Source != "fmt.Println(name)" || fix.Explanation != "Typo: nme should be name." {
		t.Errorf("fix = %+v", fix)
	}
	if len(check.checked) != 2 {
		t.Errorf("checked %d proposals, want 2", len(check.checked))
	}
	if len(progress) == 0 {
		t.Error("no progress reported")
	}

	// The model saw the failing cell, its error and the earlier cells.
	p := m.firstPrompt()
	for _, want := range []string{"In[3]", "fmt.Println(nme)", "undefined: nme", "In[1]", `name := "gopher"`, "go1.27.1"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
}

func TestFixReportsMissingModules(t *testing.T) {
	a, _ := newScripted(t, submitReply("1", "import \"example.com/x\"\nx.Y()", "use x"))
	fix, err := a.Fix(context.Background(), failing, &fakeChecker{missing: []string{"example.com/x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(fix.Missing) != 1 || fix.Missing[0] != "example.com/x" {
		t.Errorf("Missing = %v", fix.Missing)
	}
}

func TestFixWithoutProposal(t *testing.T) {
	a, _ := newScripted(t, textReply("The error is in In[1], which this cell can't change."))
	_, err := a.Fix(context.Background(), failing, &fakeChecker{}, nil)
	if err == nil || !strings.Contains(err.Error(), "can't change") {
		t.Fatalf("err = %v", err)
	}
}

func TestFixGivesUp(t *testing.T) {
	var replies [][]fantasy.StreamPart
	for i := range maxSteps + 2 {
		replies = append(replies, submitReply(string(rune('a'+i)), "BAD", "still wrong"))
	}
	a, _ := newScripted(t, replies...)
	check := &fakeChecker{}
	_, err := a.Fix(context.Background(), failing, check, nil)
	if err == nil || !strings.Contains(err.Error(), "undefined: BAD") {
		t.Fatalf("err = %v", err)
	}
	if len(check.checked) > maxSteps {
		t.Errorf("%d proposals checked, want at most %d", len(check.checked), maxSteps)
	}
}

// allText returns every text part of the first call, system prompt
// included.
func (m *scriptedModel) allText() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	if len(m.prompts) > 0 {
		for _, msg := range m.prompts[0] {
			for _, p := range msg.Content {
				if t, ok := p.(fantasy.TextPart); ok {
					b.WriteString(t.Text)
				}
			}
		}
	}
	return b.String()
}

// TestFixIgnoresProjectKitSetup guards the isolation promise: opening a
// notebook in a directory whose .kit.yml starts MCP servers, or whose
// AGENTS.md has instructions, must not start them or reach the model.
func TestFixIgnoresProjectKitSetup(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	marker := filepath.Join(project, "mcp-started")
	kitYML := "mcpServers:\n  evil:\n    type: local\n    command: [\"sh\", \"-c\", \"touch " + marker + "; sleep 2\"]\n"
	if err := os.WriteFile(filepath.Join(project, ".kit.yml"), []byte(kitYML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "AGENTS.md"), []byte("SECRET-PROJECT-INSTRUCTIONS"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(project)

	a, m := newScripted(t, submitReply("1", "fmt.Println(name)", "typo"))
	if _, err := a.Fix(context.Background(), failing, &fakeChecker{}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("the project's MCP server was started")
	}
	if strings.Contains(m.allText(), "SECRET-PROJECT-INSTRUCTIONS") {
		t.Error("the project's AGENTS.md reached the model")
	}
	if !strings.Contains(m.allText(), "gopyter") {
		t.Error("gopyter's system prompt is missing")
	}
	for _, name := range []string{".kit.yml", ".kit"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Errorf("kit wrote ~/%s", name)
		}
	}
}

func TestTrimContextKeepsNearestCells(t *testing.T) {
	big := strings.Repeat("x", maxContext/2+1)
	cells := []Cell{{"In[1]", big}, {"In[2]", big}, {"In[3]", "y := 1"}}
	got := trimContext(cells)
	if len(got) != 2 || got[0].Name != "In[2]" {
		t.Errorf("kept %d cells starting at %s", len(got), got[0].Name)
	}
}
