package redact

// Internal tests. Two things can only be checked from inside the package.
//
// The fail-closed recover is one: the real pass is pure string work with no way
// to panic on demand, so the only honest way to exercise the recover is to
// substitute a pass that panics — and the seam that allows it is unexported on
// purpose, so that no caller outside this package can weaken the pass.
//
// The package's own structural invariants are the other: "exactly one recover"
// and "zero third-party imports" are properties of this source directory, and
// checking them means reading it.

import (
	"bytes"
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/secrets"
)

func TestPrepareFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		pass func(string) (string, Counts)
	}{
		{"panic", func(string) (string, Counts) { panic("matching failed") }},
		{"residual", func(text string) (string, Counts) { return text, Counts{} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := New(Options{Environ: []string{"TEST_SECRET=private-literal"}})
			r.pass = tt.pass
			view, err := r.Prepare(context.Background(), "private-literal")
			if !errors.Is(err, ErrRedactionFailed) || view.Valid() || view.String() != "" {
				t.Fatalf("incomplete redaction escaped: valid=%v err=%v", view.Valid(), err)
			}
		})
	}
}

// TestApplyRecoversFromAPanickingPass is AC-4.9.
//
// A panic in the pass means the pass did not finish, which means the body may
// still hold a credential. Apply must convert it into an error so the caller
// aborts: no comment posted, run left un-reported, watermark left where it was.
// A loud line has to reach daemon.log, because the visible symptom of this
// failure is a run that silently never reported.
func TestApplyRecoversFromAPanickingPass(t *testing.T) {
	t.Parallel()

	const token = "lf-live-bot-token-value"
	const input = "the run printed " + token + " right before the pass blew up"

	tests := []struct {
		name string
		pass func(string) (string, Counts)
	}{
		{
			name: "panic with a string",
			pass: func(string) (string, Counts) { panic("regexp: something impossible") },
		},
		{
			name: "panic with an error",
			pass: func(string) (string, Counts) { panic(errors.New("pass exploded")) },
		},
		{
			name: "runtime panic mid-pass",
			pass: func(s string) (string, Counts) {
				// An index out of range: the shape a real bug in a pattern
				// pass would take, rather than a deliberate panic call.
				return s[:len(s)+1], Counts{}
			},
		},
		{
			name: "panic after producing a partially redacted body",
			pass: func(s string) (string, Counts) {
				// The dangerous case. A pass that gets halfway has replaced
				// some credentials and not others; that half-redacted string
				// must never escape as a Body.
				var c Counts
				c.add(ClassGitHubToken, 1)
				_ = strings.ReplaceAll(s, "printed", Replacement)
				panic("halfway through")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

			r := New(Options{
				Logger:  log,
				Token:   secrets.NewToken(token),
				Environ: []string{},
			})
			r.pass = tc.pass

			body, err := r.Apply(t.Context(), input, 65536)

			if err == nil {
				t.Fatal("Apply returned no error after the pass panicked; the comment would be posted")
			}
			var panicErr *PanicError
			if !errors.As(err, &panicErr) {
				t.Fatalf("Apply error = %T (%v), want *redact.PanicError", err, err)
			}
			if !errors.Is(err, ErrRedactionFailed) {
				t.Error("the panic error does not wrap ErrRedactionFailed; a caller matching on the " +
					"one sentinel would post anyway")
			}
			if panicErr.Stack == "" {
				t.Error("PanicError.Stack is empty; the stack is the only clue to which pass failed")
			}

			// The body must be unusable. github.PostComment refuses an invalid
			// Body, so this is the second half of "no comment is posted".
			if body.Valid() {
				t.Error("Apply returned a valid Body after a panic")
			}
			if got := body.String(); got != "" {
				t.Errorf("Apply returned body text after a panic: %q", got)
			}

			// Loud, and at error level: this line is what someone greps for
			// when a run never reported.
			logged := buf.String()
			if !strings.Contains(logged, `"level":"ERROR"`) {
				t.Errorf("the panic was not logged at ERROR level:\n%s", logged)
			}
			if !strings.Contains(logged, "refusing to post") {
				t.Errorf("the log line does not name the consequence:\n%s", logged)
			}
			if strings.Contains(logged, token) {
				t.Errorf("the fail-closed log line leaked the bot token:\n%s", logged)
			}
		})
	}
}

// TestApplyWithoutALoggerStillFailsClosed covers the ordering inside Apply: the
// nil checks run before the deferred recover is installed, so a Redactor this
// package did not build cannot turn the fail-closed path into a second panic
// inside the logger.
func TestApplyWithoutALoggerStillFailsClosed(t *testing.T) {
	t.Parallel()

	// A Redactor with a pass but no logger cannot be built by New; construct
	// it directly to prove Apply still refuses rather than dereferencing nil.
	r := &Redactor{pass: func(string) (string, Counts) { panic("boom") }}

	body, err := r.Apply(t.Context(), "anything", 65536)
	if !errors.Is(err, ErrUninitialized) {
		t.Errorf("Apply error = %v, want ErrUninitialized", err)
	}
	if body.Valid() {
		t.Error("Apply returned a valid Body from a half-built Redactor")
	}
}

