---
description: Open a GitHub PR for the current branch, grounded in its commits and diff
---

Open a GitHub pull request for the current branch with a description grounded in the actual commits and diff. The default branch of this repo is **master**.

## Steps

1. **Verify the branch**:
   - If the current branch is `master`, stop: PRs need a feature branch. Suggest creating one, e.g. `git switch -c feat/<slug>`
   - `git status -sb` and `git log @{u}..HEAD --oneline 2>/dev/null`. If there is no upstream or there are unpushed commits, push with `git push -u origin "$(git branch --show-current)"`
   - If the working tree is dirty, stop and tell the user to commit first (suggest `/commit-push`)
2. **Gather context**:
   - `git fetch origin master`
   - `git log origin/master..HEAD --oneline` lists the commits going into the PR
   - `git diff origin/master...HEAD --stat`, then `git diff origin/master...HEAD`, to read the actual changes
   - Identify the linked issue from commit messages, the branch name (`fix/42-...`), or the user input: $@. Capture it as `Fixes #N` if applicable
3. **Locate a PR template**: check `.github/pull_request_template.md` and `.github/PULL_REQUEST_TEMPLATE.md`. This repo currently has none, so use the structure below
4. **Draft the PR body**:

   ```markdown
   ## Description
   <1–3 short paragraphs: what changed and why, grounded in the diff.
   For UI changes, include a short tmux capture (see /tui-check) showing the result.>

   Fixes #N   <!-- only if there is a real linked issue -->

   ## Type of Change
   - [ ] Bug fix
   - [ ] New feature
   - [ ] Refactor
   - [ ] Documentation
   - [ ] Build / tooling

   ## Checklist
   - [ ] `gofmt -l .` clean, `go vet ./...` and `golangci-lint run ./...` pass
   - [ ] `go test -race ./...` passes
   - [ ] Tests added or updated for the change
   - [ ] UI changes verified visually in tmux
   - [ ] README / in-app help (`fullHelp` in `internal/ui/keys.go`) / AGENTS.md updated if behavior, keys or flags changed

   ## Additional Information
   - Files added / modified, and any backward-compatibility notes
     (e.g. `.ipynb` format changes, new CLI flags)
   ```

   Tick the single most accurate Type of Change box, and only tick checklist items that are genuinely true
5. **Write the body to a temp file**: `/tmp/pr-body-<branch>.md`. Never pass a long body inline via `--body`; always use `--body-file`
6. **Choose the title**: prefer the subject of the primary commit if it already follows Conventional Commits; otherwise write one in the same style (`<type>(<scope>): <imperative summary>`, ≤72 chars)
7. **Create the PR**:

       gh pr create --title "<title>" --body-file /tmp/pr-body-<branch>.md --base master --head "$(git branch --show-current)"

8. **Report the PR URL** returned by `gh` and stop

## Guidelines

- Read the diff and commit messages. Do **not** invent features that aren't in the code
- One PR per logical change. If the branch contains unrelated commits, point that out and ask before continuing
- Keep the description focused on what reviewers need (what and why), not a replay of the diff
- If `gh auth status` fails, stop and tell the user

$@
