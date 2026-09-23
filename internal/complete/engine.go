// Package complete provides code completion for notebook cells, backed by
// gopls when available and by a simple identifier completer otherwise.
package complete

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/lsp"
)

// Kind classifies completion items.
type Kind int

const (
	KindOther Kind = iota
	KindFunc
	KindMethod
	KindVar
	KindConst
	KindField
	KindType
	KindInterface
	KindPackage
	KindKeyword
)

// Item is a completion candidate.
type Item struct {
	Label  string
	Detail string // e.g. the type signature
	Doc    string
	Insert string // text replacing the word before the cursor
	Kind   Kind
	// Replace is how many runes before the cursor Insert replaces.
	Replace int
}

// Result is a completion response.
type Result struct {
	Items []Item
	// Replace is the default number of runes before the cursor to replace.
	Replace int
	// Source names the backend that produced the items ("gopls" or "basic").
	Source string
}

// Request describes what to complete.
type Request struct {
	CellID   string
	Src      string
	Row, Col int  // cursor, 0-based rune column
	Manual   bool // explicitly requested by the user
	Trigger  rune // character that triggered completion, if any
}

// Engine produces completions for notebook cells.
type Engine struct {
	k *kernel.Kernel

	mu       sync.Mutex
	client   *lsp.Client
	uri      string
	file     string
	version  int
	opened   bool
	modTime  time.Time
	ready    chan struct{}
	startErr error

	stdOnce sync.Once
	std     map[string]string // unambiguous package name -> import path
}

// stdPackages maps standard library package names to import paths,
// omitting names shared by several packages (like rand or template).
func (e *Engine) stdPackages() map[string]string {
	e.stdOnce.Do(func() {
		e.std = map[string]string{}
		cmd := exec.Command("go", "list", "std")
		cmd.Dir = e.k.Dir
		out, err := cmd.Output()
		if err != nil {
			return
		}
		ambiguous := map[string]bool{}
		for p := range strings.FieldsSeq(string(out)) {
			if strings.Contains(p, "internal") || strings.HasPrefix(p, "vendor/") {
				continue
			}
			name := path.Base(p)
			if strings.HasPrefix(name, "v") && strings.Count(p, "/") > 0 {
				if _, err := strconv.Atoi(name[1:]); err == nil {
					continue // major version suffix, e.g. math/rand/v2
				}
			}
			if _, dup := e.std[name]; dup {
				ambiguous[name] = true
			}
			e.std[name] = p
		}
		for name := range ambiguous {
			delete(e.std, name)
		}
	})
	return e.std
}

// goplsSettings configures gopls for notebook-style completion.
var goplsSettings = map[string]any{
	"completeUnimported":      true,
	"deepCompletion":          true,
	"usePlaceholders":         false,
	"completionDocumentation": true,
	"matcher":                 "Fuzzy",
	"staticcheck":             false,
	"analyses":                map[string]bool{"unusedvariable": false},
	"completionBudget":        "200ms",
}

// New creates an engine and starts gopls in the background.
func New(k *kernel.Kernel) *Engine {
	e := &Engine{k: k, ready: make(chan struct{})}
	go func() {
		e.startErr = e.start()
		close(e.ready)
	}()
	go e.stdPackages()
	return e
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func (e *Engine) start() error {
	path, err := exec.LookPath("gopls")
	if err != nil {
		return errors.New("gopls not found (go install golang.org/x/tools/gopls@latest)")
	}
	file, err := e.k.CompletionWorkspace()
	if err != nil {
		return err
	}
	cmd := exec.Command(path, "serve")
	cmd.Dir = e.k.Dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	client, err := lsp.Start(cmd, func(method string, params json.RawMessage) any {
		if method == "workspace/configuration" {
			var p struct{ Items []json.RawMessage }
			_ = json.Unmarshal(params, &p)
			out := make([]any, len(p.Items))
			for i := range out {
				out[i] = goplsSettings
			}
			return out
		}
		return nil
	})
	if err != nil {
		return err
	}

	root := fileURI(e.k.Dir)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = client.Call(ctx, "initialize", map[string]any{
		"processId":             os.Getpid(),
		"rootUri":               root,
		"workspaceFolders":      []any{map[string]string{"uri": root, "name": "gopyter"}},
		"initializationOptions": goplsSettings,
		"capabilities": map[string]any{
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true},
			"textDocument": map[string]any{
				"completion": map[string]any{
					"completionItem": map[string]any{
						"snippetSupport":      false,
						"documentationFormat": []string{"plaintext"},
					},
					"contextSupport": true,
				},
			},
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
		},
	}, nil)
	if err == nil {
		err = client.Notify("initialized", map[string]any{})
	}
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("starting gopls: %w", err)
	}
	e.mu.Lock()
	e.client, e.file, e.uri = client, file, fileURI(file)
	e.mu.Unlock()
	return nil
}

// Status reports whether gopls is in use, and why not otherwise. It
// doesn't block.
func (e *Engine) Status() (gopls bool, reason string) {
	select {
	case <-e.ready:
		if e.startErr != nil {
			return false, e.startErr.Error()
		}
		return true, ""
	default:
		return false, "gopls starting"
	}
}

// Close stops gopls.
func (e *Engine) Close() error {
	<-e.ready
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil {
		err := e.client.Close()
		e.client = nil
		return err
	}
	return nil
}

