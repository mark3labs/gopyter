# ◆ gopyter

A Jupyter-style notebook for **Go** that runs in your terminal.

```
 ◆ gopyter   hello.ipynb ●                                        gopls  │  go1.27.1  │  ● idle
── ▶ run ─ ▶▶ run all ─ ■ stop ─ ↻ restart ───┼─ + code ─ + markdown ───┼─ save ─ ? help ───────
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
- **Keyboard and mouse.** Jupyter's modal keys, plus clickable toolbar, cell actions, context menus and text selection
- **Standard `.ipynb` files** with a [GoNB](https://github.com/janpfeifer/gonb) kernelspec, so notebooks also open in Jupyter
- **Headless runs** for scripts and CI: `gopyter run notes.ipynb --save`

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
cells are compiled with it. For completion, install
`go install golang.org/x/tools/gopls@latest` (without it, gopyter falls back to
basic completion).

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

For a guided tour: `gopyter examples/tour.ipynb`, then press `A` to run all cells.

## How cells run

- Top-level `func`, `type`, `var`, `const` and `import` declarations **persist**.
  Re-running a cell replaces what it declared. Everything else runs inside a
  generated `main()` in a fresh process, so local variables don't carry over;
  keep shared state in package-level `var`s.
- Imports are added automatically, and third-party modules are fetched on first use.
- `Display(v...)` and `DisplayMarkdown(s)` produce rich output.

| Cell command     | Effect                                        |
|------------------|-----------------------------------------------|
| `!cmd`           | run a shell command (e.g. `!go get pkg@v1`)    |
| `%reset`         | forget all declarations                       |
| `%env K=V`       | set environment variables for your programs   |
| `%args a b`      | set program arguments                         |
| `%ls`            | list persisted declarations                   |
| `%workspace`     | print the kernel workspace directory          |
| `%help`          | show this list                                |

## Keys

gopyter is modal like Jupyter: **command mode** (blue) works on cells, **edit
mode** (green) edits text. Press `?` for the full list.

| Key                          | Action                                   |
|------------------------------|------------------------------------------|
| `enter` / `esc`              | edit mode / command mode                 |
| `ctrl+r` or `shift+enter`    | run cell and advance                     |
| `ctrl+j` or `ctrl+enter`     | run cell                                 |
| `A` / `R` / `ctrl+c`         | run all / restart kernel / interrupt     |
| `j` `k` `g` `G`              | move selection                           |
| `a` / `b`                    | insert cell above / below                |
| `dd` / `z`                   | delete / undo delete                     |
| `x` `c` `v`, `K` / `J`       | cut / copy / paste, move cell up / down  |
| `m` / `y` (`ctrl+t` editing) | convert to markdown / code               |
| `o` / `O`                    | fold long output / clear output          |
| `ctrl+s` / `q`               | save / quit                              |

In edit mode: `tab` or `ctrl+space` completes, `shift`+arrows select, `ctrl+c`/`ctrl+x`
copy/cut, `ctrl+z`/`ctrl+y` undo/redo, and `↑`/`↓` flow between cells.
`shift+enter` and `ctrl+enter` need a terminal with the kitty keyboard protocol
(kitty, Ghostty, WezTerm, foot…); `ctrl+r` and `ctrl+j` work everywhere.

## Mouse

Click a cell to edit it, and drag, double-click or triple-click to select text.
Hover `[n]` and click `[▶]` to run a cell; the buttons on a cell's border run,
convert, move, duplicate or delete it. Right-click opens a context menu, and the
toolbar under the title covers running, adding cells, saving and help. Hovering
any button shows what it does and its shortcut. Hold `shift` while dragging to
use your terminal's own selection instead (`alt`/`option` in some terminals).

## Headless

```sh
gopyter run notes.ipynb            # run every code cell, print the outputs
gopyter run notes.ipynb --save     # …and write the outputs into the notebook
gopyter run notes.ipynb --fail-fast
```

It exits non-zero if any cell fails, so it works as a CI check for notebooks.

## Development

```sh
go test -race ./...
golangci-lint run ./...
go run . examples/tour.ipynb
```

See [AGENTS.md](AGENTS.md) for the architecture, conventions and testing
workflow. Releases are cut by pushing a `v*` tag; GoReleaser builds and
publishes the binaries.
