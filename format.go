package main

import (
	"os"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
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
	data, changed, err := notebook.RewriteCode(original, kernel.FormatCell)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return notebook.WriteFile(path, data)
}
