package complete

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/mark3labs/gopyter/internal/kernel"
)

// Severity is an LSP diagnostic severity.
type Severity int

const (
	SeverityError Severity = 1 + iota
	SeverityWarning
	SeverityInfo
	SeverityHint
)

// Diagnostic describes a cell problem using 0-based rows and rune columns.
// End is exclusive.
type Diagnostic struct {
	Row, Col, EndRow, EndCol int
	Severity                 Severity
	Message, Source          string
}

type lspDiagnostic struct {
	Range           struct{ Start, End position }
	Severity        Severity
	Message, Source string
}

type diagnosticPublication struct {
	URI         string          `json:"uri"`
	Version     int             `json:"version"`
	Diagnostics []lspDiagnostic `json:"diagnostics"`
}

type mirror struct {
	cellID, src string
	cs          kernel.CompletionSource
	sent        int // Engine.sent once this version was sent
}

type progressParams struct {
	Token json.RawMessage `json:"token"`
	Value struct {
		Kind, Title string
	} `json:"value"`
}

var errStale = errors.New("diagnostics: superseded mirror")

func (e *Engine) notifyDiagnostics(method string, params json.RawMessage) {
	if method == "$/progress" {
		e.notifyProgress(params)
		return
	}
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p diagnosticPublication
	if json.Unmarshal(params, &p) != nil {
		return
	}
	// The URI is fixed before starting the server; derive it without taking
	// mu, which Close holds while waiting for the read loop to finish.
	if p.URI != fileURI(e.file) {
		return
	}
	e.diagMu.Lock()
	defer e.diagMu.Unlock()
	if p.Version < e.publication.Version {
		return
	}
	e.publication = p
	e.wakeLocked()
}

// syncDiagnoses are the titles of the diagnoses gopls starts for sync's
// notifications (didOpen, didChange, didChangeWatchedFiles), in order. Any
// other "diagnosing …" (the initial workspace load) is only waited for.
var syncDiagnoses = map[string]bool{
	"diagnosing opened files":          true,
	"diagnosing changed files":         true,
	"diagnosing files changed on disk": true,
}

// notifyProgress tracks gopls's diagnoses.
func (e *Engine) notifyProgress(params json.RawMessage) {
	var p progressParams
	if json.Unmarshal(params, &p) != nil {
		return
	}
	token := string(p.Token)
	e.diagMu.Lock()
	defer e.diagMu.Unlock()
	switch p.Value.Kind {
	case "begin":
		if !strings.HasPrefix(p.Value.Title, "diagnosing ") {
			return
		}
		if e.working == nil {
			e.working = map[string]bool{}
		}
		e.working[token] = true
		if syncDiagnoses[p.Value.Title] {
			e.begun++
		}
		e.progress = true
	case "end":
		if !e.working[token] {
			return
		}
		delete(e.working, token)
	default:
		return
	}
	e.wakeLocked()
}

// wakeLocked wakes the Diagnose calls waiting under diagMu.
func (e *Engine) wakeLocked() {
	if e.diagChanged != nil {
		close(e.diagChanged)
	}
	e.diagChanged = make(chan struct{})
}

// Diagnose type-checks a cell, returning no problems when gopls is unavailable.
func (e *Engine) Diagnose(ctx context.Context, req Request) ([]Diagnostic, error) {
	select {
	case <-e.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.startErr != nil {
		return nil, nil
	}
	e.mu.Lock()
	client := e.client
	e.mu.Unlock()
	if client == nil {
		return nil, nil
	}
	select {
	case <-client.Done():
		return nil, nil
	default:
	}
	_, version, err := e.sync(client, req)
	if err != nil {
		return nil, err
	}
	e.mu.Lock()
	sent := e.mirrors[version].sent
	e.mu.Unlock()
	return e.awaitDiagnostics(ctx, client.Done(), req, version, sent)
}

// awaitDiagnostics waits for the complete diagnostics of mirror version,
// the sent-th notification. done is closed when gopls exits.
func (e *Engine) awaitDiagnostics(ctx context.Context, done <-chan struct{}, req Request, version, sent int) ([]Diagnostic, error) {
	for {
		e.diagMu.Lock()
		p := e.publication
		if e.diagChanged == nil {
			e.diagChanged = make(chan struct{})
		}
		changed := e.diagChanged
		// A publication isn't necessarily final: gopls may publish a
		// version before diagnosing it (an empty list, when it clears
		// orphaned-file diagnostics after the previous change) and again
		// once it has. Once gopls began diagnosing this change and no
		// diagnosis is in flight, the last publication is complete.
		// (Should the titles change, begun stays 0: settle when idle.)
		settled := !e.progress || len(e.working) == 0 && (e.begun >= sent || e.begun == 0)
		e.diagMu.Unlock()
		if p.Version >= version && settled {
			e.mu.Lock()
			m, ok := e.mirrors[p.Version]
			e.mu.Unlock()
			if !ok || m.cellID != req.CellID || m.src != req.Src {
				return nil, errStale
			}
			return mapDiagnostics(m.cs, m.src, p.Diagnostics), nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-done:
			return nil, nil
		case <-changed:
		}
	}
}

// mapDiagnostics drops generated code, low-severity suggestions, and errors
// repaired by the kernel's build-time rewrites.
func mapDiagnostics(cs kernel.CompletionSource, cellSrc string, ds []lspDiagnostic) []Diagnostic {
	lines, cell := strings.Split(cs.Content, "\n"), strings.Split(cellSrc, "\n")
	lastStmt := -1
	for i, s := range cs.Segments {
		if s.Stmt {
			lastStmt = i
		}
	}
	var out []Diagnostic
	for _, d := range ds {
		if d.Severity != SeverityError && d.Severity != SeverityWarning {
			continue
		}
		for i, s := range cs.Segments {
			start, end := d.Range.Start, d.Range.End
			if start.Line < s.Line || start.Line >= s.Line+s.Lines {
				continue
			}
			msg := d.Message
			if strings.Contains(msg, "imported and not used") || strings.Contains(msg, "redeclared in this block") || strings.Contains(msg, "other declaration of") ||
				(s.Stmt && start.Line == s.Line && strings.Contains(msg, "declared and not used")) ||
				(i == lastStmt && strings.Contains(msg, "is not used")) {
				break
			}
			if end.Line >= s.Line+s.Lines {
				end.Line = s.Line + s.Lines - 1
				end.Character = utf16Len(lines[end.Line])
			}
			convert := func(p position) (int, int, bool) {
				row := s.CellLine + p.Line - s.Line
				if p.Line < 0 || p.Line >= len(lines) || row < 0 || row >= len(cell) {
					return 0, 0, false
				}
				b := min(utf16ToByte(lines[p.Line], p.Character), len(cell[row]))
				return row, utf8.RuneCountInString(cell[row][:b]), true
			}
			row, col, ok := convert(start)
			er, ec, endOK := convert(end)
			if ok && endOK && (er > row || er == row && ec >= col) {
				out = append(out, Diagnostic{Row: row, Col: col, EndRow: er, EndCol: ec, Severity: d.Severity, Message: msg, Source: d.Source})
			}
			break
		}
	}
	return out
}
