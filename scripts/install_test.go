package scripts_test

// These tests guard install.sh at the repository root. A user runs it as
// `curl ... | bash`, so a mistake reaches a user's machine before anyone
// sees it. The tests serve a fake release over HTTP and point the script at
// it with GOPYTER_RELEASES_URL, so no test touches GitHub.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBinary is the content of the "gopyter" file in the fake archive.
const fakeBinary = "#!/bin/sh\necho fake gopyter\n"

// release describes the fake release a test server serves.
type release struct {
	tag     string            // e.g. "v1.2.3"
	files   map[string][]byte // asset name → body
	missing map[string]bool   // asset names to answer with 404
}

// newRelease makes a release for tag with correct archives for every
// platform the release ships, and a checksum file covering them, in the
// layout goreleaser writes.
func newRelease(t *testing.T, tag string) *release {
	t.Helper()
	version := strings.TrimPrefix(tag, "v")
	r := &release{tag: tag, files: map[string][]byte{}, missing: map[string]bool{}}
	var sums strings.Builder
	for _, platform := range []string{"linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64"} {
		asset := fmt.Sprintf("gopyter_%s_%s.tar.gz", version, platform)
		archive := tarGz(t, map[string]string{
			"README.md": "readme",
			"gopyter":   fakeBinary,
		})
		sum := sha256.Sum256(archive)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(sum[:]), asset)
		r.files[asset] = archive
	}
	r.files[fmt.Sprintf("gopyter_%s_checksums.txt", version)] = []byte(sums.String())
	return r
}

func (r *release) serve(t *testing.T) string {
	t.Helper()
	prefix := "/download/" + r.tag + "/"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name, ok := strings.CutPrefix(req.URL.Path, prefix)
		body, found := r.files[name]
		if !ok || !found || r.missing[name] {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar write: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// fakeUname writes a `uname` that reports os and arch, and returns a PATH
// that finds it first. The tests then do not depend on the host platform.
func fakeUname(t *testing.T, osName, arch string) string {
	t.Helper()
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\ncase \"$1\" in -s) echo %s ;; -m) echo %s ;; *) exit 1 ;; esac\n", osName, arch)
	if err := os.WriteFile(filepath.Join(dir, "uname"), []byte(script), 0o755); err != nil {
		t.Fatalf("write uname: %v", err)
	}
	return dir + string(os.PathListSeparator) + os.Getenv("PATH")
}

// install runs install.sh and returns the combined output and exit code.
func install(t *testing.T, baseURL, path string, args ...string) (string, int) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("..", "install.sh"))
	if err != nil {
		t.Fatalf("resolve script: %v", err)
	}
	cmd := exec.Command("bash", append([]string{script, "--no-checks"}, args...)...)
	cmd.Env = []string{
		"PATH=" + path,
		"HOME=" + t.TempDir(),
		"GOPYTER_RELEASES_URL=" + baseURL,
	}
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run install.sh: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return string(out), code
}

func TestInstallVerifiesAndInstallsTheBinary(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "v1.2.3")
	binDir := filepath.Join(t.TempDir(), "bin")

	// The version is given without the "v": the script must add it for the
	// tag, and drop it for the asset name.
	out, code := install(t, r.serve(t), fakeUname(t, "Linux", "x86_64"),
		"--version", "1.2.3", "--bin-dir", binDir)
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "Checksum verified") {
		t.Errorf("output does not report the checksum check:\n%s", out)
	}

	got, err := os.ReadFile(filepath.Join(binDir, "gopyter"))
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(got) != fakeBinary {
		t.Errorf("installed binary = %q, want %q", got, fakeBinary)
	}
	info, err := os.Stat(filepath.Join(binDir, "gopyter"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed binary is not executable: %v", info.Mode())
	}
	// Only the binary is installed, not the README beside it.
	if _, err := os.Stat(filepath.Join(binDir, "README.md")); err == nil {
		t.Errorf("README.md was installed into the bin directory")
	}
}

// macOS on Apple silicon is a shipped platform: uname reports "Darwin" and
// "arm64", which must map to the darwin_arm64 asset.
func TestInstallSupportsMacOS(t *testing.T) {
	t.Parallel()
	r := newRelease(t, "v1.2.3")
	binDir := filepath.Join(t.TempDir(), "bin")
	out, code := install(t, r.serve(t), fakeUname(t, "Darwin", "arm64"),
		"--version", "v1.2.3", "--bin-dir", binDir)
	if code != 0 {
		t.Fatalf("exit %d, output:\n%s", code, out)
	}
	if !strings.Contains(out, "gopyter_1.2.3_darwin_arm64.tar.gz") {
		t.Errorf("did not download the darwin_arm64 asset:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(binDir, "gopyter")); err != nil {
		t.Errorf("binary not installed: %v", err)
	}
}

// Each refusal case must exit non-zero AND leave nothing installed: a
// script that prints an error but still copies the binary has failed.
func TestInstallRefusesAnUnverifiedBinary(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(r *release)
		want   string
	}{
		{
			name: "checksum mismatch",
			mutate: func(r *release) {
				r.files["gopyter_1.2.3_checksums.txt"] = []byte(strings.Repeat("0", 64) + "  gopyter_1.2.3_linux_amd64.tar.gz\n")
			},
			want: "Checksum mismatch",
		},
		{
			name: "no entry for the asset",
			mutate: func(r *release) {
				r.files["gopyter_1.2.3_checksums.txt"] = []byte(strings.Repeat("0", 64) + "  gopyter_1.2.3_linux_arm64.tar.gz\n")
			},
			want: "has no entry for gopyter_1.2.3_linux_amd64.tar.gz",
		},
		{
			name:   "checksum file missing",
			mutate: func(r *release) { r.missing["gopyter_1.2.3_checksums.txt"] = true },
			want:   "Refusing to install an unverified binary",
		},
		{
			name:   "archive missing",
			mutate: func(r *release) { r.missing["gopyter_1.2.3_linux_amd64.tar.gz"] = true },
			want:   "Could not download",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRelease(t, "v1.2.3")
			tc.mutate(r)
			binDir := filepath.Join(t.TempDir(), "bin")

			out, code := install(t, r.serve(t), fakeUname(t, "Linux", "x86_64"),
				"--version", "v1.2.3", "--bin-dir", binDir)
			if code == 0 {
				t.Fatalf("exit 0, want a refusal; output:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output does not contain %q:\n%s", tc.want, out)
			}
			if _, err := os.Stat(filepath.Join(binDir, "gopyter")); err == nil {
				t.Errorf("a binary was installed after a refusal")
			}
		})
	}
}

