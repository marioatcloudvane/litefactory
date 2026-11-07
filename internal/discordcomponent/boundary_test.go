package discordcomponent_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/discordcomponent"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
)

// Check the lifecycle owner and every Discord/remote consumer adapter, including
// nested packages. Compatibility store aliases in messaging carry values or
// error identities only; no alias is permitted in the new component.
func TestDiscordComponentConsumerBoundary(t *testing.T) {
	for _, root := range []string{".", "../messaging", "../connector"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return err
			}
			imports := map[string]string{}
			for _, spec := range file.Imports {
				imported, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					return err
				}
				name := filepath.Base(imported)
				if spec.Name != nil {
					name = spec.Name.Name
				}
				if name == "." {
					t.Errorf("%s hides imported capability names", path)
				}
				imports[name] = imported
				for _, forbidden := range []string{"database/sql", "/internal/daemon", "/internal/turn", "/internal/submission", "/pkg/execution"} {
					if imported == forbidden || strings.HasSuffix(imported, forbidden) || strings.Contains(imported, forbidden+"/") {
						t.Errorf("%s imports privileged capability %s", path, imported)
					}
				}
				if root == "." && (strings.HasSuffix(imported, "/internal/store") || strings.HasSuffix(imported, "/internal/owneraction")) {
					t.Errorf("component imports private persistence/coordinator %s", imported)
				}
			}
			ast.Inspect(file, func(node ast.Node) bool {
				selected, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				pkg, ok := selected.X.(*ast.Ident)
				if !ok {
					return true
				}
				imported := imports[pkg.Name]
				for _, coordinator := range []string{"/internal/owneraction", "/internal/durableworkflow", "/internal/taskdiscussion"} {
					if strings.HasSuffix(imported, coordinator) && (selected.Sel.Name == "Service" || selected.Sel.Name == "New") {
						t.Errorf("%s acquires a full factory coordinator", path)
					}
				}
				if strings.HasSuffix(imported, "/internal/messagingstorage") && selected.Sel.Name == "New" {
					t.Errorf("%s constructs privileged storage instead of receiving its port", path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// Follow public fields and method signatures, including embedded interfaces and
// callback arguments/results. A private adapter may retain its database; its
// consumer-facing port may not expose it or arbitrary transaction callbacks.
func TestDiscordComponentPublicCapabilities(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var inspect func(reflect.Type)
	inspect = func(typ reflect.Type) {
		if typ == nil || seen[typ] {
			return
		}
		seen[typ] = true
		path := typ.PkgPath()
		if path == "database/sql" || strings.HasSuffix(path, "/internal/store") || strings.HasSuffix(path, "/internal/daemon") || (strings.HasSuffix(path, "/internal/owneraction") && typ.Name() == "Service") {
			t.Errorf("consumer capability exposes %s", typ)
			return
		}
		switch typ.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			inspect(typ.Elem())
		case reflect.Map:
			inspect(typ.Key())
			inspect(typ.Elem())
		case reflect.Func:
			for i := 0; i < typ.NumIn(); i++ {
				inspect(typ.In(i))
			}
			for i := 0; i < typ.NumOut(); i++ {
				inspect(typ.Out(i))
			}
		case reflect.Interface:
			for i := 0; i < typ.NumMethod(); i++ {
				inspect(typ.Method(i).Type)
			}
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				field := typ.Field(i)
				if field.IsExported() {
					inspect(field.Type)
				}
			}
		}
	}
	inspect(reflect.TypeFor[discordcomponent.Options]())
	inspect(reflect.TypeFor[discordcomponent.Hooks]())
	port := reflect.TypeFor[messagingstorage.Port]()
	inspect(port)
	for i := 0; i < port.NumMethod(); i++ {
		method := port.Method(i)
		for j := 0; j < method.Type.NumIn(); j++ {
			if method.Type.In(j).Kind() == reflect.Func {
				t.Errorf("storage method %s accepts an arbitrary callback", method.Name)
			}
		}
	}
}
