package factory_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Keep future cutovers from reintroducing a transport policy dependency into
// the application owner or persistence capabilities into its shared values.
// Ticket identities and display-only disk observations are neutral shared values.
func TestFactoryDependencyDirection(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			name, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				return err
			}
			for _, forbidden := range []string{"/internal/cli", "/internal/daemon", "/internal/messaging", "/internal/connector", "/internal/discordcomponent"} {
				if strings.HasSuffix(name, forbidden) || strings.Contains(name, forbidden+"/") {
					t.Errorf("%s imports transport or host %s", path, name)
				}
			}
			if strings.HasPrefix(path, "contracts"+string(filepath.Separator)) && (name == "database/sql" || strings.Contains(name, "/internal/") && !strings.Contains(name, "/internal/ticket") && name != "github.com/cloudvane/litefactory/internal/diskstatus") {
				t.Errorf("shared contract %s imports implementation %s", path, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
