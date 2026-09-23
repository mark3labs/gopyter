---
description: Fix review findings on the current PR, push, and loop until clean
---

Resolve all review findings on a pull request. Verify each finding against the current code, fix the valid ones, push, wait for any re-review, and repeat until no actionable comments remain. Extra context from the user: $@

gopyter currently has no CI and no review bot configured. If a bot such as CodeRabbit is added, the bot-specific sections below apply. Otherwise, treat human review comments the same way and skip the polling and rate-limit steps.

## Identify the PR

- If the user input names a PR number, use it. Otherwise resolve the PR for the current branch:

      gh pr view --json number,headRefName,state -q '{number: .number, branch: .headRefName, state: .state}'

- If the PR is merged or closed, stop and tell the user
- If the working tree has unrelated changes, stop and suggest /commit-push first
- The repo slug is `mark3labs/gopyter` (confirm with `gh repo view --json nameWithOwner -q .nameWithOwner`)

## Fetch the findings

Pull **both** comment surfaces, since reviewers use them differently:

1. **Review-level bodies** (summaries, "changes requested"):

       gh pr view <pr> --json reviews --jq '.reviews[] | {author: .author.login, state, body}'

2. **Line comments** (the individual findings):

       gh api repos/mark3labs/gopyter/pulls/<pr>/comments --jq '.[] | "=== \(.id) \(.user.login) \(.path):\(.line) ===\n\(.body)"'

Read the **full body** of each comment. Bots such as CodeRabbit include severity markers (🟠 Major / 🟡 Minor), committable suggestions, a `🤖 Prompt for AI Agents` block and "Also applies to" line lists, and they all matter.

## Triage each finding: verify before fixing

Check every finding against the **current** code, because the comment may be outdated:

- **Still valid** → fix it, keeping the change minimal and scoped to the finding
- **Already addressed** (by a later commit) → skip, and note the commit that fixed it
- **Intentional behavior** the reviewer misread → skip, and reply on the thread explaining why:

      gh api repos/mark3labs/gopyter/pulls/<pr>/comments/<comment-id>/replies -f body="..."

- **Wrong or out of scope** → skip, with a brief reason in your report; don't silently ignore it

Never blindly apply a committable suggestion; read the surrounding code first. Check the findings against the AGENTS.md architecture notes especially. A "fix" that, say, removes the serialized emit in the kernel, bypasses `doAction`, or strips the `//line` directives reintroduces a known problem and must be declined with an explanation.

## Fix, validate, push

1. Apply the fixes. Add or extend tests when a finding exposed a real gap; a fixed bug deserves a regression test
2. Run everything AGENTS.md requires:
   - `gofmt -l .` (must print nothing), `go vet ./...`, `golangci-lint run ./...`
   - `go test -race ./...`
   - For UI-affecting fixes, a visual check in tmux (see /tui-check)
3. Commit with a Conventional Commit subject that references the review, e.g.:

       git commit -m "fix(<scope>): address review on <topic> (#<pr>)"

   Body: one bullet per finding fixed, and one line per finding skipped with the reason
4. `git push` (never `--force` during the loop)

## Poll for a bot re-review (only if a bot is configured)

1. Get the pushed SHA: `git log -1 --format=%H`
2. Poll the commit status until the bot reports completion:

       gh api repos/mark3labs/gopyter/commits/<sha>/status --jq '{state, statuses: [.statuses[] | {context, state, description}]}'

   Wait 90–240 seconds between checks; reviews usually land in 2–5 minutes
3. If no review lands after about 3 polls, check for a rate-limit pause: an issue comment from the bot saying "Please wait N minutes". Wait out that **exact** cooldown plus about 60 seconds, then request a review once with `gh pr comment <pr> --body "@coderabbitai review"`. Never request again while a pause is active; early requests reset the timer

## Check for new findings and loop

1. Re-fetch the line comments and compare them with the set you already handled (new findings have new IDs)
2. Check that the old threads resolved:

       gh api graphql -f query='query { repository(owner: "mark3labs", name: "gopyter") { pullRequest(number: <pr>) { reviewThreads(first: 50) { nodes { isResolved isOutdated path } } } } }'

3. New actionable comments → back to *Triage*
4. A bot thread still open for a finding you fixed → reply once on that thread: `@coderabbitai please check that this has been addressed.`
5. No comments left and every thread resolved or outdated → done

Stop early only if the PR is closed or merged, a finding needs a human product decision (say which one and why), or the user tells you to stop.

## Report

- Findings fixed (with severity), findings skipped (with reasons), and threads replied to
- Commits pushed this session (`git log --oneline` of the new commits)
- Final state: review status and unresolved thread count (target: 0)
- Anything intentionally left open

## Guidelines

- Verify every finding against the current code before touching anything
- Keep each loop iteration to a single commit; don't mix review fixes with unrelated work
- Reply on threads when skipping for "intentional behavior". Silent skips look like neglect
