// Package factorylibtest gives tests the repository's factory library source
// (teams/source/factory), read from disk like an owner install would.
package factorylibtest

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factorylib"
)

// SourceDir is the absolute path of teams/source/factory in this checkout.
func SourceDir() string {
	// Trimmed builds report module paths from runtime.Caller, so prefer the
	// checkout containing the test process's working directory.
	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; dir = filepath.Dir(dir) {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return filepath.Join(dir, "teams", "source", "factory")
			}
			if filepath.Dir(dir) == dir {
				break
			}
		}
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "teams", "source", "factory")
}

// Library reads and validates the repository library.
func Library(t testing.TB) *factorylib.Library {
	t.Helper()
	lib, err := factorylib.ReadSource(t.Context(), SourceDir())
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// Connector is the repository's connector guidance tree (guidance/...).
func Connector() fs.FS { return os.DirFS(filepath.Join(SourceDir(), factorylib.ConnectorDir)) }

// Install installs the repository library into a test factory home.
func Install(t testing.TB, paths config.Paths) *factorylib.Library {
	t.Helper()
	lib, _, err := factorylib.Install(t.Context(), paths, SourceDir())
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// With returns the repository library with some files replaced, keyed by
// library path (for example factorylib.Judge.Path()).
func With(t testing.TB, replace map[string]string) *factorylib.Library {
	t.Helper()
	files := Library(t).Files()
	for name, text := range replace {
		files[name] = []byte(text)
	}
	lib, err := factorylib.FromFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// InstallWith installs the repository library with some files replaced into a
// test factory home, through the same install an owner runs.
func InstallWith(t testing.TB, paths config.Paths, replace map[string]string) *factorylib.Library {
	t.Helper()
	source := t.TempDir()
	if err := os.CopyFS(source, os.DirFS(SourceDir())); err != nil {
		t.Fatal(err)
	}
	for name, text := range replace {
		if err := os.WriteFile(filepath.Join(source, filepath.FromSlash(name)), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	lib, _, err := factorylib.Install(t.Context(), paths, source)
	if err != nil {
		t.Fatal(err)
	}
	return lib
}

// SourceFunc adapts a function to factorylib.Source.
type SourceFunc func(context.Context) (*factorylib.Library, error)

// Library calls f.
func (f SourceFunc) Library(ctx context.Context) (*factorylib.Library, error) { return f(ctx) }
