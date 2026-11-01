package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/pkg/execution/providers/claude"
	"github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)

func TestExecutionProviderConstruction(t *testing.T) {
	directory := t.TempDir()
	binary := filepath.Join(directory, "provider")
	marker := binary + ".launched"
	// The executable would leave a marker if construction accidentally launched
	// it. No provider installation, authentication or network access is needed.
	if err := os.WriteFile(binary, []byte(`#!/bin/sh
touch "$0.launched"
exit 99
`), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := claude.New(claude.Options{Binary: binary, Environment: []string{}, Tools: []string{}}); err != nil {
		t.Fatalf("construct Claude: %v", err)
	}
	if _, err := codex.New(codex.Options{Binary: binary, Environment: []string{}}); err != nil {
		t.Fatalf("construct Codex: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("provider construction launched an executable: %v", err)
	}
}
