//go:build !noai

package ai

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	kit "github.com/mark3labs/kit/pkg/kit"
)

// Assistant runs AI requests with one model. Creating it is free: kit is
// only used when a request is made.
type Assistant struct {
	model string
	opts  []kit.Option
}

// New returns an assistant for model ("provider/model", see Resolve).
// opts are added to every agent it creates; tests use them to plug in a
// scripted model with kit.WithProvider.
func New(model string, opts ...kit.Option) *Assistant {
	return &Assistant{model: model, opts: opts}
}

// Model returns the "provider/model" the assistant uses.
func (a *Assistant) Model() string { return a.model }

// Ready reports whether the model's credentials are available (see Ready).
func (a *Assistant) Ready() error { return Ready(a.model) }

const (
	// maxSteps bounds one request: each submission that doesn't compile
	// costs a step, and a proposal that still fails after that many tries
	// isn't worth the wait or the tokens.
	maxSteps = 6
	// maxContext bounds the source of the earlier cells sent with a
	// request; the nearest cells are kept.
	maxContext = 24 << 10
	// submitTool is the name of the tool the model submits cells with.
	submitTool = "submit_cell"
)

// cellRules explains gopyter's cells; both system prompts start with it.
const cellRules = `How cells work:
- Top-level declarations (func, type, var, const, import) stay available to later cells. Statements run inside main().
- A top-level "x := ..." also stays available to later cells.
- A trailing bare expression is displayed as the cell's result.
- Standard library imports are added automatically when a package is used, so an import line is optional.
- Rich output comes from package nb, used without an import: nb.Display(v) (draws an image.Image as a picture), nb.DisplayMarkdown(s), nb.DisplayPNG(data), and nb.DisplayID(id, v) / nb.DisplayMarkdownID(id, s), which replace their earlier output with the same id (animations, progress). nb.Cache/nb.CacheErr compute a value once per kernel.
- For interaction, GoNB's widgets work: import "github.com/janpfeifer/gonb/gonbui/widgets", e.g. s := widgets.Slider(0, 100, 50).Done(); for v := range s.Listen().C { ... }. The loop ends when the user presses Done.
- Lines starting with ! are shell commands and lines starting with % are magics (e.g. %test runs the cell's Test functions with go test). A first line starting with %% (%%writefile, %%bash) makes the whole cell a file or script, not Go. Keep them as they are.
- Error positions look like In[3]:2:5 (cell, line, column).

Identifiers from earlier cells only exist once those cells have run in this session; the prompt lists the ones that currently do, and marks cells that haven't run.`

const fixSystemPrompt = `You fix Go code in cells of gopyter, a Jupyter-style notebook for Go.

` + cellRules + `

Fix the failing cell with the smallest change that keeps what the user meant. Don't rewrite code that works, and don't add "package" or "func main" unless the cell already has them. You can only change this cell, not the earlier ones.

If the error is only there because an earlier cell hasn't run, don't call the tool and don't work around it (for example by copying a value): say which cell to run first.

Submit the complete corrected cell with the ` + submitTool + ` tool. It compiles the cell together with the rest of the notebook, without running it, and returns any remaining errors: keep fixing and submitting until it compiles. If the problem can't be fixed inside this cell, don't call the tool; explain why in one or two sentences.`

const editSystemPrompt = `You write and change Go code in cells of gopyter, a Jupyter-style notebook for Go, as the user asks.

` + cellRules + `

Do what the user asks, in this cell only: you can't change the earlier cells. If the cell is empty, write it from scratch. Leave the parts of the cell the request isn't about as they are. Build on what the earlier cells declare instead of redefining it. Prefer the standard library; use another module only when the user asks for one or the standard library has nothing suitable. Show results the way notebook cells do: print them, use Display or DisplayMarkdown, or end with the expression. Don't add "package" or "func main" unless the cell already has them. Keep the code idiomatic and short, with comments only where they help.

If the request needs identifiers from an earlier cell that hasn't run, don't call the tool and don't work around it: say which cell to run first.

Submit the complete new cell with the ` + submitTool + ` tool. It compiles the cell together with the rest of the notebook, without running it, and returns any errors: keep fixing and submitting until it compiles. If the request is unclear, or can't be done inside this cell, don't call the tool; explain why in one or two sentences.`

type submitInput struct {
	Source      string `json:"source" description:"The complete new cell source."`
	Explanation string `json:"explanation" description:"One short sentence saying what was wrong or what changed."`
}

// Fix asks the model for a corrected version of req's cell, whose last run
// failed with req.Error. Every proposal is compiled with check before it is
// accepted, so a returned Proposal compiles (up to Proposal.Missing).
// progress receives short status updates, from other goroutines. Cancel
// ctx to abort.
func (a *Assistant) Fix(ctx context.Context, req Request, check Checker, progress func(string)) (*Proposal, error) {
	return a.propose(ctx, fixSystemPrompt, fixPrompt(req), "fix", req, check, progress)
}

// Edit asks the model to change req's cell, or write it when it's empty,
// as req.Instruction says. Proposals are checked like Fix's.
func (a *Assistant) Edit(ctx context.Context, req Request, check Checker, progress func(string)) (*Proposal, error) {
	if strings.TrimSpace(req.Instruction) == "" {
		return nil, errors.New("say what the cell should do")
	}
	return a.propose(ctx, editSystemPrompt, editPrompt(req), "change", req, check, progress)
}

