package kernel

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// CheckDirName is the sub-package of the workspace where Check builds
// cells. It is separate from the workspace's own main package, which
// Execute rewrites, so a check can run while a cell executes.
const CheckDirName = "gopyter_check"

// CheckResult is the outcome of Check.
type CheckResult struct {
	// Errors are the compile errors, positioned in the cell like those of
	// Execute (In[n]:line:col). Empty when the cell compiles.
	Errors string
	// Missing lists imported packages whose modules are not in the
	// workspace yet. Execute downloads them; Check does not, so when
	// Missing is set the rest of the cell was not type-checked.
	Missing []string
}

// OK reports whether the cell compiled.
func (r CheckResult) OK() bool { return r.Errors == "" && len(r.Missing) == 0 }

// Check compiles src the way Execute would compile it as cell cellID, with
// the declarations of the other cells, but doesn't run it: the kernel's
// declarations are left unchanged and !shell or %magic lines are ignored.
// It never uses the network or edits go.mod, so it doesn't download modules
// (see CheckResult.Missing). The returned error is for failures other than
// compile errors, like a cancelled ctx.
func (k *Kernel) Check(ctx context.Context, cellID, name, src string) (CheckResult, error) {
	// Checks share one directory; Execute has its own.
	k.checkMu.Lock()
	defer k.checkMu.Unlock()

	pc, err := parseCell(cellID, name, src)
	if err != nil {
		return CheckResult{Errors: cleanErrors(err.Error(), k.Dir)}, nil
	}
	if !pc.hasCode {
		return CheckResult{}, nil
	}

	dir := filepath.Join(k.Dir, CheckDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return CheckResult{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "gopyter_helpers.go"), []byte(helpersSrc), 0o644); err != nil {
		return CheckResult{}, err
	}
	// Unlike Execute's -mod=mod, which resolves unknown imports over the
	// network and records them in go.mod, a check stays offline and leaves
	// the workspace's module alone. Later settings win, so these override.
	env := append(k.goCmd(ctx).Env, "GOFLAGS=-mod=readonly", "GOPROXY=off")

	// The same steps as Execute's prepare, in dir. merge only reads the
	// kernel's declarations; committing them is Execute's job.
	decls, imps := k.merge(cellID, pc.decls, pc.imports)
	if len(pc.defines) > 0 {
		how, extra := k.hoist(ctx, dir, env, cellID, pc, decls, imps)
		pc.render(how)
		if len(extra) > 0 {
			decls, imps = k.merge(cellID, append(slices.Clip(pc.decls), extra...), pc.imports)
		}
	}
	write := func() error {
		gen, _ := k.generate(decls, imps, pc)
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(gen), 0o644); err != nil {
			return err
		}
		return writeVars(dir, decls)
	}
	if err := write(); err != nil {
		return CheckResult{}, err
	}

	k.mu.Lock()
	build := append(append([]string{"build"}, k.goflags...), "-o", os.DevNull, ".")
	k.mu.Unlock()
	for {
		cmd := k.goCmd(ctx, build...)
		cmd.Dir, cmd.Env = dir, env
		out, err := cmd.CombinedOutput()
		if ctx.Err() != nil {
			return CheckResult{}, ctx.Err()
		}
		if err == nil {
			return CheckResult{}, nil
		}
		if pc.body != pc.plain && strings.Contains(string(out), "used as value") {
			// As in Execute: a trailing call without a value isn't displayed.
			pc.body = pc.plain
			if err := write(); err != nil {
				return CheckResult{}, err
			}
			continue
		}
		if missing := checkMissing(string(out)); len(missing) > 0 {
			return CheckResult{Missing: missing}, nil
		}
		return CheckResult{Errors: cleanErrors(string(out), dir)}, nil
	}
}

// offlineMissingRe matches the error of an offline, read-only build for an
// import whose module isn't required yet.
var offlineMissingRe = regexp.MustCompile(`cannot find module providing package ([^\s;:]+)`)

// checkMissing returns the packages a check could not find modules for.
func checkMissing(out string) []string {
	missing := missingModules(out)
	for _, m := range offlineMissingRe.FindAllStringSubmatch(out, -1) {
		if !slices.Contains(missing, m[1]) {
			missing = append(missing, m[1])
		}
	}
	return missing
}
