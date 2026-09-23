---
description: Scaffold a new prompt template in .kit/prompts/
---

Create a new kit prompt template for this repository. The user wants a prompt that does: $@

## What a prompt template is

A prompt template is a `.md` file in `.kit/prompts/` (project-local) or `~/.kit/prompts/` (global). It becomes a slash command in the kit input box, typed as `/filename` with optional arguments.

Existing project prompts (check these first, and extend one rather than duplicating it):
commit-push, create-pr, file-issue, fix-issue, code-audit, update-docs, release-tagger, resolve-reviews, tui-check, new-prompt.

## File format

```
---
description: One-line description shown in autocomplete
---

Body text of the prompt. Reference user-supplied arguments
with positional placeholders (see "Argument placeholders" below).
```

- **Filename → slug**: `commit-push.md` becomes `/commit-push`
- **Frontmatter**: only `description` is recognized; keep it under ~80 characters
- **Body**: plain markdown; the full text is sent as the user's message when the template runs
- **Required args**: kit infers required positional args from the highest `$N` it finds *outside* backtick and tilde code fences. A stray `$2` in plain prose means kit will refuse to run without 2 arguments

## Argument placeholders

kit performs shell-style substitution before sending the prompt to the model:

- `$1`, `$2`, … : positional arguments (1-indexed)
- `${1}`, `${2}`, … : the same, in brace form (use when followed by letters or digits: `${1}_suffix`)
- `$@` : all arguments joined by spaces (zero or more, optional)
- `$+` : all arguments, **at least one required**
- `$ARGUMENTS` / `${ARGUMENTS}` : alias for `$@`
- `${@:N}` : arguments from the Nth onwards (1-indexed, bash-style)
- `${@:N:L}` : `L` arguments starting from the Nth

### ⚠️ Code fences and inline code keep placeholders verbatim

Anything inside triple-backtick fences, `~~~` fences, or single-backtick `inline` code spans is **left untouched**, so example code isn't corrupted. That means:

- An inline-coded `gh issue view $1` stays a literal `$1` in the model's input ❌
- The same command written in plain prose (no backticks) has its `$1` replaced by the first argument, e.g. `gh issue view 42` ✓

**Rule of thumb:** to substitute a placeholder, keep it outside backticks and fences. For a literal `$1` in the output (e.g. teaching shell syntax), put it inside backticks.

### Workarounds for "I want it to look like code AND substitute"

1. **Drop the backticks** around just the placeholder; the rest can still read as a command line in prose
2. **Use a 4-space-indented code block** instead of a triple-backtick fence. kit only skips backtick and tilde fences, so indented blocks still get substitution *and* count toward the required arguments. For example, a prompt taking an issue number could contain this indented block (shown fenced here so this template itself needs no argument):

   ```
       gh pr create --title "fix: ... (#$1)" --base master
   ```

3. **Bind once, reference loosely**: put `Issue: $1` at the top in prose, then leave the backticked examples literal. The model will substitute mentally

## Steps

1. **Understand the workflow** the user described. Ask a clarifying question if the intent is ambiguous
2. **Choose a filename**: short, lowercase, hyphen-separated and descriptive (e.g. `add-magic.md`)
3. **Write the description**: one imperative sentence that fits in autocomplete
4. **Decide on arguments**:
   - No arguments needed → omit placeholders entirely
   - One required value (issue number, file path) → use `$1`
   - Free-form trailing context → end with a single `$@` line
   - Several distinct values → use `$1`, `$2`, … and document each at the top
5. **Draft the body**, grounded in this repo:
   - Open with one sentence stating the goal, putting `$1`/`$@` where the value belongs
   - Use the repo's real commands (`go test -race ./...`, `golangci-lint run ./...`, `gofmt -l .`), its default branch (**master**), and its package names (see AGENTS.md)
   - Point to AGENTS.md for conventions rather than restating them
   - Use `## Steps` for multi-step workflows and plain prose for simple prompts
   - **Audit every backtick and code fence**: any `$N` or `$@` inside them will not expand. Was that intentional? If not, apply a workaround above
6. **Write the file** to `.kit/prompts/<slug>.md`
7. **Check substitution**: mentally (or actually) replace `$1`/`$@` with a sample value and confirm every reference resolves. Also make sure the prompt's own example snippets don't raise the required-arg count: wrap illustrative `$N` examples in triple-backtick fences, not 4-space indentation, so `RequiredArgs()` ignores them
8. **Confirm** by showing the final file and the slash command that runs it (e.g. `/add-magic timeit`)

## Guidelines

- Keep prompts action-oriented: tell kit *what to do*, not just *what to think about*
- Prefer concrete steps over vague instructions
- A prompt that does one thing well beats one that tries to cover every edge case
- If unsure about substitution, write the file and run `/<slug> testvalue` once to confirm. Misplaced backticks are the #1 failure mode