// Releases cover Linux and macOS on amd64 and arm64 (see .goreleaser.yaml).
// For anything else the script must say so, and point at `go install`,
// before it downloads anything, rather than fail later on a 404.
func TestInstallRefusesAnUnsupportedPlatform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		os, arch, want string
	}{
		{"FreeBSD", "amd64", "Unsupported OS"},
		{"MINGW64_NT-10.0", "x86_64", "Unsupported OS"},
		{"Linux", "riscv64", "Unsupported architecture"},
	}
	for _, tc := range cases {
		t.Run(tc.os+"_"+tc.arch, func(t *testing.T) {
			t.Parallel()
			r := newRelease(t, "v1.2.3")
			out, code := install(t, r.serve(t), fakeUname(t, tc.os, tc.arch),
				"--version", "v1.2.3", "--bin-dir", t.TempDir())
			if code == 0 {
				t.Fatalf("exit 0, want a refusal; output:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("output does not contain %q:\n%s", tc.want, out)
			}
			if !strings.Contains(out, "go install github.com/mark3labs/gopyter@latest") {
				t.Errorf("refusal does not suggest go install:\n%s", out)
			}
			if strings.Contains(out, "Downloading") {
				t.Errorf("script downloaded before it refused the platform:\n%s", out)
			}
		})
	}
}

// install.sh builds asset names from goreleaser's defaults. If someone sets
// an archive name_template or changes the checksum name, every install
// breaks with a 404 on the next release. Fail here instead.
func TestInstallMatchesGoreleaserAssetNames(t *testing.T) {
	t.Parallel()
	cfg, err := os.ReadFile(filepath.Join("..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	s := string(cfg)

	if !strings.Contains(s, `name_template: "{{ .ProjectName }}_{{ .Version }}_checksums.txt"`) {
		t.Errorf("checksum name_template changed; update install.sh to match")
	}
	archives := s[strings.Index(s, "archives:"):strings.Index(s, "checksum:")]
	if strings.Contains(archives, "name_template") {
		t.Errorf("archives set a name_template; install.sh expects the default gopyter_<version>_<os>_<arch>.tar.gz")
	}
	if !strings.Contains(archives, "tar.gz") {
		t.Errorf("archive format is not tar.gz; install.sh extracts with tar -xzf")
	}
}

// install.sh refuses platforms by name before downloading. Every goos and
// goarch that goreleaser builds must be one the script accepts, or users on
// a shipped platform are turned away.
func TestInstallAcceptsEveryReleasedPlatform(t *testing.T) {
	t.Parallel()
	cfg, err := os.ReadFile(filepath.Join("..", ".goreleaser.yaml"))
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	unames := map[string]string{"linux": "Linux", "darwin": "Darwin", "amd64": "x86_64", "arm64": "aarch64"}
	list := func(key string) []string {
		s := string(cfg)
		i := strings.Index(s, key+":")
		if i < 0 {
			t.Fatalf("no %s in .goreleaser.yaml", key)
		}
		var out []string
		for _, line := range strings.Split(s[i:], "\n")[1:] {
			item, ok := strings.CutPrefix(strings.TrimSpace(line), "- ")
			if !ok {
				break
			}
			out = append(out, item)
		}
		return out
	}
	for _, goos := range list("goos") {
		for _, goarch := range list("goarch") {
			osName, arch := unames[goos], unames[goarch]
			if osName == "" || arch == "" {
				t.Errorf("no uname mapping for %s/%s; extend this test and install.sh", goos, goarch)
				continue
			}
			t.Run(goos+"_"+goarch, func(t *testing.T) {
				t.Parallel()
				r := newRelease(t, "v1.2.3")
				out, code := install(t, r.serve(t), fakeUname(t, osName, arch),
					"--version", "v1.2.3", "--bin-dir", t.TempDir())
				if code != 0 {
					t.Fatalf("exit %d for a released platform, output:\n%s", code, out)
				}
				if want := fmt.Sprintf("gopyter_1.2.3_%s_%s.tar.gz", goos, goarch); !strings.Contains(out, want) {
					t.Errorf("did not download %s:\n%s", want, out)
				}
			})
		}
	}
}
