// Package notebook reads and writes Jupyter notebooks (nbformat v4), using a
// kernelspec compatible with GoNB so notebooks can be shared with Jupyter.
package notebook

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CellType is the kind of a notebook cell.
type CellType string

const (
	Code     CellType = "code"
	Markdown CellType = "markdown"
	Raw      CellType = "raw"
)

// OutputKind is the kind of a cell output.
type OutputKind string

const (
	Stdout      OutputKind = "stdout"
	Stderr      OutputKind = "stderr"
	Result      OutputKind = "result"
	MarkdownOut OutputKind = "markdown"
	ImageOut    OutputKind = "image" // Text is base64 image data (PNG unless read from a notebook)
	Error       OutputKind = "error"
	Info        OutputKind = "info"
)

// Output is a single output of a code cell.
type Output struct {
	Kind OutputKind
	Text string
	// ID is the id of an updatable output (DisplayID) while its cell
	// runs. Like Jupyter's transient display_id, it isn't saved.
	ID string
}

// Cell is a notebook cell.
type Cell struct {
	ID             string
	Type           CellType
	Source         string
	ExecutionCount int
	Outputs        []Output
	Metadata       map[string]any
}

// Notebook is a Jupyter notebook.
type Notebook struct {
	Cells    []*Cell
	Metadata map[string]any
}

// NewID returns a random cell id.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// multiline handles nbformat strings which can be a string or a list of strings.
type multiline string

func (m *multiline) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*m = multiline(s)
		return nil
	}
	var ss []string
	if err := json.Unmarshal(b, &ss); err != nil {
		return err
	}
	*m = multiline(strings.Join(ss, ""))
	return nil
}

func (m multiline) MarshalJSON() ([]byte, error) {
	s := string(m)
	lines := []string{}
	for s != "" {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, s)
			break
		}
		lines = append(lines, s[:i+1])
		s = s[i+1:]
	}
	return json.Marshal(lines)
}

type rawOutput struct {
	OutputType     string               `json:"output_type"`
	Name           string               `json:"name,omitempty"`
	Text           *multiline           `json:"text,omitempty"`
	Data           map[string]multiline `json:"data,omitempty"`
	Metadata       map[string]any       `json:"metadata,omitempty"`
	ExecutionCount *int                 `json:"execution_count,omitempty"`
	EName          string               `json:"ename,omitempty"`
	EValue         string               `json:"evalue,omitempty"`
	Traceback      []string             `json:"traceback,omitempty"`
}

type rawCell struct {
	CellType       string          `json:"cell_type"`
	ID             string          `json:"id,omitempty"`
	ExecutionCount json.RawMessage `json:"execution_count,omitempty"`
	Metadata       map[string]any  `json:"metadata"`
	Outputs        *[]rawOutput    `json:"outputs,omitempty"`
	Source         multiline       `json:"source"`
}

type rawNotebook struct {
	Cells         []rawCell      `json:"cells"`
	Metadata      map[string]any `json:"metadata"`
	NBFormat      int            `json:"nbformat"`
	NBFormatMinor int            `json:"nbformat_minor"`
}

// New returns an empty notebook with a single code cell.
func New() *Notebook {
	return &Notebook{Cells: []*Cell{{ID: NewID(), Type: Code}}}
}

// Load reads a notebook from disk.
func Load(path string) (*Notebook, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse decodes an ipynb document.
func Parse(b []byte) (*Notebook, error) {
	var raw rawNotebook
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("invalid notebook: %w", err)
	}
	nb := &Notebook{Metadata: raw.Metadata}
	for _, rc := range raw.Cells {
		c := &Cell{ID: rc.ID, Type: CellType(rc.CellType), Source: string(rc.Source), Metadata: rc.Metadata}
		if c.ID == "" {
			c.ID = NewID()
		}
		_ = json.Unmarshal(rc.ExecutionCount, &c.ExecutionCount)
		if rc.Outputs != nil {
			for _, o := range *rc.Outputs {
				c.Outputs = append(c.Outputs, decodeOutput(o)...)
			}
		}
		nb.Cells = append(nb.Cells, c)
	}
	if len(nb.Cells) == 0 {
		nb.Cells = New().Cells
	}
	return nb, nil
}

