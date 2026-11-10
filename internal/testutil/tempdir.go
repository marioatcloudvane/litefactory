package testutil

import (
	"os"
	"path/filepath"
)

// shortTempRootLimit bounds the resolved temp root of a Unix socket fixture.
// macOS limits a socket path to 104 bytes; /private/tmp and an agent
// sandbox's private TMPDIR (for example /private/tmp/claude-501) fit, the
// per-user /private/var/folders/... directory does not.
const shortTempRootLimit = 40

// ShortTempRoot is the parent for os.MkdirTemp in tests that need a short
// path, typically for Unix sockets. It is the system temp directory when that
// is short and writable, so the tests run inside a sandbox that grants only
// its private TMPDIR, and /tmp otherwise. Use it instead of a fixed "/tmp".
func ShortTempRoot() string {
	return shortTempRoot(shortTempRootLimit, os.TempDir(), "/tmp")
}

// shortTempRoot returns the first root that resolves within limit and accepts
// a new directory, and the last root when none does, so the caller's own
// os.MkdirTemp reports the failure.
func shortTempRoot(limit int, roots ...string) string {
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil || len(resolved) > limit {
			continue
		}
		probe, err := os.MkdirTemp(resolved, "lf-probe-")
		if err != nil {
			continue
		}
		if err := os.Remove(probe); err != nil {
			continue
		}
		return root
	}
	return roots[len(roots)-1]
}
