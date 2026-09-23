---
description: Update README, AGENTS.md, in-app help and godoc for a recent change
---

Review recent code changes, identify every documentation surface that should mention them, and update each one. Base the updates on the actual diff, not on guesses.

## Steps

1. **Identify the change**:
   - If the user input ($@) names a commit, PR, branch or topic, focus on that
   - Otherwise, on a feature branch, inspect `git log origin/master..HEAD --oneline` and `git diff origin/master...HEAD --stat`. On master, use the most recent commits (`git log -10 --oneline`)
   - Read the actual diff. Never document features that aren't in the code

2. **Inventory the doc surfaces** for this repo:
   - `README.md`: the user-facing surface, with sections for usage, how cells run, cell commands (`%magics`, `!shell`), code completion, keys, mouse, and layout
   - `AGENTS.md`: commands, conventions, architecture notes and testing instructions for agents. Update it when invariants, package responsibilities or workflows change
   - **In-app help**: `fullHelp()` and the short help bindings in `internal/ui/keys.go`, the tips text in the help overlay (`renderOverlay` in `internal/ui/view.go`), and `MagicHelp` in `internal/kernel/kernel.go` for cell commands
   - **CLI help**: the cobra `Short`/`Long`/`Example`/flag descriptions in `main.go` (rendered by fang)
   - **Tooltips**: `tip` strings on zones and buttons (hovering shows them in the footer)
   - **Doc comments** on changed or new exported symbols
   - `examples/tour.ipynb`, if the change deserves a demo cell

3. **Audit each surface** with grep:
   - Search for the names of related keys, flags, magics and functions to find every place that already discusses the area
   - Decide for each hit whether it needs an update, a cross-reference, or no change

4. **Draft the updates**:
   - Match each surface's existing voice and format (README tables for keys and mouse, `key.NewBinding(... key.WithHelp(...))` for help)
   - Keep key names consistent everywhere: the README, `WithHelp`, footer hints and tooltips must agree
   - Verify every command, key and identifier against the source files

5. **Verify**:
   - `go vet ./...` and `go build ./...` if Go files changed (help text lives in code)
   - Check the in-app help renders without overflowing: open it with `?` in tmux at 100 and 150 columns (see /tui-check)
   - `go run . --help` if CLI help changed

6. **Report**:
   - List every file changed, and every surface deliberately left alone with a one-line reason
   - Suggest the next step (usually /commit-push with a `docs:` subject). Do not commit unless asked

## Guidelines

- Read the diff before writing anything. Invented key names or flags erode trust faster than missing docs
- Keep doc updates in their own commit, separate from code changes, where practical
- Prefer linking between README sections over duplicating content

$@
