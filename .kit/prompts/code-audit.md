---
description: Read-only audit for dead code, duplication, boundary violations and refactors
---

Perform a thorough **read-only** audit of this repository and report the findings. **Do not edit, rename, or delete any files.** Optional focus or scope hints from the user: $@

## Scope

If the user gave focus hints above (a package such as `internal/ui`, or a concern such as "completion" or "mouse"), scope the audit to that. Otherwise audit the whole repo, starting with the largest and busiest packages: `internal/ui`, then `internal/kernel`, `internal/complete`, `internal/lsp`, `internal/notebook`, `internal/runner`, and `main.go`.

## Steps

1. **Map the repo first**:
   - List every Go package and its size (`find . -name '*.go' | xargs wc -l | sort -n`)
   - Read `AGENTS.md` and `README.md` to learn the intended boundaries and documented invariants. These define what counts as a violation

2. **Hunt for dead code**:
   - Run `go vet ./...` and `golangci-lint run ./...` and capture their output
   - If `deadcode` is available (`go run golang.org/x/tools/cmd/deadcode@latest ./...`), run it and include its output verbatim
   - Everything is under `internal/`, so "exported" doesn't mean "public". Grep exported and unexported symbols and cross-check call sites; symbols with no non-test references are suspects
   - Check for unused action kinds (`act*` constants in `internal/ui/mouse.go`), theme styles in `theme.go`, and key bindings in `keys.go` that nothing references
   - Look for commented-out blocks, `// TODO` markers, and pointless `_ = x` discards
   - **Do not delete anything.** List candidates with file:line and a confidence level (high / medium / low)

3. **Find unnecessary duplication**:
   - Known hot spots: output appending (`appendOutput` in `internal/ui/cell.go` vs `appendOut` in `internal/runner/runner.go`), markdown rendering (the UI vs the runner), output styling (the UI vs the runner), and word/identifier helpers (`isWord` in the editor vs `isIdent`/`wordLen` in `internal/complete`)
   - Tell *coincidental* duplication (things that look alike but will change independently) from *unnecessary* duplication (same intent, has to change in lockstep). Flag only the latter
   - For each cluster, suggest where a shared helper should live and whether that would cross a package boundary

4. **Check boundary violations** against the AGENTS.md architecture notes:
   - `internal/kernel` must not import `internal/ui`, `internal/complete` or any Charm library; it is UI-agnostic
   - `internal/lsp` must stay a generic LSP client with no gopyter or gopls specifics (those belong in `internal/complete`)
   - `main.go` should only wire things together; business logic belongs in `internal/`
   - **Mouse / keyboard parity**: mouse actions should go through `doAction` and reuse keyboard code paths. Flag any behavior implemented twice
   - **Dialogs**: buttons must come only from `dialogButtons()`
   - **Kernel**: code generation must keep the `//line` directives; `CompletionSource` must copy cell chunks verbatim at their original columns
   - For each violation, cite the offending import or code with file:line

5. **Spot refactor opportunities**:
   - Long functions (>80 lines) doing several unrelated things. Likely candidates: `renderCell`, `handleEditKey`, `handleCommandKey`, `openContextMenu`, `doAction`, `Execute`
   - Deeply nested conditionals that early returns would flatten
   - Structs with many fields that suggest split responsibilities (`Model` is a candidate: group mouse, completion and dialog state?)
   - Test setup boilerplate that a helper could replace
   - For each: location, current shape (1–2 lines), proposed shape (1–2 lines), and risk (low / medium / high)

6. **Cross-check against project rules**: re-read the AGENTS.md architecture notes and drop any "refactor" that would break a documented invariant, for example removing the serialized emit or moving zone registration out of render. Say that you dropped it and why

7. **Write the report** as your final message (do not write it to disk):

   ```
   # Code Audit Report

   ## Summary
   - N dead-code candidates
   - N duplication clusters
   - N boundary violations
   - N refactor opportunities

   ## Dead Code
   ### High confidence
   - path/to/file.go:LINE — symbol — reason
   ### Medium confidence
   …

   ## Duplication
   ### Cluster: <short name>
   - Sites: file:line, file:line, …
   - Suggested home: package/path
   - Notes: …

   ## Boundary Violations
   - Rule: <which AGENTS.md rule or convention>
   - Offender: file:line
   - Fix sketch: …

   ## Refactor Opportunities
   - Location: file:line
   - Current: …
   - Proposed: …
   - Risk: low/medium/high
   - Why it's worth it: …

   ## Suggested Next Steps
   1. …
   ```

8. **End the report with an explicit reminder** that no files were modified. Recommend picking the highest-value items to act on one at a time (for example via /file-issue then /fix-issue) rather than a sweeping refactor

## Guidelines

- **Read-only, always**: no edits, no writes, no `git commit`, no `go mod tidy`. Use only read, grep, find, ls, and read-only commands (`go vet`, `go build -o /tmp/...`, `golangci-lint run`, `deadcode`)
- **Cite every finding** with `path/to/file.go:LINE`
- **Be honest about confidence**: false positives are expensive, so prefer "medium confidence, worth a look" over confidently wrong claims
- **Quality over quantity**: 10 sharp findings beat 100 nitpicks. Cut anything purely stylistic
- **Don't propose architectural rewrites**: recommend small, reviewable changes that fit the existing structure
