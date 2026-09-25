# Demo of gopyter. Render with: vhs assets/demo.tape
Output assets/demo.gif

Require gopyter
Require go

Set Shell "bash"
Set FontSize 16
Set Width 1300
Set Height 860
Set Padding 16
Set TypingSpeed 35ms
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful

# Start in a scratch directory with a fresh config (default theme, no vim, no AI).
Hide
Type "rm -rf /tmp/gpdemo && mkdir -p /tmp/gpdemo/cfg && cd /tmp/gpdemo && export XDG_CONFIG_HOME=/tmp/gpdemo/cfg PS1='$ ' && clear"
Enter
Show

Type "gopyter demo.ipynb"
Sleep 400ms
Enter
Sleep 2s

# A markdown title.
Ctrl+T
Type "# Go, cell by cell ◆"
Sleep 400ms
Ctrl+R
Sleep 1s

# Declarations persist across cells.
Type "type Point struct{ X, Y float64 }"
Enter
Enter
Type "func (p Point) Dist() float64 { return math.Hypot(p.X, p.Y) }"
Sleep 500ms
Ctrl+R
Sleep 2s

# The last expression is displayed.
Type "p := Point{3, 4}"
Enter
Type "p.Di"
Sleep 1.5s
Tab
Sleep 500ms
Ctrl+R
Sleep 2.5s

# Output streams while the cell runs.
Type "for i := range 5 {"
Enter
Type 'fmt.Printf("step %d %s\n", i+1, strings.Repeat("■", i+1))'
Enter
Type "time.Sleep(300 * time.Millisecond)"
Enter
Type "}"
Sleep 400ms
Ctrl+R
Sleep 3s

# Images are drawn right in the output.
Type "img := image.NewRGBA(image.Rect(0, 0, 96, 64))"
Enter
Type "for py := range 64 {"
Enter
Type "for px := range 96 {"
Enter
Type "c := complex(float64(px)/32-2.2, float64(py)/32-1)"
Enter
Type "z, n := c, 0"
Enter
Type "for n < 50 && cmplx.Abs(z) < 2 {"
Enter
Type "z, n = z*z+c, n+1"
Enter
Type "}"
Enter
Type "t, s := float64(n)/50, 1-float64(n)/50"
Enter
Type "img.Set(px, py, color.RGBA{uint8(2295 * s * t * t * t), uint8(3825 * s * s * t * t), uint8(2168 * s * s * s * t), 255})"
Enter
Type "}"
Enter
Type "}"
Enter
Type "img"
Sleep 500ms
Ctrl+R
Sleep 5s

# Preview a couple of themes.
Escape
# Pause so esc+T is not read as alt+T.
Sleep 800ms
Type "T"
Sleep 1s
Down
Sleep 1s
Down
Sleep 1s
Down
Sleep 1.5s
Escape
Sleep 1s