// propose runs one agent turn that ends when the model submits a cell that
// compiles, gives up, or runs out of steps. noun names the outcome in
// errors ("fix", "change").
func (a *Assistant) propose(ctx context.Context, system, prompt, noun string, req Request, check Checker, progress func(string)) (*Proposal, error) {
	if progress == nil {
		progress = func(string) {}
	}
	// Tool calls of one turn run one at a time, but on kit's goroutines.
	var mu sync.Mutex
	var lastErrors string
	submit := kit.NewTool(submitTool,
		"Submit the complete new cell. It is compiled with the notebook's other cells, without running it. Returns the compile errors if it doesn't compile yet.",
		func(ctx context.Context, in submitInput) (kit.ToolOutput, error) {
			progress("compiling the proposed " + noun)
			r, err := check.Check(ctx, req.CellID, req.Name, in.Source)
			if err != nil {
				return kit.ToolOutput{}, err
			}
			if r.Errors != "" {
				mu.Lock()
				lastErrors = r.Errors
				mu.Unlock()
				progress("the proposal didn't compile, retrying")
				return kit.ErrorResult("The cell doesn't compile:\n" + r.Errors + "\nFix these errors and call " + submitTool + " again."), nil
			}
			p := &Proposal{Source: in.Source, Explanation: strings.TrimSpace(in.Explanation), Missing: r.Missing}
			return kit.ToolOutput{Content: "Accepted.", Halt: true, FinalValue: p}, nil
		})

	opts := append([]kit.Option{
		kit.WithModel(a.model),
		kit.WithSystemPrompt(system),
		kit.WithExtraTools(submit),
		func(o *kit.Options) { o.MaxSteps = maxSteps },
	}, a.opts...)
	progress("asking " + a.model)
	k, err := kit.NewIsolatedAgent(ctx, opts...)
	if err != nil {
		return nil, a.explain(err)
	}
	defer func() { _ = k.Close() }() // nothing is persisted; a close error changes nothing for the user

	res, err := k.PromptResult(ctx, prompt)
	if err != nil {
		return nil, a.explain(err)
	}
	if p, ok := res.FinalValue.(*Proposal); ok {
		return p, nil
	}
	mu.Lock()
	defer mu.Unlock()
	if lastErrors != "" {
		return nil, fmt.Errorf("no %s that compiles after %d tries; last errors:\n%s", noun, maxSteps, lastErrors)
	}
	if reply := strings.TrimSpace(res.Response); reply != "" {
		return nil, fmt.Errorf("no %s proposed: %s", noun, reply)
	}
	return nil, fmt.Errorf("no %s proposed", noun)
}

// explain turns kit errors users can act on into short messages.
func (a *Assistant) explain(err error) error {
	provider, _, _ := strings.Cut(a.model, "/")
	switch {
	case errors.Is(err, context.Canceled):
		return err
	case kit.IsMissingCredentialsError(err):
		return fmt.Errorf("no API key for %s: set %s", provider, EnvHint(provider))
	}
	switch c := kit.ClassifyProviderError(err); {
	case errors.Is(c, kit.ErrAuth):
		return fmt.Errorf("%s rejected the API key (%s)", provider, EnvHint(provider))
	case errors.Is(c, kit.ErrRateLimit):
		return fmt.Errorf("%s is rate limiting requests; try again shortly", provider)
	}
	return err
}

// fixPrompt renders a fix request for the model.
func fixPrompt(req Request) string {
	var b strings.Builder
	writeContext(&b, req)
	writeBlock(&b, "The failing cell, "+req.Name+":", "go", req.Source)
	writeBlock(&b, "Its error:", "", req.Error)
	return b.String()
}

// editPrompt renders an edit request for the model.
func editPrompt(req Request) string {
	var b strings.Builder
	writeContext(&b, req)
	if strings.TrimSpace(req.Source) == "" {
		b.WriteString("The cell to write, ")
		b.WriteString(req.Name)
		b.WriteString(", is empty.\n\n")
	} else {
		writeBlock(&b, "The cell to change, "+req.Name+":", "go", req.Source)
	}
	if req.Error != "" {
		writeBlock(&b, "Its last run failed with:", "", req.Error)
	}
	writeBlock(&b, "The user's request:", "", req.Instruction)
	return b.String()
}

// writeContext renders what both prompts share: the Go version, the
// earlier cells and the declared identifiers.
func writeContext(b *strings.Builder, req Request) {
	if req.GoVersion != "" {
		b.WriteString("Go version: ")
		b.WriteString(req.GoVersion)
		b.WriteString("\n\n")
	}
	if before := trimContext(req.Before); len(before) > 0 {
		b.WriteString("Earlier code cells, oldest first:\n\n")
		for _, c := range before {
			writeBlock(b, c.Name+":", "go", c.Source)
		}
	}
	if len(req.Declarations) > 0 {
		b.WriteString("Identifiers the notebook currently declares: ")
		b.WriteString(strings.Join(req.Declarations, ", "))
		b.WriteString("\n\n")
	} else {
		b.WriteString("The notebook declares nothing yet: no earlier cell has run in this session.\n\n")
	}
}

// trimContext keeps the nearest cells whose sources fit in maxContext.
func trimContext(cells []Cell) []Cell {
	size := 0
	for i, cell := range slices.Backward(cells) {
		size += len(cell.Source)
		if size > maxContext {
			return cells[i+1:]
		}
	}
	return cells
}

func writeBlock(b *strings.Builder, title, lang, body string) {
	b.WriteString(title)
	b.WriteString("\n```")
	b.WriteString(lang)
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(body, "\n"))
	b.WriteString("\n```\n\n")
}
