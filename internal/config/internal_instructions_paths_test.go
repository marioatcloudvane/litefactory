package config

import (
	"path/filepath"
	"testing"
)

func TestInternalInstructionsDir(t *testing.T) {
	home := t.TempDir()
	if got, want := NewPaths(home).InternalInstructionsDir(), filepath.Join(home, "internal-instructions"); got != want {
		t.Fatalf("instruction owner directory = %q, want %q", got, want)
	}
}
