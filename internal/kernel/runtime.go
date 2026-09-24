package kernel

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The runtime packages cell programs import: gopyter's own API (nb) and
// the GoNB compatibility layer, adapted from GoNB's gonbui packages
// (https://github.com/janpfeifer/gonb, MIT license: runtime/gonb/LICENSE). They are part of this repository (so they
// are built, vetted and tested with it) and written into the kernel
// workspace as two local modules, which its go.mod uses through replace
// directives. That keeps them offline and in sync with gopyter.
//
//go:embed runtime
var runtimeFS embed.FS

// RuntimeDirName is the workspace directory holding the runtime modules.
const RuntimeDirName = "gopyter_runtime"

// zeroVersion is the version required for the replaced runtime modules.
const zeroVersion = "v0.0.0-00010101000000-000000000000"

// runtimeModule is a module written from runtime/<dir>.
type runtimeModule struct {
	dir, path, gomod string
}

var runtimeModules = []runtimeModule{
	{"gopyter", "github.com/mark3labs/gopyter", "module github.com/mark3labs/gopyter\n\ngo 1.23\n"},
	{"gonb", "github.com/janpfeifer/gonb", "module github.com/janpfeifer/gonb\n\ngo 1.23\n\nrequire github.com/mark3labs/gopyter " + zeroVersion + "\n"},
}

// repoRuntime is the import path of the runtime packages in this
// repository; in the workspace, runtime/<dir>/ is module <path>.
const repoRuntime = "github.com/mark3labs/gopyter/internal/kernel/runtime/"

// Paths of the runtime packages, as cells import them.
const (
	NBPath      = "github.com/mark3labs/gopyter/nb"
	GonbuiPath  = "github.com/janpfeifer/gonb/gonbui"
	WidgetsPath = GonbuiPath + "/widgets"
)

// runtimePackages maps the package names cells use to their paths, so
// that e.g. nb.Display works without an import.
var runtimePackages = map[string]string{
	"nb":       NBPath,
	"gonbui":   GonbuiPath,
	"widgets":  WidgetsPath,
	"comms":    GonbuiPath + "/comms",
	"dom":      GonbuiPath + "/dom",
	"protocol": GonbuiPath + "/protocol",
}

// rewriteImports maps the repository's import paths of the runtime
// packages to the ones they have in the workspace.
func rewriteImports(src string) string {
	for _, m := range runtimeModules {
		src = strings.ReplaceAll(src, `"`+repoRuntime+m.dir+"/", `"`+m.path+"/")
	}
	return src
}

// setupRuntime writes the runtime modules into the workspace and points
// its go.mod at them.
func (k *Kernel) setupRuntime(ctx context.Context) error {
	root := filepath.Join(k.Dir, RuntimeDirName)
	if err := os.RemoveAll(root); err != nil {
		return err
	}
	err := fs.WalkDir(runtimeFS, "runtime", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "_test.go") {
			return err
		}
		b, err := runtimeFS.ReadFile(path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "runtime/")))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, []byte(rewriteImports(string(b))), 0o644)
	})
	if err != nil {
		return err
	}
	args := []string{"mod", "edit"}
	for _, m := range runtimeModules {
		if err := os.WriteFile(filepath.Join(root, m.dir, "go.mod"), []byte(m.gomod), 0o644); err != nil {
			return err
		}
		args = append(args,
			"-require="+m.path+"@"+zeroVersion,
			"-replace="+m.path+"=./"+RuntimeDirName+"/"+m.dir)
	}
	if out, err := k.goCmd(ctx, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("go mod edit: %v: %s", err, out)
	}
	return nil
}
