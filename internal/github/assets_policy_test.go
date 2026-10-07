package github_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// TestFetchAssetNeverRetriesOrSleeps is US-8's T-8.4, and it guards a
// decision rather than a bug.
//
// The feature spec originally placed the attachment retry and its 30s/120s
// budgets in this package. CONC-6 holds that this package retries nothing
// and sleeps nowhere, which is what makes every forge call in this codebase
// cost one predictable round trip and keeps a stuck network from being this
// package's problem. The plan resolved the tension by splitting the work:
// FetchAsset stays single-shot and returns ErrAssetUnauthorized;
// internal/attach owns the retry and both timeouts.
//
// That split is invisible in the code — FetchAsset simply does not have a
// loop in it — so without this test the natural fix for "attachments
// sometimes 404" is to add three lines here, which would compile, pass every
// other test, and quietly undo the decision. The plan says a deviation must
// be logged in .aiteam/AC_AMENDMENTS.md before merge; this is what makes
// anyone notice they are deviating.
func TestFetchAssetNeverRetriesOrSleeps(t *testing.T) {
	t.Parallel()

	const file = "assets.go"

	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}

	// Nothing in this file may reach for a clock or a timer.
	for _, spec := range parsed.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote import %s: %v", spec.Path.Value, err)
		}
		if path == "time" {
			t.Errorf("%s imports time; the retry and both timing budgets belong in internal/attach (CONC-6)", file)
		}
	}

	ast.Inspect(parsed, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			// A loop here is how a retry gets written. There is no legitimate
			// reason for one in a single-shot GET.
			t.Errorf("%s contains a loop at %s; FetchAsset is single-shot and retries nothing",
				file, fset.Position(n.Pos()))
		case *ast.SelectorExpr:
			if ident, ok := node.X.(*ast.Ident); ok {
				if ident.Name == "time" && strings.HasPrefix(node.Sel.Name, "Sleep") {
					t.Errorf("%s calls time.%s at %s; this package sleeps nowhere",
						file, node.Sel.Name, fset.Position(n.Pos()))
				}
			}
		}
		return true
	})
}
