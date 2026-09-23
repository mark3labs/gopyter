---
description: Implement the fix, feature, or docs change requested by a GitHub issue
---

Resolve GitHub issue #$1 by reading it, classifying it, and making the appropriate code or doc change. **Stop once the working tree contains the change.** Committing, pushing, and opening a PR are handled by /commit-push and /create-pr.

## Steps

1. **Fetch the issue**:
   - Run: gh issue view $1 --json number,title,body,labels,state,author,comments
   - If the issue is closed, stop and ask the user whether to proceed
   - Read the **entire** thread, including comments. The latest comment often refines the request

2. **Classify the issue** from labels, title prefix, and body content:
   - `bug` / `fix:` → reproduce, then fix
   - `enhancement` / `feat:` → design, then implement
   - `documentation` / `docs:` → locate and update docs
   - `question` → answer in a comment and do **not** write code
   - Anything else → ask the user how to proceed

3. **Create a working branch** off the default branch (**master**):
   - `git switch master && git pull --ff-only`
   - Branch name: `<type>/<issue>-<slug>`, e.g. `fix/42-popup-overlap`, `feat/57-find-in-notebook`, `docs/63-persistence`

4. **Read AGENTS.md first**, especially the Architecture notes for the package you're touching (kernel `//line` directives, serialized emit, render-time zones, `doAction`/`dialogButtons` as single sources of truth). Then do the work:

   ### Bug
   - Reproduce the failure first:
     - For kernel, notebook or completion bugs, write a failing test
     - For UI bugs, reproduce in tmux (see /tui-check) and, if possible, a model-level test (see `completion_test.go` / `dialog_test.go` for patterns)
   - If you cannot reproduce it, comment on the issue asking for clarification and stop
   - Find the root cause; do not patch symptoms
   - Add a regression test that fails before the fix and passes after

   ### Feature
   - Re-read the motivation and proposed implementation in the issue
   - For large, ambiguous, or breaking changes (e.g. the `.ipynb` format, the kernel's code generation, CLI flags), sketch the design in an issue comment and wait for sign-off before writing code
   - Keyboard and mouse are peers: new actions should get a key binding, a clickable affordance where it makes sense, and tooltips/help text. Route both through the same function
   - Add doc comments on exported symbols, and unit tests for the new behavior and edge cases
   - Update README.md and the in-app help (`fullHelp` in `internal/ui/keys.go`) when keys, flags or visible behavior change

   ### Documentation
   - Open the location named in the issue (README.md, AGENTS.md, in-app help, or godoc)
   - Apply the improvement, and verify every command, key and identifier you mention against the code

5. **Validate**: `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`, `go test -race ./...`, and a tmux check for UI changes

6. **Report**:
   - Branch name (`git branch --show-current`)
   - Files changed (`git status -s`) and the key parts of the diff
   - Test, lint and visual-check results
   - The next step:
     - /commit-push to commit (the subject should reference `(#<issue>)` and the body include `Fixes #<issue>`)
     - then /create-pr, which picks up the linked issue from the branch name and commit messages

## Guidelines

- This prompt **stops at a working tree with the change applied**. Do not run `git commit`, `git push`, or `gh pr create`
- If the issue is unclear, post a clarifying comment on the issue and stop; do not guess
- Keep the change scoped to the issue. Raise unrelated cleanups separately
- If the issue is a duplicate or already fixed on master, comment with the reference and stop
- Do not close the issue manually. The eventual PR's `Fixes #<issue>` closes it on merge
