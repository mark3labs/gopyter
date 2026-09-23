---
description: Visually verify the TUI in tmux (keys, mouse, overlays) for a change
---

Build gopyter and check the terminal UI in a real tmux session, then report exactly what is on screen. Scenario or change to verify (optional): $@

Use this after any change to `internal/ui` (rendering, keys, mouse, dialogs, completion), and whenever unit tests can't show what the user will actually see.

## Steps

1. **Build to /tmp** (never into the repo):

       go build -o /tmp/gopyter .

2. **Start a detached session** at a fixed size. Use a scratch notebook, or `examples/tour.ipynb` when you need content (don't save changes to it):

       tmux kill-session -t gp 2>/dev/null
       tmux new-session -d -s gp -x 100 -y 30 "/tmp/gopyter /tmp/tui-check.ipynb"
       sleep 1

   Also check a narrow size (for example `-x 64 -y 20`) when layout, the toolbar, dialogs or popups changed

3. **Drive the UI**:
   - Literal text: `tmux send-keys -t gp -l 'fmt.Println("hi")'`
   - Named keys: `tmux send-keys -t gp Escape`, `Enter`, `Tab`, `BTab` (shift+tab), `Up`, `C-r` (run & advance), `C-j` (run), `C-c`
   - tmux **cannot** send `shift+enter` or `ctrl+enter`; use `C-r` / `C-j`
   - Send `Escape` on its own and wait about 0.2s before the next key; otherwise the next key is read as an alt+key combination
   - Mouse, as SGR sequences. Coordinates are **1-based** (0-based screen x/y + 1); `M` is press and `m` is release:

         # left click at column X, row Y (0-based) → \e[<0;X+1;Y+1M then \e[<0;X+1;Y+1m
         tmux send-keys -t gp -l $'\e[<0;12;4M'$'\e[<0;12;4m'
         tmux send-keys -t gp -l $'\e[<35;12;4M'   # hover (motion)
         tmux send-keys -t gp -l $'\e[<2;12;4M'    # right click
         tmux send-keys -t gp -l $'\e[<65;12;4M'   # wheel down (64 = up)
         tmux send-keys -t gp -l $'\e[<32;20;6M'   # drag (motion with the left button held)

   - To target an element, find its coordinates from a capture instead of guessing, e.g. a Python one-liner over `tmux capture-pane -t gp -p` that prints the line number and `.index('Cancel')`
   - `sleep` after actions: about 0.3s for UI updates, 1–3s for cell execution, about 1.5s for gopls completion (longer on the first request)

4. **Capture and inspect**:
   - Text: `tmux capture-pane -t gp -p` (use `sed -n 'A,Bp'` to show the relevant rows)
   - Styles and colors: `tmux capture-pane -t gp -p -e`. Look for `48;5;N` background codes to tell focused, hovered and selected states apart
   - Check:
     - alignment of borders and scrollbar
     - the footer mode pill (COMMAND / EDIT) and hints or tooltips
     - overlays centered and not clipped
     - the popup not covering the cursor line
     - nothing left on screen from a previous frame

5. **Clean up**: `tmux kill-session -t gp` and remove scratch files (`/tmp/tui-check.ipynb`). If the app was told to quit, confirm it exited (`tmux has-session -t gp` fails) and that no `gopls` processes were left behind (`pgrep -a gopls`)

6. **Report**: paste the relevant captured screen regions for each step, state pass or fail against the expected behavior, and describe any visual defect precisely (row, column, what's wrong)

## Guidelines

- Always capture before claiming something works. Screenshots in text form are the evidence
- Test both the keyboard and the mouse path for interactive features; they should behave the same
- If a capture looks wrong, first rule out an off-by-one in your own mouse coordinates before concluding the app is broken
