package github_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
)

// TestPostCommentTakesARedactedBody is the compile-time guarantee, asserted so
// that weakening it is a test failure and not a quiet refactor.
//
// github.PostComment accepts a redact.Body and never a string. redact.Body has
// no exported constructor and no exported field, so the only way to obtain one
// carrying text is redact.Apply — which means "post a body that has not been
// through the S-7 redaction pass" is not an expressible program. Changing this
// parameter to a string reopens the hole that guarantee closes.
func TestPostCommentTakesARedactedBody(t *testing.T) {
	t.Parallel()

	method, ok := reflect.TypeOf((*github.Client)(nil)).MethodByName("PostComment")
	if !ok {
		t.Fatal("github.Client has no PostComment method")
	}

	// Receiver, ctx, Repo, issue number, body.
	const bodyParam = 4
	if got := method.Type.NumIn(); got != bodyParam+1 {
		t.Fatalf("PostComment takes %d parameters, want %d", got-1, bodyParam)
	}
	if got, want := method.Type.In(bodyParam), reflect.TypeOf(redact.Body{}); got != want {
		t.Fatalf("PostComment's body parameter is %s, want %s.\n\n"+
			"The type is the guarantee: redact.Body can only be produced by "+
			"redact.Apply, so an unredacted body cannot be posted.", got, want)
	}
}

// TestClientMethodSetIsTheSevenEndpoints pins the API surface. A small,
// countable set is the reason this package is net/http rather than an SDK
// dependency, and one arriving quietly is the moment that trade is worth
// revisiting — so the count is asserted rather than assumed.
//
// The seventh was added on 2026-08-25 as a decision, not a convenience: issues
// and comments are indexed separately and lag separately, so a second,
// independent path to a new comment is worth one request per repository per
// tick. See [github.Client.ListCommentedIssues] for the measurement. Seven
// still does not pay for an SDK.
func TestClientMethodSetIsTheSevenEndpoints(t *testing.T) {
	t.Parallel()

	want := []string{
		"AddCommentReaction",  // POST …/issues/comments/{id}/reactions
		"AuthenticatedUser",   // GET  /user
		"GetIssue",            // GET  /repos/{o}/{r}/issues/{n}
		"ListCommentedIssues", // GET  /repos/{o}/{r}/issues/comments?since=
		"ListCommentsAfter",   // GET  /repos/{o}/{r}/issues/{n}/comments
		"ListIssues",          // GET  /repos/{o}/{r}/issues?since=&state=open
		"PostComment",         // POST /repos/{o}/{r}/issues/{n}/comments
		// BaseURL reads a field rather than calling the API.
		"BaseURL",
	}
	slices.Sort(want)

	typ := reflect.TypeOf((*github.Client)(nil))
	got := make([]string, 0, typ.NumMethod())
	for i := range typ.NumMethod() {
		got = append(got, typ.Method(i).Name)
	}
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Fatalf("github.Client method set changed.\n got: %s\nwant: %s\n\n"+
			"A countable endpoint set is what makes raw net/http the right call here. "+
			"An eighth is a reason to re-open the SDK question, not to add a method quietly.",
			strings.Join(got, ", "), strings.Join(want, ", "))
	}
}

// TestNoForgeAbstraction is the executable form of a global invariant: M0
// exports concrete structs, and the Forge protocol is deferred to LF-18 so it
// can be *extracted* from this working implementation rather than guessed at.
// An interface invented now would be shaped by one forge and wrong for the
// second, which is the only reason to have it.
func TestNoForgeAbstraction(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if name == "forge.go" {
			t.Errorf("internal/github/forge.go exists; the Forge protocol is deferred to LF-18")
		}

		file, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			spec, ok := node.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if _, isInterface := spec.Type.(*ast.InterfaceType); isInterface {
				t.Errorf("%s declares the interface %s; internal/github exports concrete "+
					"structs at M0, and a consumer that needs a seam declares it at the "+
					"point of consumption", name, spec.Name.Name)
			}
			if spec.Name.Name == "Capabilities" {
				t.Errorf("%s declares a Capabilities type; capability descriptors are "+
					"part of the deferred Forge protocol", name)
			}
			return true
		})
	}
}

// TestRevealIsCalledExactlyOnce keeps the credential boundary greppable and
// small. secrets.Token.Reveal is the deliberate act of handling the raw value,
// and in this package there is exactly one legitimate reason to do it: setting
// the Authorization header.
func TestRevealIsCalledExactlyOnce(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	var sites []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		contents, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(contents), "\n") {
			if strings.Contains(line, ".Reveal()") {
				sites = append(sites, name+":"+strings.TrimSpace(line))
				if !strings.Contains(line, "Authorization") {
					t.Errorf("%s:%d reveals the token outside the Authorization header: %s",
						name, i+1, strings.TrimSpace(line))
				}
			}
		}
	}

	if len(sites) != 1 {
		t.Errorf("found %d calls to Reveal, want exactly 1:\n%s",
			len(sites), strings.Join(sites, "\n"))
	}
}