func decodeOutput(o rawOutput) []Output {
	switch o.OutputType {
	case "stream":
		kind := Stdout
		if o.Name == "stderr" {
			kind = Stderr
		}
		if o.Text != nil {
			return []Output{{Kind: kind, Text: string(*o.Text)}}
		}
	case "execute_result", "display_data":
		if md, ok := o.Data["text/markdown"]; ok {
			return []Output{{Kind: MarkdownOut, Text: string(md)}}
		}
		for _, mime := range []string{"image/png", "image/jpeg", "image/gif"} {
			if img, ok := o.Data[mime]; ok {
				// Jupyter may wrap base64 over lines; keep it on one.
				return []Output{{Kind: ImageOut, Text: strings.Join(strings.Fields(string(img)), "")}}
			}
		}
		if t, ok := o.Data["text/plain"]; ok {
			return []Output{{Kind: Result, Text: string(t)}}
		}
	case "error":
		text := o.EName + ": " + o.EValue
		if len(o.Traceback) > 0 {
			text = strings.Join(o.Traceback, "\n")
		}
		return []Output{{Kind: Error, Text: text}}
	}
	return nil
}

func encodeOutput(o Output, count int) rawOutput {
	text := multiline(o.Text)
	switch o.Kind {
	case Stdout, Stderr:
		return rawOutput{OutputType: "stream", Name: string(o.Kind), Text: &text}
	case Info:
		return rawOutput{OutputType: "stream", Name: "stdout", Text: &text}
	case Result:
		c := count
		return rawOutput{OutputType: "execute_result", ExecutionCount: &c, Metadata: map[string]any{},
			Data: map[string]multiline{"text/plain": text}}
	case MarkdownOut:
		return rawOutput{OutputType: "display_data", Metadata: map[string]any{},
			Data: map[string]multiline{"text/markdown": text}}
	case ImageOut:
		return rawOutput{OutputType: "display_data", Metadata: map[string]any{},
			Data: map[string]multiline{imageMime(o.Text): text, "text/plain": "[image]"}}
	default:
		return rawOutput{OutputType: "error", EName: "error", EValue: o.Text, Traceback: strings.Split(o.Text, "\n")}
	}
}

// imageMime sniffs the type of base64 image data, so images read from a
// notebook are written back under the type they came with.
func imageMime(b64 string) string {
	// A partial final quantum is an error but still decodes the bytes before
	// it, which is all the sniffing needs.
	head, _ := base64.StdEncoding.DecodeString(b64[:min(len(b64), 16)])
	switch {
	case bytes.HasPrefix(head, []byte("\xff\xd8")):
		return "image/jpeg"
	case bytes.HasPrefix(head, []byte("GIF8")):
		return "image/gif"
	}
	return "image/png"
}

// Marshal encodes the notebook as ipynb JSON.
func (nb *Notebook) Marshal() ([]byte, error) {
	meta := nb.Metadata
	if meta == nil {
		meta = map[string]any{}
	}
	if _, ok := meta["kernelspec"]; !ok {
		meta["kernelspec"] = map[string]any{"display_name": "Go (gonb)", "language": "go", "name": "gonb"}
	}
	if _, ok := meta["language_info"]; !ok {
		meta["language_info"] = map[string]any{"name": "go", "file_extension": ".go", "mimetype": "text/x-go"}
	}
	raw := rawNotebook{Metadata: meta, NBFormat: 4, NBFormatMinor: 5, Cells: []rawCell{}}
	for _, c := range nb.Cells {
		md := c.Metadata
		if md == nil {
			md = map[string]any{}
		}
		rc := rawCell{CellType: string(c.Type), ID: c.ID, Metadata: md, Source: multiline(c.Source)}
		if c.Type == Code {
			rc.ExecutionCount = json.RawMessage("null")
			if c.ExecutionCount > 0 {
				rc.ExecutionCount = json.RawMessage(fmt.Sprint(c.ExecutionCount))
			}
			outs := []rawOutput{}
			for _, o := range c.Outputs {
				outs = append(outs, encodeOutput(o, c.ExecutionCount))
			}
			rc.Outputs = &outs
		}
		raw.Cells = append(raw.Cells, rc)
	}
	b, err := json.MarshalIndent(raw, "", " ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Save writes the notebook to path atomically.
func (nb *Notebook) Save(path string) error {
	b, err := nb.Marshal()
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
