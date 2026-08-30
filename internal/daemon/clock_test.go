package daemon_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

const (
	// daemonType is the receiver type whose clock this file guards, and
	// clockField is the field that holds it — see [daemon.Options.Now] for the
	// seam itself.
	daemonType = "Daemon"
	clockField = "now"

	// clockReader is the one method allowed to read the clock. It is a method
	// name rather than a set on purpose: the moment this becomes a list, the
	// invariant has already been given up.
	clockReader = "Tick"
)

// TestClockIsReadOnlyFromTick is AC-PAR-7.2. Only [daemon.Daemon.Tick] may read
// the daemon's clock; nothing below Daemon.poll may, and nothing may read it
// from a function literal.
//
// This is not tidiness. The clock is an injected seam, and the one the tests
// inject (testClock, fixture_test.go) advances a whole second per reading —
// deliberately, because a whole second is the resolution of GitHub's `since`
// filter and smaller steps would leave two events in the same second with the
// cursor appearing not to move. A reading is therefore a mutation, and how many
// readings happen is part of what the suite asserts.
//
// Today every reading is on the Tick goroutine and the count is deterministic.
// Once Tick splits into discovery and per-session workers (T-PAR-5, T-PAR-6),
// a d.now() that has drifted below poll would be read from a worker instead:
// the number of readings would then depend on how many issues were dispatched
// and in which order they got there, and every assertion on a `since` string or
// a duration would become intermittently wrong. The sanctioned way for anything
// below poll to have an instant is to take the one the tick began with — the
// tickAt field copied onto the task — not to turn the clock again.
//
// The check lands ahead of the split rather than after it, because a guardrail
// that arrives once the mistake is already made is an audit, and this one is
// cheap enough to have first.
//
// What it catches: any mention of d.now outside Daemon.Tick — the call someone
// adds to a per-issue path because a timestamp was wanted there, which is the
// mistake that is actually likely — and any mention inside a function literal,
// including one written in Tick itself, since a closure is exactly how the
// reading would reach another goroutine.
//
// What it does not catch, and cannot, being a syntactic check over one package:
//
//   - The clock handed down as a value. If Tick were to put the func() time.Time
//     itself onto a task or into a struct, the call site further down reads a
//     field of some other name and this check sees nothing.
//   - A bare time.Now() below poll. That bypasses the seam entirely rather than
//     turning it, so it does not make the fixture non-deterministic — it makes
//     the code untestable instead, which is a different fault with a different
//     remedy.
//   - Anything outside internal/daemon's own non-test files.
func TestClockIsReadOnlyFromTick(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	declared := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		if declaresClock(file) {
			declared = true
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			checkClockReads(t, fset, fn)
		}
	}

	// A rename would otherwise turn this test into one that passes because it
	// no longer looks at anything.
	if !declared {
		t.Fatalf("no %s field of func type found on %s; if the clock was renamed, rename it here too",
			clockField, daemonType)
	}
}

// declaresClock reports whether the file declares the daemon's clock field, so
// that the test can tell "nobody reads it" apart from "it is not called that
// any more".
func declaresClock(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.TypeSpec)
		if !ok || spec.Name.Name != daemonType {
			return true
		}
		structType, ok := spec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range structType.Fields.List {
			if _, ok := field.Type.(*ast.FuncType); !ok {
				continue
			}
			for _, name := range field.Names {
				if name.Name == clockField {
					found = true
				}
			}
		}
		return true
	})
	return found
}

// checkClockReads reports every mention of the clock in fn that the invariant
// does not allow. Any mention counts, not only a call: passing d.now on as a
// value moves the reading somewhere this check cannot see it.
func checkClockReads(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl) {
	t.Helper()

	receivers := daemonNames(fn)
	if len(receivers) == 0 {
		return
	}

	var literals []*ast.FuncLit
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if lit, ok := n.(*ast.FuncLit); ok {
			literals = append(literals, lit)
		}
		return true
	})

	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != clockField {
			return true
		}
		ident, ok := sel.X.(*ast.Ident)
		if !ok || !receivers[ident.Name] {
			return true
		}

		where := fset.Position(sel.Pos())
		switch {
		case within(literals, sel):
			t.Errorf("%s: %s reads %s.%s inside a function literal; the clock is read on the tick goroutine only, and a closure is how a reading escapes to another one",
				where, funcName(fn), ident.Name, clockField)
		case !isClockReader(fn):
			t.Errorf("%s: %s reads %s.%s; only %s.%s may read the clock — take the instant the tick began (task.tickAt) instead",
				where, funcName(fn), ident.Name, clockField, daemonType, clockReader)
		}
		return true
	})
}

// daemonNames returns the identifiers bound to a Daemon within fn — its
// receiver, and any parameter of that type — so that an unrelated x.now on some
// other type is not mistaken for a reading of this one.
func daemonNames(fn *ast.FuncDecl) map[string]bool {
	names := map[string]bool{}
	add := func(fields *ast.FieldList) {
		if fields == nil {
			return
		}
		for _, field := range fields.List {
			if !isDaemon(field.Type) {
				continue
			}
			for _, name := range field.Names {
				if name.Name != "_" {
					names[name.Name] = true
				}
			}
		}
	}
	add(fn.Recv)
	add(fn.Type.Params)
	return names
}

// isDaemon reports whether expr names the daemon type, by value or by pointer.
func isDaemon(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == daemonType
}

// isClockReader reports whether fn is the one method allowed to read the clock.
func isClockReader(fn *ast.FuncDecl) bool {
	return fn.Recv != nil && fn.Name.Name == clockReader
}

// within reports whether node lies inside one of the given function literals.
func within(literals []*ast.FuncLit, node ast.Node) bool {
	for _, lit := range literals {
		if lit.Pos() <= node.Pos() && node.End() <= lit.End() {
			return true
		}
	}
	return false
}

// funcName renders a declaration the way the package's documentation refers to
// it, so a failure names something greppable.
func funcName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	ident, ok := recv.(*ast.Ident)
	if !ok {
		return fn.Name.Name
	}
	return ident.Name + "." + fn.Name.Name
}
