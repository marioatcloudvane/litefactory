package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const modulePath = "github.com/cloudvane/litefactory"

// Include test dependencies: a library test must not quietly reconnect the
// supposedly independent consumer to factory storage or coordination.
func TestExecutionDependencyBoundary(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-test", "-f", "{{.ImportPath}}", "./pkg/execution/...", "./examples/execution")
	cmd.Dir = filepath.Join("..", "..")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("list dependency closure: %v\n%s", err, output)
	}
	for _, path := range strings.Fields(string(output)) {
		if strings.HasPrefix(path, modulePath+"/internal/") {
			t.Errorf("execution dependency reaches factory package %s", path)
		}
	}
}

func TestExecutionOutsideConsumer(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mod := "module example.com/execution-consumer\n\ngo 1.26\n\nrequire " + modulePath + " v0.0.0\nreplace " + modulePath + " => " + strconv.Quote(root) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	// Compile the consumer's own copy outside this module, where Go's internal
	// import rule also protects the public API from factory-owned parameter types.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), source, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Compile both real provider constructors from outside the module too.
	const constructors = `package main
import (
 "github.com/cloudvane/litefactory/pkg/execution/providers/claude"
 "github.com/cloudvane/litefactory/pkg/execution/providers/codex"
)
func constructProviders(binary string) error {
 if _, err := claude.New(claude.Options{Binary: binary}); err != nil { return err }
 _, err := codex.New(codex.Options{Binary: binary})
 return err
}
`
	if err := os.WriteFile(filepath.Join(dir, "providers.go"), []byte(constructors), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-mod=mod", "-o", filepath.Join(dir, "consumer"), ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile external consumer: %v\n%s", err, output)
	}
}
