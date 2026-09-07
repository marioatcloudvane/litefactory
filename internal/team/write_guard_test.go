package team

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileAtomicRefusesPathOutsideOwnedPaths is T-46/AC-3's
// fault-injected test: no code path in this package today ever asks
// writeFileAtomic to write somewhere outside the Owned Paths (that is the
// whole point of the guard), so this calls the guarded primitive directly
// with a deliberately bad relPath — standing in for a future bug in this
// package's own write path — and proves the guard refuses it rather than
// relying on no caller ever getting it wrong.
func TestWriteFileAtomicRefusesPathOutsideOwnedPaths(t *testing.T) {
	root := t.TempDir()

	badPaths := []string{
		"escaped-secret.txt",           // outside every Owned Path, at repo root
		"../outside-repo.txt",          // escapes root entirely
		".git/hooks/pre-commit",        // inside the repo but never an Owned Path
		".claude/not-agents/sneaky.md", // inside .claude/ but not the agents/ dir
	}

	for _, bad := range badPaths {
		err := writeFileAtomic(root, bad, []byte("payload"), 0o666)
		if err == nil {
			t.Errorf("writeFileAtomic(%q) succeeded, want it refused as outside Owned Paths", bad)
			continue
		}
		if !errors.Is(err, ErrWriteOutsideOwnedPath) {
			t.Errorf("writeFileAtomic(%q) error = %v, want ErrWriteOutsideOwnedPath", bad, err)
		}
		var wopErr *WriteOutsideOwnedPathError
		if !errors.As(err, &wopErr) {
			t.Errorf("writeFileAtomic(%q) error = %v, want *WriteOutsideOwnedPathError", bad, err)
		}

		// Nothing must have landed on disk, inside or outside root, from a
		// refused write.
		if _, statErr := os.Stat(filepath.Join(root, bad)); !os.IsNotExist(statErr) {
			t.Errorf("writeFileAtomic(%q) wrote a file despite being refused", bad)
		}
	}
}

// TestIsOwnedWriteTargetAcceptsEveryPathThisPackageActuallyWrites is a
// regression guard the other direction: every relPath this package's own
// call sites pass to writeFileAtomic must be accepted, so a future
// tightening of the Owned Paths list cannot silently break a legitimate
// write without a test failing here first.
func TestIsOwnedWriteTargetAcceptsEveryPathThisPackageActuallyWrites(t *testing.T) {
	standaloneClaude, err := standalonePath("claude", "go-architect")
	if err != nil {
		t.Fatalf("standalonePath: %v", err)
	}
	standaloneCodex, err := standalonePath("codex", "go-tester")
	if err != nil {
		t.Fatalf("standalonePath: %v", err)
	}
	standaloneOpencode, err := standalonePath("opencode", "go-writer")
	if err != nil {
		t.Fatalf("standalonePath: %v", err)
	}

	good := []string{
		standaloneClaude,
		standaloneCodex,
		standaloneOpencode,
		RootFileClaude,
		RootFileOpencode,
		filepath.Join(litefactoryDir, lockfileName),
		filepath.Join(litefactoryDir, overridesName),
		gitignoreName,
	}

	for _, p := range good {
		if !isOwnedWriteTarget(p) {
			t.Errorf("isOwnedWriteTarget(%q) = false, want true (this package writes here)", p)
		}
	}
}
