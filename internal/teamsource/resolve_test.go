package teamsource_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/teamsource"
)

func resolvePut(t *testing.T, name string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// resolveRoot builds a small fixture repository with a base and a child source.
func resolveRoot(t *testing.T, inherit string, child map[string]string) *os.Root {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	resolvePut(t, filepath.Join(dir, "teams/source/base-x/base.json"), []byte(`{"variables":["team","lang"]}`))
	resolvePut(t, filepath.Join(dir, "teams/source/base-x/stack-profile.json"), []byte("profile"))
	resolvePut(t, filepath.Join(dir, "teams/source/base-x/plan/plan.dot"), []byte(`team="{{team}}" lang={{lang}}`))
	resolvePut(t, filepath.Join(dir, "teams/source/base-x/plan/a.md"), []byte("base a {{lang}}"))
	resolvePut(t, filepath.Join(dir, "teams/source/base-x/plan/b.md"), []byte("base b"))
	resolvePut(t, filepath.Join(dir, "teams/source/kid/inherit.json"), []byte(inherit))
	for name, body := range child {
		resolvePut(t, filepath.Join(dir, "teams/source/kid", name), []byte(body))
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestResolveReplaceAddExcludeAndVariables(t *testing.T) {
	root := resolveRoot(t, `{"base":"base-x","replace":["plan/a.md"],"exclude":["plan/b.md"],"variables":{"lang":"go"}}`,
		map[string]string{"plan/a.md": "kid a {{lang}}", "plan/c.md": "kid c"})
	got, err := teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"stack-profile.json": []byte("profile"),
		"plan/plan.dot":      []byte(`team="kid-team" lang=go`),
		"plan/a.md":          []byte("kid a {{lang}}"), // the child's own bytes are not filled
		"plan/c.md":          []byte("kid c"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("resolved %q", got)
	}
}

func TestResolveReportsEveryProblemSorted(t *testing.T) {
	root := resolveRoot(t, `{"base":"base-x","replace":["plan/zz.md"],"exclude":["plan/a.md","plan/none.md"],"variables":{"team":"x","extra":"y"}}`,
		map[string]string{"plan/a.md": "kid", "plan/b.md": "collides"})
	_, err := teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err == nil {
		t.Fatal("bad input accepted")
	}
	const p = "teams/source/kid/inherit.json: "
	want := strings.Join([]string{
		p + "add plan/b.md: collides with base base-x file; list it under replace",
		p + "exclude plan/a.md: source also provides this file",
		p + "exclude plan/none.md: base base-x has no such file",
		p + "replace plan/zz.md: base base-x has no such file",
		p + "variable extra: not declared by base base-x",
		p + "variable lang: declared by base base-x but not set",
		p + "variable team: filled from the release slug and cannot be set",
	}, "\n")
	if err.Error() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", err, want)
	}
}

func TestResolveMissingBaseAndUndeclaredPlaceholder(t *testing.T) {
	root := resolveRoot(t, `{"base":"nope"}`, nil)
	_, err := teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err == nil || !strings.Contains(err.Error(), "teams/source/kid/inherit.json: base nope: missing or unreadable base") {
		t.Fatalf("missing base: %v", err)
	}
	root = resolveRoot(t, `{"base":"base-x","variables":{"lang":"go"}}`, nil)
	resolvePut(t, filepath.Join(root.Name(), "teams/source/base-x/plan/b.md"), []byte("{{ghost}}"))
	_, err = teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	want := "teams/source/kid/inherit.json: variable ghost: used in base file plan/b.md but not declared by base base-x"
	if err == nil || err.Error() != want {
		t.Fatalf("undeclared: %v", err)
	}
	root = resolveRoot(t, `{"base":"base-x","variables":{"lang":"a\nb"}}`, nil)
	_, err = teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err == nil || !strings.Contains(err.Error(), "variable lang: value must be a single line without {{") {
		t.Fatalf("multi-line value: %v", err)
	}
}

func TestResolveRejectsUnknownManifestFields(t *testing.T) {
	root := resolveRoot(t, `{"base":"base-x","bogus":1}`, nil)
	if _, err := teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team"); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestResolveCarriesTeamFilesOutsideWorkflows(t *testing.T) {
	root := resolveRoot(t, `{"base":"base-x","variables":{"lang":"go"}}`, nil)
	resolvePut(t, filepath.Join(root.Name(), "teams/source/base-x", teamsource.AccessClassifierPath), []byte("base access {{lang}}"))
	resolvePut(t, filepath.Join(root.Name(), "teams/source/base-x/access/other.md"), []byte("not a team file"))
	got, err := teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err != nil {
		t.Fatal(err)
	}
	if string(got[teamsource.AccessClassifierPath]) != "base access go" {
		t.Fatalf("inherited access role %q", got[teamsource.AccessClassifierPath])
	}
	if _, ok := got["access/other.md"]; ok {
		t.Fatal("unlisted file outside the workflow folders was carried")
	}

	root = resolveRoot(t, `{"base":"base-x","replace":["access/access-classifier.md"],"variables":{"lang":"go"}}`,
		map[string]string{teamsource.AccessClassifierPath: "team access"})
	resolvePut(t, filepath.Join(root.Name(), "teams/source/base-x", teamsource.AccessClassifierPath), []byte("base access"))
	got, err = teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err != nil {
		t.Fatal(err)
	}
	if string(got[teamsource.AccessClassifierPath]) != "team access" {
		t.Fatalf("replaced access role %q", got[teamsource.AccessClassifierPath])
	}

	root = resolveRoot(t, `{"base":"base-x","variables":{"lang":"go"}}`, nil)
	got, err = teamsource.ResolveSource(t.Context(), root, "teams/source/kid", "kid-team")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[teamsource.AccessClassifierPath]; ok {
		t.Fatal("absent access role was invented")
	}
}
