package kernel

import "github.com/mark3labs/gopyter/internal/notebook"

// OutputKind returns the notebook kind for an output event. Operations and
// unknown event kinds are not notebook outputs.
func (k EventKind) OutputKind() (notebook.OutputKind, bool) {
	switch k {
	case Stdout:
		return notebook.Stdout, true
	case Stderr:
		return notebook.Stderr, true
	case Result:
		return notebook.Result, true
	case Markdown:
		return notebook.MarkdownOut, true
	case Error:
		return notebook.Error, true
	case Info:
		return notebook.Info, true
	case Image:
		return notebook.ImageOut, true
	case HTML:
		return notebook.HTMLOut, true
	default:
		return "", false
	}
}
