# ◆ gopyter

A Jupyter-style notebook for **Go** that runs entirely in your terminal.

Built with [Bubble Tea v2](https://charm.land/bubbletea), [Lip Gloss v2](https://charm.land/lipgloss),
[Bubbles v2](https://charm.land/bubbles), [Ultraviolet](https://github.com/charmbracelet/ultraviolet),
[Glamour v2](https://charm.land/glamour) and [Fang](https://charm.land/fang). Execution
semantics are inspired by [GoNB](https://github.com/janpfeifer/gonb), and notebooks are
saved as standard `.ipynb` files with a GoNB kernelspec so they open in Jupyter too.

```
 ◆ gopyter   tour.ipynb ●                                   go1.27.1  │  ● idle
─────────────────────────────────────────────────────────────────────────────────
▌        ╭─ go ──────────────────────────────────────────────────── ✓ 154ms ─╮
▌    [2] │ 1  p := Point{3, 4}                                                │
▌        │ 2  fmt.Println("distance:", p.Dist())                              │
▌        │ 3  p                                                               │
▌        ╰────────────────────────────────────────────────────────────────────╯
▌          distance: 5
▌ Out[2]   {X:3 Y:4}
 COMMAND   ↵ edit · ⇧↵/^r run & next · b insert below · dd delete · ? help
```

## Install

```sh
go install github.com/mark3labs/gopyter@latest
```

Requires the Go toolchain in `PATH` (it's used to compile the cells).

## Usage

```sh
gopyter                         # new, untitled notebook
gopyter notes.ipynb             # open or create a notebook
gopyter run notes.ipynb --save  # execute headlessly and store outputs
gopyter --help
```

Try the tour: `gopyter examples/tour.ipynb`, then press `A` to run everything.

## How cells run

* Top-level `func`, `type`, `var`, `const` and `import` declarations **persist** across
  cells. Re-running a cell replaces the declarations it previously contributed;
  redeclaring a name in another cell replaces the old one.
* Everything else runs inside a generated `func main()`. Each execution is a fresh
  process, so use package-level `var`s for values you want to reuse (they are
  re-initialised on each run, just like in GoNB).
* A trailing expression is displayed as the cell result (`Out[n]`), like Jupyter.
* Imports are managed for you (goimports); third-party modules are fetched
  automatically. You can also run `!go get github.com/foo/bar`.
* `Display(v...)` and `DisplayMarkdown(s)` are available for rich output.
* Compile errors point back to cell lines, e.g. `In[3]:2:5: undefined: x`.
* A cell may define its own `func main()`.

### Cell commands

| Command          | Description                                        |
|------------------|----------------------------------------------------|
| `!cmd`           | run a shell command in the kernel workspace        |
| `%reset`         | forget all declarations                            |
| `%env KEY=VALUE` | set environment variables for executed programs    |
| `%args a b c`    | set program arguments                              |
| `%ls`            | list persisted declarations                        |
| `%workspace`     | print the kernel workspace directory               |
| `%help`          | show help                                          |

## Keys

gopyter is modal, like Jupyter. **Command mode** (blue) navigates and manipulates
cells; **edit mode** (green) edits the selected cell. Press `?` for the full list.

| Key                          | Action                                   |
|------------------------------|------------------------------------------|
| `enter` / `esc`              | edit mode / command mode                 |
| `shift+enter` or `ctrl+r`    | run cell and advance                     |
| `ctrl+enter` or `ctrl+j`     | run cell                                 |
| `alt+enter`                  | run cell and insert below                |
| `A` / `R` / `ctrl+c`         | run all / restart kernel / interrupt     |
| `j` `k` `g` `G`              | move selection                           |
| `a` / `b`                    | insert cell above / below                |
| `dd` / `z`                   | delete / undo delete                     |
| `x` `c` `v`                  | cut / copy / paste cell                  |
| `K` / `J`                    | move cell up / down                      |
| `m` / `y` (`ctrl+t` editing) | convert to markdown / code               |
| `o` / `O`                    | fold long output / clear output          |
| `ctrl+s` / `q`               | save / quit                              |

In edit mode you get auto-indentation, smart closing braces, undo/redo
(`ctrl+z`/`ctrl+y`), word motions (`alt+←/→`), emacs-style `ctrl+a/e/k/u/w`, and
`↑`/`↓` flow between cells. `shift`+motion selects text, `ctrl+c`/`ctrl+x` copy/cut
the selection (to the system clipboard via OSC 52), `alt+a` selects all, and
`tab`/`shift+tab` indent/dedent a multi-line selection.

## Code completion

gopyter uses [gopls](https://go.dev/gopls) for IDE-style completion, with full
knowledge of what previous cells declared:

* Suggestions pop up as you type identifiers and after `.`, with type signatures
  and a documentation panel. Nothing pops up inside strings or comments.
* `↑`/`↓` (or `ctrl+p`/`ctrl+n`, mouse wheel) select, `tab`/`enter` (or a click)
  accept, `esc` dismisses. Functions get `()` with the cursor placed inside.
* `tab` after an identifier or `.` (or `ctrl+space` anywhere) asks explicitly; a
  single match is inserted right away. `tab` elsewhere still indents.
* Standard library packages complete without being imported first.

Install gopls with `go install golang.org/x/tools/gopls@latest`. Without it, a basic
completer suggests keywords, builtins and identifiers from the notebook (the header
shows which is active). Disable completion with `--no-complete`.

## Mouse

| Action                                  | Effect                                           |
|-----------------------------------------|--------------------------------------------------|
| click code                              | select cell, enter edit mode, place cursor       |
| drag / shift+click                      | select text (auto-scrolls past the edges)        |
| double / triple click                   | select word / line                               |
| click line numbers                      | select line                                      |
| double click rendered markdown          | edit it                                          |
| hover `[n]` label, click `[▶]`           | run the cell (`[■]` stops a running cell)       |
| `▶ ⇄ ↑ ↓ ⧉ ✕` on a cell's border        | run · convert · move up/down · duplicate · delete |
| `go` / `markdown` label on a cell       | convert between code and markdown                |
| right click                             | context menu (cell and text actions)             |
| toolbar under the title                 | run, run all, stop, restart, add cells, save, help |
| `⋯ N earlier lines hidden`              | expand / collapse long output                    |
| `+ code` / `+ markdown` at the end      | append a cell                                    |
| `COMMAND` / `EDIT` pill                 | toggle mode                                      |
| wheel / scrollbar click & drag          | scroll                                           |

Dialogs are fully keyboard driven too: `tab`/`shift+tab` (or `←`/`→`) cycle
through the buttons and the filename field, `enter`/`space` press the focused
button, and `esc` cancels.

Hovering any button shows what it does (and its keyboard shortcut) in the footer.
To select text the terminal way instead, hold `shift` (or `alt`/`option`, depending
on your terminal) while dragging.

`shift+enter` and `ctrl+enter` need a terminal that supports the kitty keyboard
protocol (kitty, Ghostty, WezTerm, foot, recent iTerm2…); `ctrl+r`/`ctrl+j` work
everywhere.

## Layout

```
main.go                  CLI (cobra + fang)
internal/kernel          cell parsing, declaration tracking, build & run
internal/complete        code completion (gopls, basic fallback)
internal/lsp             minimal LSP client
internal/notebook        .ipynb reading/writing
internal/runner          headless execution (`gopyter run`)
internal/ui              Bubble Tea app: editor, cells, overlays
```
