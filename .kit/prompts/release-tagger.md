---
description: Semantic version tagging workflow: analyze commits and tag a release
---

# Release Tagging Workflow

Tag a new version of gopyter following semantic versioning.

## Steps

1. **Make sure you're releasing master**:
   - `git switch master && git pull --ff-only`
   - The working tree must be clean (`git status --short` prints nothing)
   - Run the full checks: `gofmt -l .`, `go vet ./...`, `golangci-lint run ./...`, `go test -race ./...`. Do not tag a red build

2. **Fetch remote tags**: `git fetch --tags origin`

3. **Find the latest version**: `git tag -l 'v*' | sort -V | tail -5`
   - **No tags yet?** This is the first release. Propose `v0.1.0` (gopyter is pre-1.0), list the highlights of the whole history, and skip the bump analysis

4. **Analyze changes since the last tag**:
   - `git log <latest-tag>..HEAD --oneline`
   - `git diff <latest-tag>..HEAD --stat`

5. **Determine the version bump** (semver; while < v1.0.0, breaking changes bump MINOR):
   - **MAJOR / breaking**: `BREAKING CHANGE:` footers or `!` subjects, incompatible `.ipynb` output, removed or renamed CLI flags or commands, changed cell semantics (what persists between cells), removed keybindings
   - **MINOR**: `feat:` commits, new commands, flags, magics, keybindings or mouse interactions
   - **PATCH**: `fix:`, `perf:`, and `refactor:` with no visible change
   - **Skip**: `docs:`, `test:`, `chore:` only. Suggest not releasing
   - When in doubt, prefer the smaller bump

6. **Calculate the new version**: increment the chosen segment and reset lower segments to 0

7. **Draft the tag message**, grouped by type. (The GitHub release notes are generated separately by GoReleaser from commit subjects; the tag message is for `git show`.)

   ```
   v0.2.0 - gopls completion and dialog keyboard navigation

   Features:
   - IDE-style code completion backed by gopls, with basic fallback
   - Tab/arrow focus cycling for dialog buttons

   Fixes:
   - Serialize kernel output events to avoid races in headless runs
   ```

8. **Wait for the user to confirm** the version and message before running any tag commands

9. **Create and push an annotated tag**:
   - `git tag -a vX.Y.Z -F /tmp/tag-msg.txt` (write the message to a file so multi-line bodies survive)
   - `git push origin vX.Y.Z`
   - Pushing the tag triggers `.github/workflows/release.yml`: tests, then GoReleaser publishes the Linux/macOS tarballs and checksums that `install.sh` downloads. Watch it with `gh run watch` and confirm the release page lists 4 archives plus `gopyter_X.Y.Z_checksums.txt`
   - Once published, both `curl -fsSL https://raw.githubusercontent.com/mark3labs/gopyter/master/install.sh | bash` and `go install github.com/mark3labs/gopyter@vX.Y.Z` install it

## Guidelines

- Always fetch remote tags first to avoid conflicts
- Always use annotated tags (`-a`) with descriptive messages
- If there are no changes since the last tag, suggest skipping the release

$@
