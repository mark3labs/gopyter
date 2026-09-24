# AGENTS.md

Guidance for AI coding agents working on **gopyter**, a Jupyter-style notebook
for Go that runs in the terminal. For the user-facing overview, see `README.md`.

## Project overview

- Single Go module `github.com/mark3labs/gopyter` (Go 1.27). The CLI entry
  point is `main.go`; everything else lives under `internal/`.
- The UI is built on the Charm **v2** stack: Bubble Tea, Bubbles, Lip Gloss,
  Glamour and Fang, plus Ultraviolet and chroma. The CLI uses cobra wrapped by
  fang.
- Cells are executed by generating a Go program and running `go build`
  (modeled on [GoNB](https://github.com/janpfeifer/gonb)). Code completion
  talks to `gopls` over LSP.

| Package             | Responsibility                                                            |
|---------------------|---------------------------------------------------------------------------|
| `main.go`           | cobra commands (`gopyter`, `gopyter run`, `gopyter model`) executed through `fang`; AI wiring in `ai.go` / `ai_noai.go` |
| `internal/kernel`   | splits cells into decls/statements, persists decls, generates, builds and runs programs; `Check` compiles a cell without running it |
| `internal/ai`       | optional AI features on the kit SDK: model setting, isolated agents, the cell fix and edit loop |
| `internal/config`   | user settings (theme, vim, AI model) persisted as JSON in the user config dir |
| `internal/notebook` | `.ipynb` (nbformat v4) read/write with a GoNB kernelspec                  |
| `internal/runner`   | headless execution for `gopyter run`                                      |
| `internal/termimg`  | draws images as half-block text for image outputs (TUI and `run`)        |
| `internal/htmlview` | draws HTML outputs as text with widgets; applies the program's DOM/widget ops (`Session`) |
| `internal/kernel/runtime` | packages cell programs import: `nb` (gopyter's API) and the GoNB shim (`gonbui`, `widgets`, `comms`, `dom`, `protocol`, adapted from GoNB under its MIT license); stdlib only |
| `internal/complete` | completion engine: gopls backend plus a basic fallback                    |
| `internal/lsp`      | minimal JSON-RPC/LSP client (stdio)                                       |
| `internal/ui`       | the Bubble Tea app: editor, cells, mouse zones, dialogs, completion popup |
| `skills/gopyter`    | agent skill (`SKILL.md`, installable with `npx skills add mark3labs/gopyter`); keep it in sync with user-visible behavior |
| `scripts`           | tests for `install.sh` (run against a fake release; checked against `.goreleaser.yaml`) |

### Credit to GoNB

gopyter borrows from [GoNB](https://github.com/janpfeifer/gonb) (Jan Pfeifer,
MIT license): its execution model, notebook format, cell commands and the
`gonbui` packages. Keep that visible:

- gopyter itself is MIT-licensed (`LICENSE`).
- `internal/kernel/runtime/gonb/LICENSE` is GoNB's license: keep it with the
  shim. Release archives ship it as `third_party/gonb/LICENSE`
  (`.goreleaser.yaml`). Code or documentation adapted from GoNB goes there, and each package
  comment says what it's adapted from, with a link.
- When adding a feature that follows GoNB, say so where it's documented
  (README, `%help`, comments) and link to the project. The README's
  Acknowledgements section lists what gopyter borrows; update it too.

## Setup and commands

Requires the Go toolchain on `PATH`, because the kernel shells out to `go`.
`gopls` is optional: install it for completion and for the gopls tests,
otherwise those tests are skipped.

```sh
go build ./...                       # build
go run . examples/tour.ipynb         # run the TUI on the sample notebook
go run . run examples/tour.ipynb     # headless execution
go test ./...                        # all tests (~2s wall clock)
go test -race ./...                  # must stay race-free
go test ./internal/kernel -run TestKernel -v   # a single test
gofmt -l .                           # must print nothing
go vet ./...
golangci-lint run ./...              # v2, default config; must report 0 issues
go test -tags noai ./...             # the build without AI (no kit dependency) must pass too
```

Before finishing a change, run `gofmt -l .`, `go vet ./...`,
`golangci-lint run ./...` and `go test -race ./...`. All must be clean.
Editor diagnostics (gopls modernize hints such as "inefficient string
concatenation in WriteString") count as issues to fix too.

## Code style and conventions

- **Charm v2 import paths** are vanity domains. Use `charm.land/bubbletea/v2`,
  `charm.land/bubbles/v2/...`, `charm.land/lipgloss/v2`,
  `charm.land/glamour/v2` and `charm.land/fang/v2`. Never use the old
  `github.com/charmbracelet/{bubbletea,lipgloss,bubbles}` v1 modules.
  Ultraviolet is `github.com/charmbracelet/ultraviolet`, imported as `uv`.
- **Bubble Tea v2 API:** `View()` returns a `tea.View`. Alt-screen, mouse
  mode, cursor and window title are fields on it, not program options. Keys
  arrive as `tea.KeyPressMsg`; match them with `msg.String()` or
  `key.Matches`. Mouse input arrives as `tea.MouseClickMsg`,
  `tea.MouseMotionMsg`, `tea.MouseReleaseMsg` and `tea.MouseWheelMsg`.
- **Look before you guess:** read the dependency source under
  `$(go env GOMODCACHE)` rather than assuming an API. Several v2 APIs differ
  from v1 and from older docs.
- Handle every error. Where ignoring one is deliberate, write `_ = f()` and
  add a comment saying why (errcheck is enabled).
- Build strings with separate `WriteString` calls, not `+` inside
  `WriteString`.
- Use the palette (`col*` vars) and styles in `internal/ui/theme.go` rather
  than ad-hoc colors, so every theme applies. Themes live in `themes.go`:
  gopyter's default plus kit's presets (kept verbatim in `kitThemes`), each
  with light and dark variants.
- Keep comments explaining *why*. Exported identifiers get doc comments.

## Architecture notes (read before changing these areas)

### Kernel (`internal/kernel`)

- `parse.go` splits a cell with `go/scanner` into top-level declarations,
  which persist across cells, and statements, which go into `main()`. A
  trailing bare expression is wrapped in `Display(...)`.
- Every chunk is emitted with a `//line In[n]:line:col` directive so compile
  errors point at cell positions. Keep these intact when changing code
  generation.
- A cell's declarations only replace earlier ones after a successful build.
- `Execute` serializes calls to `emit`. Callers may assume `emit` is never
  called concurrently.
- Cell programs import runtime packages from `internal/kernel/runtime`:
  `nb` (`github.com/mark3labs/gopyter/nb`, with `nb/wire`, the transport)
  and the GoNB shim (`github.com/janpfeifer/gonb/gonbui/...`). They're
  embedded and written into the workspace as two local modules, which its
  `go.mod` requires through `replace` (`setupRuntime`, also after `%reset
  go.mod`); their repository import paths are rewritten (`rewriteImports`).
  They must stay stdlib-only (the workspace is offline) and are vetted and
  linted with the rest of the repo. `generate` adds imports for them when a
  cell uses `nb.`, `widgets.`... (`seedImports`), and declares the old
  top-level names (`Display`, `Cache`...) only when the notebook doesn't
  (`compatAliases`). The helper file (`helpersSrc`) is internal: trailing
  expressions call `gopyterDisplay`.
- Rich output goes over file descriptor 3 as JSON lines:
  `{"mime","data","id"}` displays (`Result`, `Markdown`, `Image`, `HTML`
  events) and `{"op":...}` operations (`Op` events, raw JSON: DOM changes,
  widget values, input requests, requests awaiting a reply). Events go back
  to the program on file descriptor 4 (`Input.Events`): `{"address",
  "value"}` and `{"done":true}`; its end means done too. Windows falls back
  to stdout. Events with an `ID` replace the output with that ID
  (`Cell.setOutput`); the ID isn't saved to the notebook.
- `htmlview.Session` applies `Op`s to a cell's outputs (HTML outputs are
  kept as serialized HTML and re-parsed per change) and produces replies.
  The UI and `runner` both use it; the runner sends `done` at once.
- Magics that change how a cell is built (`%%`/`%main` args, `%exec`,
  `%test`) are parsed in `parseCell` (`addMagic`); the others run as
  commands before the code (`runCommand`, `magic.go`). Cell magics
  (`%%writefile`, `%%bash`...) must be the first line and make the whole
  cell a command. `%test` cells build `main_test.go` with `go test -c`;
  only one of `main.go`/`main_test.go` may exist.
- `ExecuteInput` takes the program's `Input` (stdin and widget events).
  The UI feeds both from pipes (`input.go`): the input line under the
  running cell, the widgets drawn in its HTML outputs (`Cell.outWidgets`,
  laid out by `htmlview.Render`) and the Done button. `m.in.focus` is the
  focused item (`focusStdin` or a widget's address); `tab` cycles through
  `inputItems`. Widget mouse actions (`actWidgetClick`, `actWidgetSet`,
  `actWidgetMenu`) and keys call the same functions.
- `CompletionSource` mirrors a cell into a separate `gopyter_complete`
  sub-package. It copies chunks verbatim at their original columns, so cursor
  positions map back exactly. Preserve that property.

### UI (`internal/ui`)

- `Model` uses pointer receivers. `View()` intentionally records render-time
  layout: `m.layout` holds cell geometry and `m.zones` holds clickable
  rectangles, both rebuilt on every render and used for hit-testing.
- **Clickable elements:** register a `zone` for any new clickable element
  while rendering. Use `lineBuilder.button` inside a line, and `translate` to
  convert to screen coordinates.
- **Shared code paths for mouse and keyboard:** mouse actions go through
  `doAction`. Where a keyboard equivalent exists, call the same function (for
  example `commandKey`, `convertCell` or `dialogYes`) instead of
  re-implementing the behavior.
- Dialog buttons come from `dialogButtons()`. It is the single source for
  rendering, focus cycling (`tab`, arrows) and mouse clicks.
- Overlays (dialogs, the context menu, the completion popup) are composited
  with Ultraviolet in `composite`. While an overlay is open, only its zones
  are interactive.
- The editor (`editor.go`) is custom rather than `bubbles/textarea`. Edits go
  through `push()` for undo, and selection-aware operations must handle
  `HasSelection()`.
- Vim bindings (`--vim`, `vim.go`, `editor_vim.go`) layer NORMAL/INSERT/VISUAL
  sub-modes over edit mode. INSERT reuses `handleEditKey`; other keys go to
  `handleVimKey`, parsed as `[count] operator [count] motion`. Motions are
  `Editor.vimMotion`, and vim edits wrap in `begin`/`end` so each command is
  one undo step. Visual mode uses the editor selection with an inclusive or
  linewise `selMode`; `vimSync` converts mouse selections.
- Themes (`themepicker.go`): `applyTheme` swaps the global palette and
  rebuilds everything derived from it (styles, highlighter, glamour style,
  help/input styles, cached cell renders). Anything new that caches styled
  output must be invalidated there too.
- External edits (`watch.go`): a 1s tick polls the file (stat, then
  SHA-256 on change) off the UI goroutine. `save()` re-snapshots it and bumps
  `watch.gen` so our own writes and stale polls are ignored. Reloads wait
  while cells run or an overlay is open. `applyReload` reuses cells matched by
  ID (or by position and source for ID-less files), keeping undo and caches.
- Program output (stdout/stderr) is interpreted by a small terminal
  emulator (`termout.go`): SGR styles, `\r`, cursor movement and erasing.
  Output without escape codes skips it. Lines are wrapped when drawn and
  each is self-contained (styles reopened and reset per line).
- Completion (`completion.go`) is debounced, and responses are matched by
  sequence number. Popup keys are handled before editor keys in
  `handleKey`.

### AI (`internal/ai`, `internal/ui/assist.go`, `ai.go`)

- AI is off unless a model is set and on (`M` in the UI, `gopyter model`,
  `--model`, the `ai_model`/`ai_off` settings). `ui.Options.AI` is nil in
  the noai build; while AI is off, `Model.assistant` is nil and nothing
  AI-related may be rendered, bound or started, except `M` in the help
  overlay. Keep new AI features behind `m.assistant != nil`.
- The model picker (`modelpicker.go`) lists kit's catalog (`ai.Catalog`)
  and, on its Ollama row, the server's installed models. Switching models
  goes through `setAI`, which cancels any request of the old model.
- Agents are built with `kit.NewIsolatedAgent`: no `.kit.yml`, AGENTS.md,
  skills, extensions, MCP servers or core tools. Give them only tools that
  can't run user code, like `Kernel.Check`. `TestFixIgnoresProjectKitSetup`
  guards this.
- `Kernel.Check` builds in the `gopyter_check` sub-package, offline
  (`GOFLAGS=-mod=readonly`, `GOPROXY=off`), and never commits declarations.
- Fixes (`f`) and edits (`e`, an instruction typed in `overlayAsk`) share
  one loop: `Assistant.propose` in `internal/ai` with its own system prompt
  each, and one `aiTask` in the UI. A new kind of cell rewrite should be
  another prompt on that loop, not a second one.
- The request runs on its own goroutine and reports through `taskMsg`,
  matched by `aiTask.seq` like completion results. Only one runs at a time.
  Results are only applied after review (`overlayReview`), as one undo
  step, and the cell is not run.
- kit-using code is `//go:build !noai`; `internal/ai/ai.go` holds the
  kit-free types the UI needs, and `ai_noai.go` stubs the CLI. Tests use a
  scripted model through `kit.WithProvider`; they must never call a real
  provider.

## Testing instructions

- Put unit tests next to the code (`*_test.go`). Drive the UI model directly:
  build it with `New(Options{...})`, set `width` and `height`, and call
  `handleKey`, `handleMouseDown` and so on. See `completion_test.go` and
  `dialog_test.go` for patterns, including running returned `tea.Cmd`s
  synchronously.
- Kernel tests compile real programs, so they need `go` but no network.
- `TestExamples` (`internal/runner`) runs every notebook in `examples/`
  headlessly (skipped with `-short`). Examples must stay offline (stdlib
  and gopyter's runtime only), write files only under relative paths, and
  finish without input (widgets are done at once, stdin may be empty).
  Save them through gopyter (or `notebook.Save`) so the format matches.
  Completion tests start a real `gopls` and skip when it isn't installed.
- Add or update tests for any behavior change, and add a regression test when
  fixing a bug.
- **Verify TUI changes visually.** Run the binary in tmux and inspect the
  screen:

  ```sh
  go build -o /tmp/gopyter .
  tmux new-session -d -s gp -x 100 -y 30 "/tmp/gopyter examples/tour.ipynb"
  sleep 1
  tmux send-keys -t gp -l 'fmt.Pr'; sleep 1  # -l sends text literally
  tmux capture-pane -t gp -p                 # add -e to see colors and styles
  tmux kill-session -t gp
  ```

  Mouse input can be simulated with SGR sequences. Coordinates are 1-based;
  the final `M` is press and `m` is release; `0` is the left button, `2` the
  right, `35` is motion and `65` is wheel down:

  ```sh
  tmux send-keys -t gp -l $'\e[<0;12;4M'$'\e[<0;12;4m'   # left click at column 11, row 3 (0-based)
  ```

  tmux cannot send `shift+enter` or `ctrl+enter`. Use `ctrl+r` or `ctrl+j`,
  which do the same.

## Pull requests and commits

- The default branch is `master`.
- Keep changes focused. Update `README.md` when user-visible behavior, keys or
  flags change, and update the in-app help (`fullHelp` in `keys.go`) when
  adding shortcuts.
- Use [Conventional Commits](https://www.conventionalcommits.org/):
  `<type>(<scope>): <summary>`, in the imperative mood, lowercase, at most 72
  characters. Types: `feat`, `fix`, `refactor`, `perf`, `test`, `docs`,
  `build`, `chore`. Scopes are package names: `ui`, `kernel`, `complete`,
  `lsp`, `notebook`, `runner`, `cmd` (for `main.go`). For example:
  `feat(ui): add tab focus cycling to dialogs`.
- Don't commit build artifacts. `/gopyter` and `dist/` are ignored; build to `/tmp`.
- Reusable workflows live in `.kit/prompts/` as slash commands:
  - `/commit-push`, `/create-pr`: commit and open pull requests
  - `/file-issue`, `/fix-issue`: file and resolve issues
  - `/tui-check`: visual verification
  - `/code-audit`: read-only audit
  - `/update-docs`: documentation updates
  - `/release-tagger`: tag releases
  - `/resolve-reviews`: address review comments
  - `/new-prompt`: scaffold a new prompt

## CI and releases

- `.github/workflows/ci.yml` runs on pushes to `master` and on PRs:
  - gofmt, build, vet and `go test -race` on Linux and macOS (with gopls
    installed, so the completion tests run)
  - a CGO-disabled build
  - golangci-lint (pinned v2.13.2)
  - a GoReleaser snapshot build of every release target
- `.github/workflows/release.yml` runs on `v*` tags. It tests, then runs
  GoReleaser, which publishes Linux and macOS (amd64/arm64) tarballs and a
  checksum file. The release notes are generated from Conventional Commit
  subjects.
- `install.sh` downloads a release asset, verifies its SHA-256 and installs
  it. Asset names, supported platforms and `.goreleaser.yaml` must agree;
  `scripts/install_test.go` enforces this. When changing either, run
  `go test ./scripts/`.
- Check release config changes locally with `goreleaser check` and
  `goreleaser release --snapshot --clean` (output goes to the ignored
  `dist/`).
- Tag with `git tag -a vX.Y.Z -F <msgfile>`; see `/release-tagger`. Tags are
  GPG-signed here (`tag.gpgSign=true`), so give the message non-interactively
  or git opens an editor.

## Security considerations

- Running a cell executes arbitrary user code and `!shell` commands with the
  user's privileges. Never execute notebook content in tests or tooling
  except from fixtures you control.
- Kernel workspaces are temporary directories removed by `Kernel.Close()`.
  Always close kernels and completion engines, including in tests via
  `t.Cleanup`.
