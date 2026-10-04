package bugchecks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
)

func TestConfiguredCommandInputsObserveWithoutExecution(t *testing.T) {
	root := t.TempDir()
	tool := filepath.Join(root, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 91\n"), 0700); err != nil {
		t.Fatal(err)
	}
	p := Isolation{PATH: root, Home: root, Environment: map[string]string{"CHECK_OPTION": "one"}}
	c := config.BugCheckCommand{Argv: []string{"tool"}, CWD: ".", Env: []string{"CHECK_OPTION"}}
	first, err := p.ObserveInputs(root, c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(tool, []byte("#!/bin/sh\nexit 92\n"), 0700); err != nil {
		t.Fatal(err)
	}
	second, err := p.ObserveInputs(root, c)
	if err != nil || first == second {
		t.Fatalf("binary change identity: %s %s %v", first, second, err)
	}
	p.Environment["CHECK_OPTION"] = "two"
	third, err := p.ObserveInputs(root, c)
	if err != nil || second == third {
		t.Fatalf("environment change identity: %v", err)
	}
	c.Argv[0] = "missing"
	if _, err = p.ObserveInputs(root, c); err == nil {
		t.Fatal("unknown executable certified")
	}
}
