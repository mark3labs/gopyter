# ◆ gopyter

A Jupyter-style notebook for **Go** that runs in your terminal.

gopyter builds on the work of [GoNB](https://github.com/janpfeifer/gonb), the Go
kernel for Jupyter by Jan Pfeifer: it runs cells the same way, shares its
notebook format and cell commands, and implements its `gonbui` API and widgets,
so GoNB notebooks run in gopyter too. See [Acknowledgements](#acknowledgements).

```
 ◆ gopyter   hello.ipynb ●                                        gopls  │  go1.27.1  │  ● idle
── ▶ run ─ ▶▶ run all ─ ■ stop ─ ↻ restart ───┼─ + code ─ + markdown ───┼─ save ─ ◐ theme ─ ? help ─
         ╭─ go ───────────────────────────────────────────────────────────────────── ✓ 137ms ─╮
     [1] │ 1  type Point struct{ X, Y float64 }                                               │
         │ 2                                                                                  │
         │ 3  func (p Point) Dist() float64 { return math.Hypot(p.X, p.Y) }                   │
         ╰────────────────────────────────────────────────────────────────────────────────────╯

▌        ╭─ go ───────────────────────────────────────────────────────────────────── ✓ 144ms ─╮
▌    [2] │ 1  p := Point{3, 4}                                                                │
▌        │ 2  p.Dist()                                                                        │
▌        ╰────────────────────────────────────────────────────────── ▶ ─ ⇄md ─ ↑ ─ ↓ ─ ⧉ ─ ✕ ─╯
▌ Out[2]   5
 COMMAND   ↵ edit · ⇧↵/^r run & next · m to markdown · b insert below · dd delete · ^s save · ? help
```

- **Real Go, cell by cell.** Declarations persist across cells, the last expression is displayed, and compile errors point at cell lines
- **IDE-style completion** from [gopls](https://go.dev/gopls), aware of everything earlier cells declared
- **Symbol info.** `alt+k` (or `K` in vim) shows the signature and docs of the function, type or variable under the cursor
- **Keyboard and mouse.** Jupyter's modal keys, plus clickable toolbar, cell actions, context menus and text selection
- **Standard `.ipynb` files** with a [GoNB](https://github.com/janpfeifer/gonb) kernelspec, so notebooks also open in Jupyter, and GoNB notebooks (cell commands, `gonbui`, widgets) run in gopyter
- **Themes.** gopyter's own Go-blue look plus the themes of [kit](https://github.com/mark3labs/kit) (catppuccin, dracula, tokyonight, gruvbox, nord…), with light and dark variants
- **Headless runs** for scripts and CI: `gopyter run notes.ipynb --save`
- **Live reload.** Edits made to the notebook file by other programs show up automatically
- **Optional AI.** Off until you pick a model; then `e` asks it to write or change a cell as you describe, and `f` to fix a failing one. Every proposal is checked to compile, and you see the diff before anything changes

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/mark3labs/gopyter/master/install.sh | bash
```

The script installs a checksum-verified release binary for Linux or macOS
(amd64/arm64). Alternatively:

```sh
go install github.com/mark3labs/gopyter@latest
```

**Requirements:** the [Go toolchain](https://go.dev/dl/) on your `PATH`, since
cells are compiled with it. For completion and symbol info, install
`go install golang.org/x/tools/gopls@latest` (without it, gopyter falls back to
basic completion).

### Agent skill

To teach a coding agent (Claude Code, Codex, Cursor, …) to use gopyter and
write notebooks, install the [skill](skills/gopyter/SKILL.md) from
[skills.sh](https://skills.sh/mark3labs/gopyter):

```sh
npx skills add mark3labs/gopyter
```

## Quickstart

```sh
gopyter hello.ipynb     # opens (or creates) a notebook, in edit mode
```

1. Type some Go and press **`ctrl+r`** (or `shift+enter`) to run it and move to a new cell:

   ```go
   type Point struct{ X, Y float64 }

   func (p Point) Dist() float64 { return math.Hypot(p.X, p.Y) }
   ```

2. Use it in the next cell. The last expression is displayed as `Out[n]`:

   ```go
   p := Point{3, 4}
   p.Dist()
   ```

3. Press **`esc`** for command mode: `j`/`k` move between cells, `b` inserts
   one, `m` turns a cell into markdown, `dd` deletes, and `?` shows every shortcut.
4. **`ctrl+s`** saves; **`q`** quits.

For a guided tour: `gopyter examples/tour.ipynb`, then press `A` to run all
cells. More example notebooks show off the rest:

| Notebook                     | Shows                                                        |
|------------------------------|--------------------------------------------------------------|
| `examples/tour.ipynb`        | the basics: declarations, results, markdown, streaming       |
| `examples/images.ipynb`      | images, animation with `nb.DisplayID`, Game of Life, plotting |
| `examples/terminal.ipynb`    | colors, progress bars, full-screen animation, reading input  |
| `examples/data.ipynb`        | variables across cells, tables, charts, HTML, `nb.Cache`     |
| `examples/widgets.ipynb`     | sliders, buttons and selects driving live output             |
| `examples/magics.ipynb`      | `!` commands, `%%writefile`, `%%sh`, flags, `%test`, `%goflags` |

They run headless too: `gopyter run examples/images.ipynb`.

## How cells run

- Top-level `func`, `type`, `var`, `const` and `import` declarations **persist**.
  Re-running a cell replaces what it declared. Everything else runs inside a
  generated `main()` in a fresh process.
- Variables declared with `:=` at the top level of a cell **carry over** too:

  ```go
  a := 1           // cell 1
  fmt.Println(a)   // cell 2 prints 1
  ```

  gopyter declares them at package level. Each cell's program restores their
  values when it starts and saves them with `encoding/gob` when it ends, even
  after a panic, so changes made in later cells stick. Only what gob can encode
  is kept, which means exported struct fields. A non-nil `error` keeps its
  message. Values gob can't encode (channels, mutexes, `*regexp.Regexp`, ...)
  aren't kept, and gopyter says so. Pointers are restored as copies.
  `f := func(...) {...}` becomes a persisted declaration. Variables inside
  blocks (`for`, `if`, ...) stay local.
- A persisted `var` is re-initialized in every later cell (each cell is a new
  process). To compute an expensive value once, wrap it in `nb.Cache` or
  `nb.CacheErr`, which store the result (gob-encoded, so exported fields only)
  in the kernel workspace:

  ```go
  var resp, err = nb.CacheErr("resp", func() (*jev.Response, error) {
      return client.SystemOne(ctx, msg, criteria)
  })
  ```

  `CacheErr` does not store failed results. `%cache clear resp` forces a recompute.
- Imports are added automatically, and third-party modules are fetched on first use.

## Rich output

gopyter's API is package `nb` (`github.com/mark3labs/gopyter/nb`), which cells
use without importing it:

| Function                        | Shows                                          |
|---------------------------------|------------------------------------------------|
| `nb.Display(v...)`              | values; an `image.Image` is drawn              |
| `nb.DisplayMarkdown(s)`         | rendered markdown                              |
| `nb.DisplayPNG(data)`           | PNG bytes                                      |
| `nb.DisplayID(id, v...)`        | like `Display`, replacing the last output with the same id |
| `nb.DisplayMarkdownID(id, s)`   | like `DisplayMarkdown`, replacing by id        |
| `nb.Cache`, `nb.CacheErr`       | (see above)                                    |

A cell's trailing expression is displayed too. Notebooks written before `nb`
can keep calling `Display`, `DisplayMarkdown`, `Cache`, ... without the `nb.`
prefix: gopyter still declares these names, but only when the notebook doesn't
declare them itself, so a notebook can have its own `Display`.

- Images are drawn in the output: `nb.Display` an `image.Image` (or end the
  cell with one), or pass PNG bytes to `nb.DisplayPNG(data)`. They are drawn with
  colored half blocks (two pixels per character), scaled to fit the cell, and
  saved in the notebook as `image/png`, so Jupyter shows them too. Images in
  notebooks from Jupyter (PNG, JPEG, GIF) are shown as well. `gopyter run`
  draws them when its output is a color terminal and prints `[image WxH]`
  otherwise.
- `nb.DisplayID(id, v...)` and `nb.DisplayMarkdownID(id, s)` replace the output
  they showed earlier with the same id, for animations and progress.
- Program output keeps its colors, and is shown like a terminal would show it:
  `\r`, cursor movement and clearing the screen work, so progress bars and
  redrawing demos look right.
- Programs can read their standard input. While a cell runs, an input line
  appears under it: press `enter` on the cell (or `alt+i`, or click it), type,
  and press `enter` to send a line or `ctrl+d` to end the input (see
  [Widgets](#widgets-and-gonb-notebooks) for what else it ends). `gopyter run`
  passes its own standard input to the programs.

Cells can also hold commands and magics. Most follow
[GoNB](https://github.com/janpfeifer/gonb)'s special commands and behave the
same way:

| Cell command     | Effect                                        |
|------------------|-----------------------------------------------|
| `!cmd`           | run a shell command in the kernel workspace (e.g. `!go get pkg@v1`); a trailing `\` continues it |
| `%reset`         | forget all declarations and saved variables   |
| `%reset go.mod`  | start the workspace's `go.mod` over           |
| `%env K=V`       | set environment variables for your programs   |
| `%args a b`      | set program arguments                         |
| `%% [args]`      | call `flag.Parse()` first, with these arguments for this cell (also `%main`) |
| `%exec fn [args]`| run `fn()` after parsing flags                 |
| `%test [flags]`  | build with `go test` and run the cell's tests and benchmarks |
| `%goflags [flags]` | show or set extra `go build` flags (`-race`, `-tags=x`); `%goflags ""` clears them |
| `%autoget` / `%noautoget` | fetch missing modules automatically, or not |
| `%capture [-a] file` | also write the cell's output to a file   |
| `%ls`            | list persisted declarations                   |
| `%rm name...`    | forget declarations or imports                |
| `%cache`         | list cached values                            |
| `%cache clear [key...]` | delete cached values                   |
| `%workspace`     | print the kernel workspace directory          |
| `%version`       | print the gopyter and Go versions             |
| `%help`          | show this list                                |

Cell magics must be the first line of a cell and take the rest of it:

| Cell magic              | Effect                                      |
|-------------------------|---------------------------------------------|
| `%%writefile [-a] file` | write the cell to a file (`-a` appends)     |
| `%%bash`, `%%sh`        | run the cell as a shell script              |
| `%%script cmd`          | run `cmd` with the cell as its input (e.g. `%%script python3`) |

Files and scripts are relative to the directory programs run in, so a program
can read what `%%writefile` wrote. Commands can also be written as
`//gonb:%...`, and `!*cmd` is accepted like `!cmd`, so GoNB notebooks run
unchanged. Each cell can have its own `func init()`; GoNB's `init_xxx()`
functions work too.

## Widgets and GoNB notebooks

gopyter implements [GoNB](https://github.com/janpfeifer/gonb)'s notebook
packages (`gonbui`, `widgets`, `comms`, `dom`), adapted from GoNB's own, so GoNB
notebooks run unchanged and cells can use its widgets. GoNB draws them in
Jupyter with HTML and JavaScript; gopyter draws them in the terminal:

```go
import "github.com/janpfeifer/gonb/gonbui/widgets"

%%
iters := widgets.Slider(10, 500, 100).Done()
for n := range iters.Listen().LatestOnly().C {
    nb.DisplayID("fractal", Mandelbrot(320, 240, n)) // your own func
}
```

- **Widgets**: `widgets.Button`, `widgets.Slider` and `widgets.Select`, with
  GoNB's builder API (`Done`, `Listen`, `Value`, `SetValue`, `AppendTo`...).
  They are drawn in the cell's output while it runs. Press `enter` on the
  running cell to focus the first one and `tab` to move between them and the
  input line. `←`/`→` move a slider (`pgup`/`pgdown` by 10%) or change a
  select, and `enter` presses a button or lists a select's options. The mouse
  works too: click a slider's track, a button, or a select.
- **Done**: the `✓ done` button under the cell (or `ctrl+d`) tells the program
  the user is done. It closes every `Listen` channel, so `for v := range
  ch.C` loops end, and ends stdin. Interrupting (`ctrl+c`) stops the program
  instead. With `gopyter run` nobody can use the widgets, so they are done at
  once.
- **`gonbui`**: `DisplayHTML`, `DisplayMarkdown`, `DisplayImage`, `UpdateHTML`
  and `UpdateMarkdown` (updatable by id), `RequestInput` (focuses the input
  line, hidden for passwords), `EmbedImageAsPNGSrc`...
- **HTML** output is drawn as text: formatting, headings, lists, tables,
  preformatted text and `data:` images; styles and scripts are ignored, SVG
  shows a placeholder. It's saved as `text/html`, so Jupyter shows the real
  thing.
- **`dom`** changes displayed HTML by element id (`Append`, `SetInnerHtml`,
  `SetInnerText`, `GetInnerHtml`, `Remove`...), and **`comms`** exchanges
  values with the front-end by address (`Listen`, `Send`, `ReadValue`).
- JavaScript can't run in a terminal: `dom.TransientJavascript` is ignored,
  script loaders return an error, and `gonbui/plotly` and `%wasm` aren't
  available.

These packages ship with gopyter and need no download: the kernel workspace's
`go.mod` points `github.com/janpfeifer/gonb` and `github.com/mark3labs/gopyter`
at local copies. They are gopyter's implementation, not GoNB itself: things
that need a browser are missing (see above), and the rest may differ in
details. In a cell starting with `%%`,
as in GoNB, variables stay in that cell.

## Keys

gopyter is modal like Jupyter: **command mode** (blue) works on cells, **edit
mode** (green) edits text. Press `?` for the full list.

| Key                          | Action                                   |
|------------------------------|------------------------------------------|
| `enter` / `esc`              | edit mode / command mode                 |
| `ctrl+r` or `shift+enter`    | run cell and advance                     |
| `ctrl+j` or `ctrl+enter`     | run cell                                 |
| `A` / `R` / `ctrl+c`         | run all / restart kernel / interrupt     |
| `alt+i`                      | type input for the running program       |
| `enter` on a running cell    | focus its widgets (`tab` next, `ctrl+d` done) |
| `j` `k` `g` `G`              | move selection                           |
| `a` / `b`                    | insert cell above / below                |
| `dd` / `z`                   | delete / undo delete                     |
| `x` `c` `v`, `K` / `J`       | cut / copy / paste, move cell up / down  |
| `m` / `y` (`ctrl+t` editing) | convert to markdown / code               |
| `o` / `O`                    | fold long output / clear output          |
| `ctrl+s` / `q`               | save / quit                              |
| `T`                          | pick a color theme                       |
| `V`                          | turn vim bindings on / off (saved)       |
| `M`                          | pick the AI model, or turn AI on / off   |
| `e`                          | ask AI to write or change a cell (when AI is on) |
| `f`                          | fix a failing cell with AI (when AI is on) |

In edit mode: `tab` or `ctrl+space` completes, `shift`+arrows select, `ctrl+c`/`ctrl+x`
copy/cut, `ctrl+z`/`ctrl+y` undo/redo, and `↑`/`↓` flow between cells.
`alt+k` (or `F1`) shows the signature and documentation of the symbol under the
cursor, or of the enclosing call when the cursor is among its arguments;
`pgup`/`pgdn` scroll it and any other key dismisses it.
`shift+enter` and `ctrl+enter` need a terminal with the kitty keyboard protocol
(kitty, Ghostty, WezTerm, foot…); `ctrl+r` and `ctrl+j` work everywhere.

### Vim bindings

Press `V` in command mode to turn vim keys on or off. The setting is saved
alongside the theme (`"vim": true` in the config file described under
[Themes](#themes)), so it sticks across sessions; `gopyter --vim` or
`--vim=false` overrides it for one session. With vim keys on, edit mode has
vim's own modes, shown in the footer: `enter` opens a cell in **NORMAL**, `esc`
goes from INSERT to NORMAL and from NORMAL back to command mode. New cells open
in INSERT.

| Keys                                   | Action                                   |
|----------------------------------------|------------------------------------------|
| `i` `a` `I` `A` `o` `O`                | insert / append / open a line            |
| `h` `j` `k` `l` `w` `b` `e` `W` `B` `E`| move (`j`/`k` flow between cells)        |
| `0` `^` `$` `gg` `G`                   | line start / end, first / last line      |
| `d` `c` `y` + motion, `dd` `cc` `yy`   | delete / change / yank (with counts: `3dw`, `2dd`) |
| `x` `X` `D` `C` `s` `S` `Y` `J`        | the usual shorthands                     |
| `p` `P`                                | put after / before                       |
| `v` `V`, then `d` `c` `y` `o`          | visual and visual-line mode              |
| `u` / `ctrl+r`                         | undo / redo                              |
| `K`                                    | symbol info (signature and docs)         |

Yanks also go to the system clipboard, and a mouse selection turns into a
visual selection. Since `ctrl+r` is redo in NORMAL mode, run cells from there
with `shift+enter` or `ctrl+j` (`ctrl+r` still runs from INSERT and command mode).

## Mouse

Click a cell to edit it, and drag, double-click or triple-click to select text.
Hover `[n]` and click `[▶]` to run a cell; the buttons on a cell's border run,
convert, move, duplicate or delete it. Right-click opens a context menu, and the
toolbar under the title covers running, adding cells, saving, themes and help. Hovering
any button shows what it does and its shortcut. Hold `shift` while dragging to
use your terminal's own selection instead (`alt`/`option` in some terminals).

## Themes

Press `T` (or click **◐ theme**) to pick a theme. Moving through the list
previews each one live; `enter` keeps it and `esc` restores the previous one.
The default is gopyter's own theme; the others are the built-in themes of
[kit](https://github.com/mark3labs/kit). Each has a light and a dark variant,
chosen from your terminal's background.

The choice is saved to `$XDG_CONFIG_HOME/gopyter/config.json` (usually
`~/.config/gopyter/config.json`; `~/Library/Application Support/gopyter/` on
macOS):

```json
{ "theme": "catppuccin" }
```

```sh
gopyter themes                     # list themes, marking the active one
gopyter --theme dracula notes.ipynb  # use a theme for this session only
gopyter --syntax-theme monokai     # override the code highlighting (any chroma style)
```

## AI (optional)

AI features are off, and invisible, until you choose a model. gopyter uses
[kit](https://github.com/mark3labs/kit), so any provider kit supports works,
including local models through [Ollama](https://ollama.com).

Press `M` to pick a model from kit's catalog: type to filter, `enter` to use
it. Models whose provider has no API key are dimmed, with the variable to set.
**Ollama** lists the models installed on your Ollama server (`OLLAMA_HOST`,
`localhost:11434` by default). **Off** turns AI off but remembers the model,
and the picker opens on whichever does the opposite of the current state, so
`M` `enter` toggles AI on and off. `M` is only listed in the help (`?`) while
AI is off; nothing else shows. The same from the command line:

```sh
gopyter model                      # show the setting and which providers have a key
gopyter model anthropic            # a provider's default model
gopyter model openai/gpt-5.6       # or any provider/model
gopyter model ollama/qwen3-coder   # a local model, no key needed
gopyter model off                  # turn AI off (the model is remembered)
gopyter --model off notes.ipynb    # for one session only (like --theme)
```

API keys come from the provider's usual environment variable, like
`ANTHROPIC_API_KEY` or `OPENAI_API_KEY`. gopyter never stores them. The model
is saved as `"ai_model"` (and `"ai_off": true` while off) in the settings file
described under Themes.

To have a code cell written or changed, press `e` (or click **✦** on the cell,
or use the context menu) and say what it should do: "read data.csv and sum the
second column", "draw a histogram of xs", "make this concurrent". On an empty
cell the model writes it from scratch. What you typed is kept until a change is
applied, so after **Discard** or an error, `e` lets you refine the request
instead of retyping it.

When a cell fails, press `f` (or click **✦ fix** on the cell, or use the context
menu) to have it fixed.

Either way, the model gets the cell, its error if it failed, and the code of the
cells above it, but not their outputs. Its proposals are compiled with the rest
of the notebook, without running anything, and it retries until one compiles.
You then see a diff: **Apply** replaces the cell (undo with `ctrl+z` while
editing), **Discard** keeps it. The cell isn't run for you. `esc` cancels a
request in progress; one request runs at a time.

The agent only has that compile check as a tool: it can't read files, run
commands or run your code. It also ignores kit's own setup, like `.kit.yml`,
`AGENTS.md`, skills and MCP servers, so opening a notebook in a project that
configures kit doesn't change what it can do. To build gopyter without AI
support, and without the kit dependency, see Development.

## Headless

```sh
gopyter run notes.ipynb            # run every code cell, print the outputs
gopyter run notes.ipynb --save     # …and write the outputs into the notebook
gopyter run notes.ipynb --fail-fast
```

It exits non-zero if any cell fails, so it works as a CI check for notebooks.
On a terminal, outputs that update (`nb.DisplayID`, `gonbui.UpdateHTML`, DOM
changes) are redrawn in place. When the output isn't a terminal, they're
printed once, in their final state, when the cell ends, so logs don't get a
line per animation frame.

## Editing outside gopyter

gopyter checks the notebook file once a second. If another program changes it,
for example an editor or `gopyter run --save`, gopyter reloads it and keeps
your place. If you have unsaved changes, or the cell you're editing changed,
it asks first. **Reload** loads the file and discards your changes. **Keep
mine** (or `esc`) keeps your version, and the next save overwrites the file.
Reloading doesn't reset the kernel, so declarations from cells you already ran
stay in effect. External edits to cell text can be undone.

## Acknowledgements

gopyter owes a lot to [GoNB](https://github.com/janpfeifer/gonb), the Go
kernel for Jupyter by [Jan Pfeifer](https://github.com/janpfeifer):

- **How cells run.** Like GoNB, gopyter keeps each cell's declarations, writes a
  Go program with a `main()` for the statements, builds it with `go build` and
  runs it. Compile errors are mapped back to cell lines.
- **Notebook format.** Notebooks use GoNB's kernelspec, so the same files open
  in Jupyter with GoNB.
- **Cell commands.** `!`, `%%`/`%main`, `%args`, `%env`, `%exec`, `%test`,
  `%goflags`, `%capture`, `%autoget`, `%ls`/`%rm`, `%reset`, the cell magics
  (`%%writefile`, `%%script`...) and `//gonb:` prefixes follow GoNB's special
  commands, as do `init_xxx()` functions.
- **`gonbui`, `widgets`, `comms` and `dom`.** gopyter's versions of these
  packages keep GoNB's API and adapt its documentation, and parts of its code
  (such as `comms.ConvertTo`), so GoNB notebooks run unchanged. They're in
  [`internal/kernel/runtime/gonb`](internal/kernel/runtime/gonb), with GoNB's
  MIT [license](internal/kernel/runtime/gonb/LICENSE).

gopyter's own parts (the terminal UI, drawing images, HTML and widgets as text,
the `nb` package, variables kept across cells, completion, AI) are separate
work. To run Go notebooks in Jupyter itself, use GoNB.

## Development

```sh
go test -race ./...
golangci-lint run ./...
go run . examples/tour.ipynb
go build -tags noai .      # without AI support: no kit dependency, a much smaller binary
```

See [AGENTS.md](AGENTS.md) for the architecture, conventions and testing
workflow. Releases are cut by pushing a `v*` tag; GoReleaser builds and
publishes the binaries.

## License

gopyter is released under the [MIT License](LICENSE). The GoNB compatibility
packages in [`internal/kernel/runtime/gonb`](internal/kernel/runtime/gonb) are
adapted from [GoNB](https://github.com/janpfeifer/gonb), also under the MIT
License: see [its license](internal/kernel/runtime/gonb/LICENSE), which release
archives include as `third_party/gonb/LICENSE`.
