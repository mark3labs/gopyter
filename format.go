package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/spf13/cobra"
)

func formatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "format <notebook.ipynb>",
		Short: "Format code cells in place without executing them",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return formatNotebook(args[0])
		},
	}
}

// formatNotebook keeps unrecognized notebook and output fields opaque. Nothing
// is written until every code cell has been successfully formatted.
func formatNotebook(path string) error {
	original, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(original, &document); err != nil {
		return fmt.Errorf("invalid notebook: %w", err)
	}
	var cells []json.RawMessage
	if err := json.Unmarshal(document["cells"], &cells); err != nil {
		return fmt.Errorf("invalid notebook cells: %w", err)
	}
	if bytes.Equal(bytes.TrimSpace(document["cells"]), []byte("null")) {
		return fmt.Errorf("invalid notebook cells: expected an array")
	}
	changed := false
	for i, raw := range cells {
		var cell map[string]json.RawMessage
		if err := json.Unmarshal(raw, &cell); err != nil {
			return fmt.Errorf("cell %d: %w", i+1, err)
		}
		var kind string
		if err := json.Unmarshal(cell["cell_type"], &kind); err != nil {
			return fmt.Errorf("cell %d: invalid cell_type: %w", i+1, err)
		}
		if kind != "code" {
			continue
		}
		source, array, err := formatCellSource(cell["source"])
		if err != nil {
			return fmt.Errorf("cell %d: invalid source: %w", i+1, err)
		}
		formatted, err := kernel.FormatCell(source)
		if err != nil {
			return fmt.Errorf("cell %d: %w", i+1, err)
		}
		if formatted == source {
			continue
		}
		var value any = formatted
		if array {
			lines := strings.SplitAfter(formatted, "\n")
			if lines[len(lines)-1] == "" {
				lines = lines[:len(lines)-1]
			}
			value = lines
		}
		cell["source"], err = json.Marshal(value)
		if err != nil {
			return err
		}
		cells[i], err = json.Marshal(cell)
		if err != nil {
			return err
		}
		changed = true
	}
	if !changed {
		return nil
	}
	document["cells"], err = json.Marshal(cells)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(document, "", " ")
	if err != nil {
		return err
	}
	return writeFormattedNotebook(path, append(data, '\n'))
}

// formatCellSource accepts both nbformat source encodings, retaining the
// encoding used by the original cell rather than normalizing unrelated data.
func formatCellSource(raw json.RawMessage) (string, bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || (raw[0] != '"' && raw[0] != '[') {
		return "", false, fmt.Errorf("expected a string or an array of strings")
	}
	if raw[0] == '"' {
		var source string
		err := json.Unmarshal(raw, &source)
		return source, false, err
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", true, err
	}
	var source strings.Builder
	for _, part := range parts {
		if len(part) == 0 || part[0] != '"' {
			return "", true, fmt.Errorf("expected an array of strings")
		}
		var text string
		if err := json.Unmarshal(part, &text); err != nil {
			return "", true, err
		}
		source.WriteString(text)
	}
	return source.String(), true, nil
}

// writeFormattedNotebook uses a same-directory temporary file so a failed
// write cannot truncate the original, and preserves its permission bits.
func writeFormattedNotebook(path string, data []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".gopyter-format-*")
	if err != nil {
		return err
	}
	defer func() {
		// The file may already be closed or renamed on the successful path.
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
