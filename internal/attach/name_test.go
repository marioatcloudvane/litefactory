package attach

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestMintedNamesCannotEscapeTheDirectory is the one property this function
// exists for. The name it is built from is whatever a stranger typed as alt
// text on a public issue, and the result is joined onto a path.
func TestMintedNamesCannotEscapeTheDirectory(t *testing.T) {
	t.Parallel()

	hostile := []string{
		"../../etc/passwd",
		"..",
		".",
		"/etc/shadow",
		`..\..\windows\system32`,
		"a/b/c",
		"\x00truncated",
		strings.Repeat("A", 4096),
		"", // no name at all
		"？？？",
		".bashrc",
		"-rf",
	}

	const dir = "/runs/0001.attachments"
	for i, name := range hostile {
		got := mintFileName(i+1, name, ".png")

		if got != filepath.Base(got) {
			t.Errorf("mintFileName(%q) = %q, which is not a bare base name", name, got)
		}
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("mintFileName(%q) = %q, which contains a path separator", name, got)
		}
		if strings.Contains(got, "..") {
			t.Errorf("mintFileName(%q) = %q, which contains a traversal", name, got)
		}
		if strings.HasPrefix(got, ".") || strings.HasPrefix(got, "-") {
			t.Errorf("mintFileName(%q) = %q, which starts with %q", name, got, got[:1])
		}
		if strings.ContainsRune(got, 0) {
			t.Errorf("mintFileName(%q) = %q, which contains a NUL", name, got)
		}
		if !strings.HasSuffix(got, ".png") {
			t.Errorf("mintFileName(%q) = %q, which lost its extension", name, got)
		}
		if joined := filepath.Join(dir, got); !strings.HasPrefix(joined, dir+"/") {
			t.Errorf("mintFileName(%q) joined to %q gives %q, which is outside it", name, dir, joined)
		}
	}
}

func TestMintedNamesAreUniquePerOrdinal(t *testing.T) {
	t.Parallel()

	// Two attachments whose names slug identically still get distinct files:
	// the ordinal in front is what guarantees it, not the name.
	first := mintFileName(1, "the same!", ".png")
	second := mintFileName(2, "the-same", ".png")
	if first == second {
		t.Errorf("both attachments minted %q", first)
	}
}

func TestMintedNamesStayReadable(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"login screen":       "01-login-screen.png",
		"Screenshot 2026-09": "01-screenshot-2026-09.png",
		"a  b":               "01-a-b.png",
		"":                   "01-attachment.png",
		"🎉":                  "01-attachment.png",
	}
	for name, want := range tests {
		if got := mintFileName(1, name, ".png"); got != want {
			t.Errorf("mintFileName(1, %q) = %q, want %q", name, got, want)
		}
	}
}
