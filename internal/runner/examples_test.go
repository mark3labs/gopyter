package runner

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/gopyter/internal/kernel"
	"github.com/mark3labs/gopyter/internal/notebook"
)

// TestExamples runs every example notebook headlessly: they must run
// without failures or notes about lost variables. They only use the
// standard library and gopyter's runtime, so no network is needed.
func TestExamples(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every cell of the examples")
	}
	paths, err := filepath.Glob("../../examples/*.ipynb")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no examples: %v", err)
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			nb, err := notebook.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			k, err := kernel.New("")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := k.Close(); err != nil {
					t.Error(err)
				}
			})
			// Files the notebooks write go to a temporary directory.
			k.RunDir = t.TempDir()
			var out bytes.Buffer
			// Guesses for the terminal notebook's game, then a password.
			stdin := strings.NewReader("50\n25\n75\nsecret\n")
			if failed := Run(context.Background(), k, nb, &out, stdin, false); failed != 0 {
				t.Fatalf("%d cells failed:\n%s", failed, out.String())
			}
			if strings.Contains(out.String(), "not kept") {
				t.Fatalf("variables not kept:\n%s", out.String())
			}
		})
	}
}
