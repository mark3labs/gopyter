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
	// maxSteps bounds one fix request: each submission that doesn't
	// compile costs a step, and a fix that still fails after that many
	// tries isn't worth the wait or the tokens.
	maxSteps = 6
	// maxContext bounds the source of the earlier cells sent with a
	// request; the nearest cells are kept.
	maxContext = 24 << 10
)

const fixSystemPrompt = `You fix Go code in cells of gopyter, a Jupyter-style notebook for Go.

How cells work:
- Top-level declarations (func, type, var, const, import) stay available to later cells. Statements run inside main().
- A top-level "x := ..." also stays available to later cells.
- A trailing bare expression is displayed as the cell's result.
- Standard library imports are added automatically when a package is used, so an import line is optional.
- Display(v) and DisplayMarkdown(s) show rich output.
- Lines starting with ! are shell commands and lines starting with % are magics. Keep them as they are.
- Error positions look like In[3]:2:5 (cell, line, column).

Fix the failing cell with the smallest change that keeps what the user meant. Don't rewrite code that works, and don't add "package" or "func main" unless the cell already has them. You can only change this cell, not the earlier ones.

Identifiers from earlier cells only exist once those cells have run in this session; the prompt lists the ones that currently do, and marks cells that haven't run. If the error is only there because an earlier cell hasn't run, don't call the tool and don't work around it (for example by copying a value): say which cell to run first.

Submit the complete corrected cell with the submit_fix tool. It compiles the cell together with the rest of the notebook, without running it, and returns any remaining errors: keep fixing and submitting until it compiles. If the problem can't be fixed inside this cell, don't call the tool; explain why in one or two sentences.`

type submitInput struct {
	Source      string `json:"source" description:"The complete corrected cell source."`
	Explanation string `json:"explanation" description:"One short sentence saying what was wrong."`
}

// Fix asks the model for a corrected version of req's cell. Every
// proposal is compiled with check before it is accepted, so a returned Fix
// compiles (up to Fix.Missing). progress receives short status updates,
// from other goroutines. Cancel ctx to abort.
func (a *Assistant) Fix(ctx context.Context, req FixRequest, check Checker, progress func(string)) (*Fix, error) {
	if progress == nil {
		progress = func(string) {}
	}
	// Tool calls of one turn run one at a time, but on kit's goroutines.
	var mu sync.Mutex
	var lastErrors string
	submit := kit.NewTool("submit_fix",
		"Submit the complete corrected cell. It is compiled with the notebook's other cells, without running it. Returns the compile errors if it doesn't compile yet.",
		func(ctx context.Context, in submitInput) (kit.ToolOutput, error) {
			progress("compiling the proposed fix")
			r, err := check.Check(ctx, req.CellID, req.Name, in.Source)
			if err != nil {
				return kit.ToolOutput{}, err
			}
			if r.Errors != "" {
				mu.Lock()
				lastErrors = r.Errors
				mu.Unlock()
				progress("the proposal didn't compile, retrying")
				return kit.ErrorResult("The cell still doesn't compile:\n" + r.Errors + "\nFix these errors and call submit_fix again."), nil
			}
			fix := &Fix{Source: in.Source, Explanation: strings.TrimSpace(in.Explanation), Missing: r.Missing}
			return kit.ToolOutput{Content: "Accepted.", Halt: true, FinalValue: fix}, nil
		})

	opts := append([]kit.Option{
		kit.WithModel(a.model),
		kit.WithSystemPrompt(fixSystemPrompt),
		kit.WithExtraTools(submit),
		func(o *kit.Options) { o.MaxSteps = maxSteps },
	}, a.opts...)
	progress("asking " + a.model)
	k, err := kit.NewIsolatedAgent(ctx, opts...)
	if err != nil {
		return nil, a.explain(err)
	}
	defer func() { _ = k.Close() }() // nothing is persisted; a close error changes nothing for the user

	res, err := k.PromptResult(ctx, fixPrompt(req))
	if err != nil {
		return nil, a.explain(err)
	}
	if fix, ok := res.FinalValue.(*Fix); ok {
		return fix, nil
	}
	mu.Lock()
	defer mu.Unlock()
	if lastErrors != "" {
		return nil, fmt.Errorf("no fix that compiles after %d tries; last errors:\n%s", maxSteps, lastErrors)
	}
	if reply := strings.TrimSpace(res.Response); reply != "" {
		return nil, fmt.Errorf("no fix proposed: %s", reply)
	}
	return nil, errors.New("no fix proposed")
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

// fixPrompt renders the request for the model.
func fixPrompt(req FixRequest) string {
	var b strings.Builder
	if req.GoVersion != "" {
		b.WriteString("Go version: ")
		b.WriteString(req.GoVersion)
		b.WriteString("\n\n")
	}
	if before := trimContext(req.Before); len(before) > 0 {
		b.WriteString("Earlier code cells, oldest first:\n\n")
		for _, c := range before {
			writeBlock(&b, c.Name+":", "go", c.Source)
		}
	}
	if len(req.Declarations) > 0 {
		b.WriteString("Identifiers the notebook currently declares: ")
		b.WriteString(strings.Join(req.Declarations, ", "))
		b.WriteString("\n\n")
	} else {
		b.WriteString("The notebook declares nothing yet: no earlier cell has run in this session.\n\n")
	}
	writeBlock(&b, "The failing cell, "+req.Name+":", "go", req.Source)
	writeBlock(&b, "Its error:", "", req.Error)
	return b.String()
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