// TestResidualLiteralAbortsThePost covers the assert step. A literal surviving
// the pass cannot happen unless the pass has a bug — which is exactly why it is
// checked, because "is the bot token in this body" is a fact and checking it is
// two microseconds.
func TestResidualLiteralAbortsThePost(t *testing.T) {
	t.Parallel()

	const token = "lf-live-bot-token-value"

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := New(Options{Logger: log, Token: secrets.NewToken(token), Environ: []string{}})
	// A pass that claims to have redacted and did not: the bug this catches.
	r.pass = func(s string) (string, Counts) {
		var c Counts
		c.add(ClassBotToken, 1)
		return s, c
	}

	body, err := r.Apply(t.Context(), "the token is "+token, 65536)

	var residual *ResidualError
	if !errors.As(err, &residual) {
		t.Fatalf("Apply error = %v, want *redact.ResidualError", err)
	}
	if residual.Class != ClassBotToken {
		t.Errorf("ResidualError.Class = %s, want %s", residual.Class, ClassBotToken)
	}
	if !errors.Is(err, ErrRedactionFailed) {
		t.Error("the residual error does not wrap ErrRedactionFailed")
	}
	if body.Valid() || body.String() != "" {
		t.Errorf("Apply returned a usable Body containing the token: %q", body.String())
	}
	if strings.Contains(buf.String(), token) {
		t.Errorf("the residual log line leaked the token:\n%s", buf.String())
	}
}

// TestExactlyOneRecoverInThisPackage is the executable half of a named global
// invariant: litefactory has exactly one recover in the tree and it lives here.
//
// The tree-wide half is a grep in review — this package cannot police other
// packages without coupling its tests to them. What it can police is its own
// end of the invariant: that the one recover still exists, in redact.go, and
// that nobody has quietly added a second one here.
//
// If this fails because the recover is gone, do not delete the test. Read the
// comment above Apply first: the failure mode of removing it is an unredacted
// body going out, not a crash anyone notices.
func TestExactlyOneRecoverInThisPackage(t *testing.T) {
	t.Parallel()

	found := make(map[string]int)
	forEachSourceFile(t, func(name string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "recover" {
				found[name]++
			}
			return true
		})
	})

	total := 0
	for _, n := range found {
		total += n
	}
	if total != 1 {
		t.Errorf("found %d recover calls in internal/redact %v, want exactly 1 "+
			"(the documented fail-closed recover in Apply)", total, found)
	}
	if found["redact.go"] != 1 {
		t.Errorf("the fail-closed recover is not in redact.go; found %v", found)
	}
}

// TestPackageHasNoThirdPartyImports is a named global invariant: internal/redact
// depends on the standard library and nothing else.
//
// The reason is not purity. This package is the last thing that touches an
// outbound body, and a dependency here is a supply-chain surface on the code
// path whose whole job is that a credential does not escape. Everything it
// needs — regexp, strings, unicode/utf8, log/slog — is in the standard library.
func TestPackageHasNoThirdPartyImports(t *testing.T) {
	t.Parallel()

	const ownModule = "github.com/cloudvane/litefactory/internal/"

	forEachSourceFile(t, func(name string, file *ast.File, fset *token.FileSet) {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("%s: unquoting import %s: %v", name, spec.Path.Value, err)
			}
			switch {
			case !strings.Contains(strings.SplitN(path, "/", 2)[0], "."):
				// No dot in the first segment: standard library.
			case strings.HasPrefix(path, ownModule):
				// A sibling internal package. internal/secrets is the only one
				// this package needs, and it is where the bot token lives.
				if path != ownModule+"secrets" {
					t.Errorf("%s imports %s; internal/redact should need only internal/secrets",
						name, path)
				}
			default:
				t.Errorf("%s imports the third-party package %s; internal/redact is "+
					"stdlib-only by invariant", name, path)
			}
		}
	})
}

// forEachSourceFile parses every non-test .go file in this package directory.
// Test files are excluded deliberately: the invariants above are about the
// shipped package, and the plan permits go-cmp in tests module-wide.
func forEachSourceFile(t *testing.T, fn func(name string, file *ast.File, fset *token.FileSet)) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	fset := token.NewFileSet()
	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		seen++
		fn(name, file, fset)
	}
	if seen == 0 {
		t.Fatal("no source files parsed; the invariant checks above would pass vacuously")
	}
}
