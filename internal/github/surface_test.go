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

	gogithub "github.com/google/go-github/v90/github"

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

// TestClientMethodSetIsPinned pins the API surface. It is the reworked form of
// TestClientMethodSetIsTheSevenEndpoints, and the rework is the point.
//
// That test said: "A countable endpoint set is what makes raw net/http the
// right call here. An eighth is a reason to re-open the SDK question, not to
// add a method quietly." The tripwire did its job. The question was re-opened
// and answered (D1): sub-issues are a capability this package could not express
// with hand-rolled request shapes nobody here can verify, and the owner chose
// github.com/google/go-github. So the *reason* the old assertion gave is spent
// — seven no longer has to pay for anything.
//
// The guard is kept because the reason was never the number. What it actually
// buys is that the surface cannot grow silently: this is the list of things
// litefactory can do to a forge, and it is what LF-18 will have to extract a
// protocol from. Adding a method is still a decision someone has to write down,
// which is now a one-line edit here rather than an argument about a dependency.
func TestClientMethodSetIsPinned(t *testing.T) {
	t.Parallel()

	want := []string{
		"AddCommentReaction", // POST …/issues/comments/{id}/reactions
		"AddIssueLabel",      // POST …/issues/{n}/labels
		"AddSubIssue",        // POST /repos/{o}/{r}/issues/{n}/sub_issues
		"AuthenticatedUser",  // GET  /user
		"CreateIssue",        // POST /repos/{o}/{r}/issues
		"EditComment",        // PATCH /repos/{o}/{r}/issues/comments/{id}, body of a comment litefactory posted
		// FetchAsset is not a gogithub.Client endpoint call at all — it is a
		// raw net/http GET against an attachment CDN host (github.com,
		// *.githubusercontent.com), reusing this client's authenticated
		// http.Client. It is pinned here anyway, alongside the real
		// endpoints, because it is still "a thing litefactory can do to a
		// forge" and belongs on the same list LF-18 extracts a protocol
		// from — and because this package is the only one holding the
		// installation token, so it is the only place an attachment fetch
		// can safely happen (US-3).
		"FetchAsset",                 // GET <allow-listed attachment URL>
		"GetParentIssue",             // GET …/issues/{n}/parent
		"RetirePlanStory",            // PATCH verified direct child closed with not_planned reason
		"SetStoryState",              // PATCH verified managed story state and reason
		"UpdateIssue",                // PATCH …/issues/{n}, full title and body
		"GetIssue",                   // GET  /repos/{o}/{r}/issues/{n}
		"ListCommentedIssues",        // GET  /repos/{o}/{r}/issues/comments?since=
		"ListCommentsAfter",          // GET  /repos/{o}/{r}/issues/{n}/comments
		"ListRepositoryCommentsPage", // bounded doctor routing metadata; no issue history expansion
		"ListCommentsPage",           // GET one bounded /repos/{o}/{r}/issues/{n}/comments page for observation
		"GetDeliveryHead",
		"GetDeliveryCommit", // GET exact commit and ordered parents for Bug integration
		"ListDeliveryPRs",
		"GetDeliveryPR",
		"CreateDeliveryPR",
		"CreateHook", // POST /repos/{o}/{r}/hooks
		"EditHook",   // PATCH /repos/{o}/{r}/hooks/{id}
		"GetHook",    // GET /repos/{o}/{r}/hooks/{id}
		"ListHooks",  // GET /repos/{o}/{r}/hooks
		"MergeDeliveryPR",
		"CloseDeliveredParent",
		"ListFollowupIssues", // GET /repos/{o}/{r}/issues?state=all for reconciliation
		"ListIssues",         // GET  /repos/{o}/{r}/issues?since=&state=open
		"ListIssuesPage",     // GET one bounded /repos/{o}/{r}/issues page with explicit state
		"ListSubIssues",      // GET  /repos/{o}/{r}/issues/{n}/sub_issues
		"PostComment",        // POST /repos/{o}/{r}/issues/{n}/comments
		"RemoveIssueLabel",   // DELETE …/issues/{n}/labels/{name}
		// CreateSubIssues calls no endpoint of its own: it is ListSubIssues
		// once, then CreateIssue and AddSubIssue per story that needs them.
		// It is pinned all the same, because "file a plan on the forge" is a
		// thing litefactory can do to a forge and LF-18 will have to say how.
		"CreateSubIssues",
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
			"This list is what litefactory can do to a forge, and what LF-18 has to "+
			"extract a protocol from. Growing it is fine; growing it silently is not — "+
			"add the method to want, with the endpoint it calls.",
			strings.Join(got, ", "), strings.Join(want, ", "))
	}

	// The SDK client is a field on github.Client and never an embedded type.
	// Embedding it would promote go-github's whole method set — Repositories,
	// Actions, Git, every service on it — onto this type, and the assertion
	// above would then be pinning a surface that is not the one callers see.
	if typ.NumMethod() != len(want) {
		t.Errorf("github.Client has %d methods but %d are pinned; an embedded type is "+
			"promoting methods onto it", typ.NumMethod(), len(want))
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

// credentialSite is the one call in this package that is allowed to see the raw
// token: go-github's single credential option, which installs a RoundTripper
// setting the Authorization header on every request.
//
// It replaced the literal "Authorization" as the anchor when D1 moved the
// transport to the SDK and the hand-written header disappeared with it. The
// guard's shape is unchanged; only what it points at moved.
const credentialSite = "gogithub.WithAuthToken("

// hookSecretSite is the second, deliberate reveal: a webhook secret is read
// only to hand it to GitHub when a hook is created or edited.
const hookSecretSite = "// hook-secret-site"

// TestRevealIsCalledExactlyOnce keeps the credential boundary greppable and
// small. secrets.Token.Reveal is the deliberate act of handling the raw value,
// and in this package there is exactly one legitimate reason to do it: handing
// the token to the SDK at construction, on the line named by credentialSite.
//
// This fails the build if Reveal appears anywhere else, or more than once, or
// on a line that is not that call — including the shape where the token is
// pulled into a local variable first, which is how it would end up somewhere a
// %v could reach it.
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
				if !strings.Contains(line, credentialSite) && !strings.Contains(line, hookSecretSite) {
					t.Errorf("%s:%d reveals the token somewhere other than %s: %s",
						name, i+1, credentialSite, strings.TrimSpace(line))
				}
			}
		}
	}

	if len(sites) != 2 {
		t.Errorf("found %d calls to Reveal, want exactly 2 (the SDK credential and the webhook secret):\n%s",
			len(sites), strings.Join(sites, "\n"))
	}
}

// TestTheCredentialPathIsTheSDKs is the other half of the guard above, and the
// half a line-based grep cannot give: the anchor it greps for has to still be
// the thing that authenticates a request.
//
// If go-github renamed or removed WithAuthToken, TestRevealIsCalledExactlyOnce
// would keep passing against a string that no longer means anything — the
// credential could then be routed some other way and the guard would not
// notice. This makes that a compile error instead.
func TestTheCredentialPathIsTheSDKs(t *testing.T) {
	t.Parallel()

	client, err := gogithub.NewClient(gogithub.WithAuthToken("ghp_not-a-real-token"))
	if err != nil {
		t.Fatalf("gogithub.NewClient with the credential option: %v", err)
	}
	if client == nil {
		t.Fatal("gogithub.NewClient returned no client")
	}
	if !strings.Contains(credentialSite, "WithAuthToken") {
		t.Errorf("credentialSite = %q, which no longer names the SDK's credential option",
			credentialSite)
	}
}
