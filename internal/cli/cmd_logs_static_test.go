package cli

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestLogsFollowHasNoGoroutinesOrChannels is AC-2.5: the follow implementation
// must be a synchronous poll loop (select on ctx.Done() against a ticker),
// never a goroutine or a hand-declared channel. Unlike
// TestLogsNeverImportsRedact and TestLogsUsesNoDaemonIPC, which only inspect
// import declarations via parser.ImportsOnly, this parses each file in full
// and walks its AST for *ast.GoStmt (a "go" statement launching a goroutine)
// and *ast.ChanType (a "chan" type declaration), either of which would violate
// the "no goroutine, no channel" constraint even though nothing forbidden
// would show up in the import list.
//
// time.Ticker's own C field is a channel, but it is declared inside the
// standard library, not authored in this package's source, so it does not
// trip this check.
func TestLogsFollowHasNoGoroutinesOrChannels(t *testing.T) {
	for _, name := range logsSourceFiles {
		t.Run(name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
			if err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}

			ast.Inspect(file, func(n ast.Node) bool {
				switch node := n.(type) {
				case *ast.GoStmt:
					t.Errorf("%s:%d: found a \"go\" statement (goroutine); this feature must use a synchronous poll loop",
						name, fset.Position(node.Pos()).Line)
				case *ast.ChanType:
					t.Errorf("%s:%d: found a \"chan\" type declaration; this feature must not declare its own channels",
						name, fset.Position(node.Pos()).Line)
				}
				return true
			})
		})
	}
}
