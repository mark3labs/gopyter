# ◆ gopyter

A Jupyter-style notebook for **Go** that runs in your terminal.

gopyter builds on the work of [GoNB](https://github.com/janpfeifer/gonb), the Go
kernel for Jupyter by Jan Pfeifer: it runs cells the same way, shares its
notebook format and cell commands, and implements its `gonbui` API and widgets,
so GoNB notebooks run in gopyter too. See [Acknowledgements](#acknowledgements).

![gopyter demo](assets/demo.gif)

- **Real Go, cell by cell.** Declarations persist across cells, the last expression is displayed, and compile errors point at cell lines
- **IDE-style completion** from [gopls](https://go.dev/gopls), aware of everything earlier cells declared
- **Live diagnostics.** With gopls, errors and warnings are underlined as you type; the message under the cursor appears in the footer
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

With [Nix](https://nixos.org/download), the flake builds the **latest release**
from source and installs it into your profile:

```sh
nix profile install github:mark3labs/gopyter
```

The pin to the newest tag moves automatically after every release; a flake input
cannot say "latest tag", so `flake.nix` names one and the release workflow bumps
it. Intel Macs are served by the two commands above, since nixpkgs no longer
supports them.

**Requirements:** the [Go toolchain](https://go.dev/dl/) on your `PATH`, since
cells are compiled with it. For completion, symbol info and live diagnostics, install
`go install golang.org/x/tools/gopls@latest` (without it, gopyter falls back to
basic completion). The Go toolchain is *not* part of the Nix closure: cells are
compiled with whatever `go` is on your `PATH`, so install Go from Nix too if the
profile is all you have.

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
  Until a cell importing a third-party module has been built, live diagnostics
  may report missing imports or undefined package names. Diagnostics also omit
  parse errors located in generated wrapper code (such as an unclosed block at EOF).

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
  the Kitty graphics protocol on kitty and Ghostty, or colored half blocks
  (two pixels per character) elsewhere, scaled to fit the cell, and
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
| `!cmd`           | run a shell command in the kernel workspace (e.g. `!go get pkg@v1`); a trailing `\` continues it; it runs in a terminal, so its output keeps colors, progress bars and spinners (see below) |
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

### Shell commands in a terminal

Shell commands (`!cmd`, `%%bash`, `%%script`) run in a pseudo-terminal as wide
as the cell's output, so they see the terminal a command expects: they colorize
their output and draw progress bars and spinners, and gopyter replays that while
the command runs. crush's bang mode
([same idea](https://github.com/charmbracelet/crush)) asks for color with the
environment alone; a real terminal also gives the cursor movement and erasing
that redraws need. The terminal's own line endings don't reach the output, and
what a command writes to stderr comes out on the same stream as its stdout, as
in a terminal. gopyter only fills in what the environment is missing, so `TERM`,
`COLORTERM` or `FORCE_COLOR` that you set are kept, and `NO_COLOR` turns the
color off.

This is for the terminal UI. `gopyter run` writes to a file or a pipe, so its
shell commands keep plain pipes and no escape codes reach its output.

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
mode** (green) edits text. Press `?` for the full list, or `ctrl+p` (or `:`)
to search and run any command.

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
| `dd` / `z`                   | delete / undo delete, merge or split     |
| `alt+↑` / `alt+↓`, `shift+click` | extend cell selection (command mode) |
| `alt+a`                      | select all cells (command mode)          |
| `alt+m`                      | merge selection, or merge with next      |
| `alt+-`                      | split at cursor (edit mode, including vim) |
| `x` `c` `v`, `K` / `J`       | cut / copy / paste, move cell up / down  |
| `m` / `y` / `r`             | convert to markdown / code / raw (`ctrl+t` toggles code/markdown while editing) |
| `I`                          | hide/show code or raw input (outputs stay visible) |
| `<` / `>`                    | scroll text output left/right; first `>` stops wrapping, `<` at column zero restores it |
| `E`                          | edit the selected cell in `$VISUAL` / `$EDITOR` (fallback: `vi`) |
| `C`                          | copy the cell's output to the clipboard |
| `alt+r`                      | restart kernel and clear idle cells' outputs (both modes) |
| `alt+s`                      | save as a new path (both modes) |
| `alt+shift+f`                | format the selected Go cell with gofmt    |
| `o` / `O`                    | fold long output / clear output          |
| `D`                          | clear every cell's output                |
| `ctrl+b`                     | sidebar: contents and variables (`tab` switches) |
| `ctrl+s` / `q`               | save / quit                              |
| `T`                          | pick a color theme                       |
| `ctrl+p` or `:`               | command palette: type to find a command, enter runs it |
| `V`                          | turn vim bindings on / off (saved)       |
| `M`                          | pick the AI model, or turn AI on / off   |
| `e`                          | ask AI to write or change a cell (when AI is on) |
| `f`                          | fix a failing cell with AI (when AI is on) |

The sidebar starts hidden. **Contents** lists markdown headings (levels 1–3);
**Variables** shows persisted declarations with types and one-line value previews
from the last saving run. `tab` switches panels, arrows move, `enter` jumps to
the declaring cell, and `esc` returns focus to the notebook. It automatically
hides in narrow terminals and returns when there is room again.

Run, delete, cut/copy/paste, move, convert and clear-output commands apply to
every selected cell. Other cell commands use the cursor cell. `esc`, navigation,
plain click and entering edit mode collapse the range. Paste selects the pasted
cells. Merge keeps the first cell's type and metadata, comments prose into Go
and fences Go into markdown; running or queued cells cannot be merged or split.
Split keeps outputs on the original cell. Cell-level `z` retains the last 100
delete/merge/split operations, separate from editor undo; undoing a split after
editing either half reports `can't undo split: cell changed` rather than losing edits.

Some terminals intercept `alt+arrow`; `shift+click` is the selection fallback.
In macOS Terminal, enable **Use Option as Meta** (otherwise Option word motions
or `alt+-` typing `–` may be intercepted).

Format a code cell with `alt+shift+f` (in command or edit mode), or the `fmt`
button at the bottom right of the selected/hovered code cell. Formatting uses
Go's gofmt formatter, preserves notebook commands and source order, and is one
undo step; it does not execute code. Non-Go cell-magics such as `%%bash` and
`%%writefile` are left untouched. `gopyter format file.ipynb` formats all code
cells in place, preserving metadata and outputs; if any cell has invalid Go
syntax, the file is left unchanged. Markdown and raw cells are never formatted.

In edit mode: `tab` or `ctrl+space` completes, `shift`+arrows select, `ctrl+c`/`ctrl+x`
copy/cut, `ctrl+z`/`ctrl+y` undo/redo, and `↑`/`↓` flow between cells.
`alt+k` (or `F1`) shows the signature and documentation of the symbol under the
cursor, or of the enclosing call when the cursor is among its arguments;
`pgup`/`pgdn` scroll it and any other key dismisses it.
`shift+enter` and `ctrl+enter` need a terminal with the kitty keyboard protocol
(kitty, Ghostty, WezTerm, foot…); `ctrl+r` and `ctrl+j` work everywhere.

### Custom keys

The table above lists defaults. Run `gopyter keys` to list all command names,
current keys, defaults and descriptions (`*` marks configured commands).
Add a `keys` object to [config.json](#themes):

```json
{
  "theme": "nord",
  "keys": {
    "run_all": ["A", "ctrl+a"],
    "quit": [],
    "move_up": ["alt+k"]
  }
}
```

Each list **replaces** that command's defaults; `[]` or `null` unbinds it.
Omitted commands keep their defaults. `escape` cannot be left unbound.
Keys use Bubble Tea names such as `ctrl+s`, `alt+enter`, `shift+up`, `A`, `?`,
`f1`, `pgdown` and `space`. Modifiers are normalized to ctrl, alt, shift, meta,
hyper, super order. Use `A`, not `shift+a`; with modifiers use
`ctrl+shift+a`, not `ctrl+A`. Delete still requires pressing its key twice.

Unknown commands, malformed keys and conflicts produce warnings on stderr and
in the startup status bar; the rest of the settings still apply. A key cannot
belong to two commands in the same mode; global bindings conflict with both
command and edit modes. The overridden command loses the conflicting key; if
both were overridden, the later command in `gopyter keys` loses it. Command
and edit bindings do not conflict with each other. Help, hints and tooltips
reflect the configured keys, and mouse actions work even when unbound.

User bindings take priority over fixed editor keys (for example rebinding
`undo` to `ctrl+u` replaces kill-to-start); these shadows are not warned about.
Vim, dialogs, menus, pickers, input lines, widgets and `alt+shift+f` are not
configurable here. There are no per-notebook or command-line key overrides.

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

Raw cells are plain text and never run. Hiding input is saved as Jupyter's
`metadata.jupyter.source_hidden`; entering edit mode shows it again. Duplicated
cells start with visible input. Sideways scrolling affects stdout, stderr and
text results only, and is not saved. Programs that wrap their own terminal
output keep doing so. Restart-and-clear interrupts a running cell, but leaves
its output until it finishes, just like clearing outputs with `D`. Save-as
moves file watching to the new path, but does not move the kernel's run directory.

## Mouse

Click the sidebar's panel headers to switch panels, or an entry to jump to its
cell. The wheel over the sidebar scrolls its list independently of the notebook.

Click a cell to edit it, and drag, double-click or triple-click to select text.
Click a hidden-input placeholder to show the source. Under unwrapped text output,
click `◂` / `▸` to scroll or **wrap** to restore wrapping.
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
{ "theme": "catppuccin", "keys": { "run_all": ["ctrl+a"], "quit": [] } }
```

```sh
gopyter themes                     # list themes, marking the active one
gopyter --theme dracula notes.ipynb  # use a theme for this session only
gopyter --syntax-theme monokai     # override the code highlighting (any chroma style)
```

## Images

`--graphics=auto` (the default) detects kitty and Ghostty from the environment,
without terminal queries. `--graphics=kitty` forces Unicode-placeholder images;
`--graphics=blocks` always uses portable half blocks. Both the TUI and
`gopyter run` accept the flag. The saved default can be set in the settings file:

```json
{ "graphics": "blocks" }
```

Kitty graphics needs at least 256 colors. In tmux (3.3+), enable passthrough
with `set -g allow-passthrough on`; otherwise auto falls back to blocks and a
forced kitty request warns. WezTerm is not auto-detected because its Kitty
support does not yet include Unicode placeholders. Images scroll and clip like
text, and menus cover them normally (images themselves are not dimmed).

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
gopyter format notes.ipynb         # format code cells in place, without running
```

It exits non-zero if any cell fails, so it works as a CI check for notebooks.
On kitty/Ghostty, images use Kitty graphics, with the same `--graphics` override
as the TUI. Graphics commands are never written to redirected output, even when
colors are forced; it keeps half blocks or `[image WxH]` descriptions.
On a terminal, outputs that update (`nb.DisplayID`, `gonbui.UpdateHTML`, DOM
changes) are redrawn in place. When the output isn't a terminal, they're
printed once, in their final state, when the cell ends, so logs don't get a
line per animation frame.

## Serving over SSH

`gopyter serve` runs a separate notebook TUI, temporary kernel workspace and
completion engine for every SSH session, inspired by
[euporie-hub](https://github.com/joouha/euporie).

```sh
gopyter serve ~/notes --authorized-keys ~/.ssh/authorized_keys
ssh -t -p 2222 localhost analysis.ipynb   # open or create ~/notes/analysis.ipynb
ssh -t -p 2222 localhost sub/plots.ipynb  # paths are relative to ~/notes
ssh -t -p 2222 localhost                 # new unnamed notebook
```

The directory defaults to `.`. A PTY is required (`ssh -t`). Commands must be
one relative `.ipynb` path; command paths and save-as names cannot escape the
directory, including through symlinks. Missing notebooks are created on save.
Disconnecting cancels running cells and AI requests and closes that session's
completion engine and kernel.

| Flag | Default / purpose |
|---|---|
| `--authorized-keys FILE` | **Required** OpenSSH public-key allowlist; unreadable, empty or malformed files prevent startup |
| `--listen ADDRESS` | `127.0.0.1:2222`; choose a non-loopback address deliberately to expose it to the network |
| `--host-key FILE` | `gopyter/ssh_host_ed25519` in the user config directory; a missing ed25519 key is generated with mode 0600 |
| `--theme NAME`, `--light` | saved theme, dark by default; one shared theme for all sessions, with the theme picker disabled |
| `--vim`, `--no-complete`, `--model` | same per-session defaults as the local TUI; `--no-complete` avoids per-session gopls |

`--workdir` is not supported: each session needs its own workspace. The startup
log prints the address, authorized_keys path and host key's SHA256 fingerprint.
**Compare that fingerprint with SSH's first-connect prompt** before accepting
it (or inspect the public key with `ssh-keygen -lf HOST_KEY.pub`). Password and
keyboard-interactive authentication are never offered; there is no anonymous
mode.

**Security:** Everyone whose key is in `--authorized-keys` can run any code as
the user running `gopyter serve`: use it for yourself or a team that already
shares that account. This is **not a multi-tenant notebook hub**. Cells and
shell commands have that user's files, credentials, network and environment,
including API keys; remote AI uses the server's AI configuration and credentials.
Path confinement prevents mistakes, **not a sandbox**: a cell can still write
anywhere that OS user can. Treat giving someone access exactly like giving
them a shell. Each session also costs a workspace, gopls process (unless
disabled), and Go builds; session limits are not provided.

Sessions opening the same notebook keep separate in-memory copies and kernels.
A notice warns when another session already has the file open. Saves use atomic
replacement, so files are not torn, but **the last save wins**. Clean sessions
reload within a second; dirty sessions show **Reload / Keep mine**, as described
in [Editing outside gopyter](#editing-outside-gopyter).

## Editing outside gopyter

Press `E` in command mode to edit a cell in `$VISUAL`, then `$EDITOR`, falling
back to `vi`. Space-separated arguments such as `code --wait` are supported.
Saving and quitting applies the source as one undo step without running it.
Jupyter's `source_hidden` metadata is kept when saving and reloading notebooks.

gopyter checks the notebook file once a second. If another program changes it,
for example an editor or `gopyter run --save`, gopyter reloads it and keeps
your place. If you have unsaved changes, or the cell you're editing changed,
it asks first. **Reload** loads the file and discards your changes. **Keep
mine** (or `esc`) keeps your version, and the next save overwrites the file.
Reloading doesn't reset the kernel, so declarations from cells you already ran
stay in effect. External edits to cell text can be undone.

## Acknowledgements

The Contents and Variables sidebar is inspired by
[euporie](https://github.com/joouha/euporie) (behavior, not code).

Multi-cell selection, merge and cursor split follow
[euporie](https://github.com/joouha/euporie)'s notebook operations.

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

The Kitty Unicode-placeholder image approach is borrowed from
[euporie](https://github.com/joouha/euporie): transmit once, place virtually,
and let ordinary text cells handle scrolling, clipping and compositing.

The SSH notebook server model is borrowed from
[euporie-hub in euporie](https://github.com/joouha/euporie).

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
