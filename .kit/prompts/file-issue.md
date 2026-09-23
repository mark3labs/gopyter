---
description: File a GitHub issue (bug, feature, or docs) with a structured body
---

File a GitHub issue for the gopyter repository. The user wants to create an issue about: $@

## Issue types

This repository has no issue templates, so give each issue the right **label** and a structured body:

| Type | Label | Title prefix | Use for |
|------|-------|--------------|---------|
| Bug | `bug` | `fix:` | Something is broken or not working as expected |
| Feature | `enhancement` | `feat:` | New feature, enhancement, improvement |
| Docs | `documentation` | `docs:` | Missing, incorrect, or unclear documentation |

## Steps

1. **Determine the issue type** from the user input
2. **Investigate before writing**: grep the code to find the relevant package and file:line (see the package table in AGENTS.md). For bugs, try to reproduce with `go run . <notebook>`, `go run . run <notebook>`, or a failing test. For UI bugs, use a tmux capture (see /tui-check)
3. **Ask clarifying questions** only if critical information is missing and can't be found by investigating:
   - Bugs: "What were you doing when this happened?"
   - Features: "What problem does this solve?"
   - Docs: "Where did you look for this information?"
4. **Craft the title**: `<prefix> <short description>`, lowercase, imperative mood, ≤72 chars
   - `fix: completion popup covers the cursor line near the bottom edge`
   - `feat: add a find-in-notebook command`
   - `docs: explain how variables persist between cells`
5. **Write the body** to `/tmp/issue-body.md` using the matching structure:

   **Bug** (`bug`)
   ```markdown
   ## Bug Description
   What happened vs. what was expected.

   ## Steps to Reproduce
   1. …

   ## Relevant Code / Output
   Cell source, error output, or a tmux capture of the screen.

   ## Affected Component
   kernel | ui | complete | lsp | notebook | runner | cmd

   ## Environment
   gopyter commit (`git rev-parse --short HEAD`), `go version`, `gopls version`, terminal emulator
   ```

   **Feature** (`enhancement`)
   ```markdown
   ## Feature Description
   What to add or change. Be specific about behavior, keys and mouse interactions.

   ## Motivation / Use Case
   The problem it solves, the current workaround, and who benefits.

   ## Proposed Implementation (optional)
   High-level approach, affected packages, and example usage (cell code, keybindings).
   ```

   **Docs** (`documentation`)
   ```markdown
   ## Documentation Issue
   What's wrong or missing.

   ## Documentation Location
   README.md section, AGENTS.md, in-app help (`fullHelp` in internal/ui/keys.go), or godoc.

   ## Suggested Improvement
   How to fix it.
   ```

6. **Create the issue**:

       gh issue create --title "<title>" --label <label> --body-file /tmp/issue-body.md

7. **Confirm success**: show the issue URL, number, and label used

## Guidelines

- Include file paths and line numbers when you know them
- Keep the body factual. Keep speculation to the Proposed Implementation section
- For features, describe the problem first, then the solution. Keep gopyter's design in mind: keyboard-driven and Jupyter-like, with mouse support as a peer to the keyboard
- If you're unsure about technical details, say so in the issue