// Complete returns completions for a cell. It waits for gopls to start
// when the request is manual, and falls back to basic completion.
func (e *Engine) Complete(ctx context.Context, req Request) (Result, error) {
	if req.Manual {
		select {
		case <-e.ready:
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
	}
	select {
	case <-e.ready:
	default:
		return e.basic(req), nil
	}
	e.mu.Lock()
	client := e.client
	e.mu.Unlock()
	if client == nil {
		return e.basic(req), nil
	}
	select {
	case <-client.Done():
		return e.basic(req), nil
	default:
	}
	res, err := e.gopls(ctx, client, req)
	if err != nil {
		return Result{}, err
	}
	return res, nil
}

type position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type textEdit struct {
	Range struct {
		Start position `json:"start"`
		End   position `json:"end"`
	} `json:"range"`
	NewText string `json:"newText"`
}

type lspItem struct {
	Label         string          `json:"label"`
	Kind          int             `json:"kind"`
	Detail        string          `json:"detail"`
	Documentation json.RawMessage `json:"documentation"`
	InsertText    string          `json:"insertText"`
	TextEdit      *textEdit       `json:"textEdit"`
}

func (e *Engine) gopls(ctx context.Context, client *lsp.Client, req Request) (Result, error) {
	std := e.stdPackages()
	src := e.k.CompletionSource(req.CellID, req.Src, req.Row, req.Col, func(name string) (string, bool) {
		p, ok := std[name]
		return p, ok
	})
	lines := strings.Split(src.Content, "\n")
	cursorLine := lines[src.Line]
	char := utf16Len(cursorLine[:src.Col])

	e.mu.Lock()
	e.version++
	version := e.version
	opened := e.opened
	e.opened = true
	// Dependencies change when cells `go get` new modules.
	var modChanged bool
	if st, err := os.Stat(filepath.Join(e.k.Dir, "go.mod")); err == nil && !st.ModTime().Equal(e.modTime) {
		modChanged = !e.modTime.IsZero()
		e.modTime = st.ModTime()
	}
	e.mu.Unlock()

	if modChanged {
		_ = client.Notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{
			map[string]any{"uri": fileURI(filepath.Join(e.k.Dir, "go.mod")), "type": 2},
			map[string]any{"uri": fileURI(filepath.Join(e.k.Dir, "go.sum")), "type": 2},
		}})
	}
	doc := map[string]any{"uri": e.uri, "version": version}
	var err error
	if !opened {
		doc["languageId"] = "go"
		doc["text"] = src.Content
		err = client.Notify("textDocument/didOpen", map[string]any{"textDocument": doc})
	} else {
		err = client.Notify("textDocument/didChange", map[string]any{
			"textDocument":   doc,
			"contentChanges": []any{map[string]string{"text": src.Content}},
		})
	}
	if err != nil {
		return Result{}, err
	}

	trigger := map[string]any{"triggerKind": 1}
	if req.Trigger != 0 {
		trigger = map[string]any{"triggerKind": 2, "triggerCharacter": string(req.Trigger)}
	}
	var raw json.RawMessage
	err = client.Call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]string{"uri": e.uri},
		"position":     position{Line: src.Line, Character: char},
		"context":      trigger,
	}, &raw)
	if err != nil {
		return Result{}, err
	}
	var items []lspItem
	if len(raw) > 0 && raw[0] == '[' {
		_ = json.Unmarshal(raw, &items)
	} else {
		var list struct{ Items []lspItem }
		_ = json.Unmarshal(raw, &list)
		items = list.Items
	}

	res := Result{Source: "gopls", Replace: wordLen(cursorLine[:src.Col])}
	for _, it := range items {
		item := Item{
			Label:   it.Label,
			Detail:  it.Detail,
			Doc:     docString(it.Documentation),
			Kind:    kindOf(it.Kind),
			Insert:  it.InsertText,
			Replace: res.Replace,
		}
		if it.TextEdit != nil {
			item.Insert = it.TextEdit.NewText
			if it.TextEdit.Range.Start.Line == src.Line {
				start := utf16ToByte(cursorLine, it.TextEdit.Range.Start.Character)
				if start <= src.Col {
					item.Replace = utf8.RuneCountInString(cursorLine[start:src.Col])
				}
			}
		}
		if item.Insert == "" {
			item.Insert = it.Label
		}
		res.Items = append(res.Items, item)
	}
	return res, nil
}

func docString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var mc struct{ Value string }
	_ = json.Unmarshal(raw, &mc)
	return mc.Value
}

func kindOf(k int) Kind {
	switch k {
	case 3, 4: // Function, Constructor
		return KindFunc
	case 2: // Method
		return KindMethod
	case 6, 12: // Variable, Value
		return KindVar
	case 21, 20: // Constant, EnumMember
		return KindConst
	case 5, 10: // Field, Property
		return KindField
	case 7, 22, 13, 25: // Class, Struct, Enum, TypeParameter
		return KindType
	case 8: // Interface
		return KindInterface
	case 9: // Module
		return KindPackage
	case 14: // Keyword
		return KindKeyword
	}
	return KindOther
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func utf16ToByte(s string, units int) int {
	n := 0
	for i, r := range s {
		if n >= units {
			return i
		}
		n += utf16.RuneLen(r)
	}
	return len(s)
}

func isIdent(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// wordLen is the number of identifier runes at the end of s.
func wordLen(s string) int {
	r := []rune(s)
	n := 0
	for n < len(r) && isIdent(r[len(r)-1-n]) {
		n++
	}
	return n
}
