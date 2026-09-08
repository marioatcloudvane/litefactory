package team

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreName is the fixed, repo-root file this package manages one line
// of: an ignore rule for litefactoryDir, so that .litefactory — machine-local
// state describing one worktree's install (the lockfile and any US-13
// overrides) — never enters this repo's commit history (US-16).
const gitignoreName = ".gitignore"

// gitignoreEntry is the exact line ensureGitignoreEntry keeps present in
// root's .gitignore, in the standard git directory-ignore form.
const gitignoreEntry = litefactoryDir + "/"

// ensureGitignoreEntry makes sure root's .gitignore contains gitignoreEntry
// exactly once, creating .gitignore if it does not already exist and leaving
// every other line untouched. It is idempotent by construction: a repeat
// call against a .gitignore that already carries the entry — including one
// a person added by hand, with or without the trailing slash this package
// always writes — makes no write at all, so the line is never duplicated
// across any number of team add/update runs (AC-1).
//
// Callers acquire the per-repo flock (lock.go) before calling this, so two
// concurrent Add/SetOverride/UnsetOverride calls against the same root
// cannot race a read of .gitignore against each other's write.
func ensureGitignoreEntry(root string) error {
	if err := refuseSymlink(root, gitignoreName); err != nil {
		return err
	}
	path := filepath.Join(root, gitignoreName)
	// #nosec G304 -- path is the fixed .gitignore under the selected repository; refuseSymlink checked every component.
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("read %s: %w", gitignoreName, err)
		}
		data = nil
	}
	if gitignoreHasEntry(data) {
		return nil
	}

	var buf bytes.Buffer
	buf.Write(data)
	if len(data) > 0 && data[len(data)-1] != '\n' {
		buf.WriteByte('\n')
	}
	buf.WriteString(gitignoreEntry)
	buf.WriteByte('\n')

	if err := writeFileAtomic(root, gitignoreName, buf.Bytes()); err != nil {
		return fmt.Errorf("write %s: %w", gitignoreName, err)
	}
	return nil
}

// gitignoreHasEntry reports whether data already contains a line that names
// litefactoryDir as ignored — exactly gitignoreEntry, or the same directory
// named without the trailing slash a person might type by hand — so an
// existing rule is recognized rather than duplicated. Leading/trailing
// whitespace on each line is ignored; no broader gitignore glob syntax
// (e.g. "**/.litefactory/") is special-cased, since this package only ever
// writes the one exact form.
func gitignoreHasEntry(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := strings.TrimSpace(string(line))
		if trimmed == gitignoreEntry || trimmed == litefactoryDir {
			return true
		}
	}
	return false
}
