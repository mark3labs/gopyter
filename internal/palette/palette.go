// Package palette defines gopyter's default colors, shared by the UI and runner.
package palette

// Colors holds hex colors and the syntax highlighting theme for a variant.
type Colors struct {
	Primary, Info, Accent, Success, Warning, Error string
	ErrorBar, Orange, Selection, Raised            string
	Text, Dim, Muted, Subtle, Faint, Ink           string
	Syntax                                         string
}

// Dark is gopyter's default dark palette.
var Dark = Colors{
	Primary: "#00ADD8", Info: "#5DC9E2", Accent: "#8B5CF6",
	Success: "#4ADE80", Warning: "#FDDD00", Error: "#F87171",
	ErrorBar: "#CE3262", Orange: "#FB923C",
	Selection: "#1F4E6B", Raised: "#52525B",
	Text: "#E4E4E7", Dim: "#A1A1AA", Muted: "#71717A",
	Subtle: "#3F3F46", Faint: "#27272A", Ink: "#101014",
	Syntax: "catppuccin-mocha",
}

// Light is gopyter's default light palette.
var Light = Colors{
	Primary: "#007D9C", Info: "#0E7490", Accent: "#7C3AED",
	Success: "#16A34A", Warning: "#B45309", Error: "#DC2626",
	ErrorBar: "#CE3262", Orange: "#EA580C",
	Selection: "#BAE6FD", Raised: "#A1A1AA",
	Text: "#18181B", Dim: "#52525B", Muted: "#71717A",
	Subtle: "#C4C4CC", Faint: "#E4E4E7", Ink: "#FAFAFA",
	Syntax: "catppuccin-latte",
}
