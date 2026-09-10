package config

import (
	"path/filepath"
	"testing"
)

func TestSubmissionDirUsesConfiguredHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	paths, err := Locate()
	if err != nil {
		t.Fatal(err)
	}
	if paths.SubmissionDir() != filepath.Join(home, "s") {
		t.Fatalf("broker path escaped configured home: %s", paths.SubmissionDir())
	}
}
