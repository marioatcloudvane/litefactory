package teamrelease

import (
	"github.com/cloudvane/litefactory/internal/teamsource"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// resolveRoot builds a small fixture repository with a base and a child source.
func resolveRoot(t *testing.T, inherit string, child map[string]string) *os.Root {
	t.Helper()
	dir := canonicalTemp(t)
	put(t, filepath.Join(dir, "teams/source/base-x/base.json"), []byte(`{"variables":["team","lang"]}`))
	put(t, filepath.Join(dir, "teams/source/base-x/stack-profile.json"), []byte("profile"))
	put(t, filepath.Join(dir, "teams/source/base-x/plan/plan.dot"), []byte(`team="{{team}}" lang={{lang}}`))
	put(t, filepath.Join(dir, "teams/source/base-x/plan/a.md"), []byte("base a {{lang}}"))
	put(t, filepath.Join(dir, "teams/source/base-x/plan/b.md"), []byte("base b"))
	put(t, filepath.Join(dir, "teams/source/kid/inherit.json"), []byte(inherit))
	for name, body := range child {
		put(t, filepath.Join(dir, "teams/source/kid", name), []byte(body))
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
	put(t, filepath.Join(root.Name(), "teams/source/base-x/plan/b.md"), []byte("{{ghost}}"))
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

func TestBaseIsNotATeam(t *testing.T) {
	root := canonicalTemp(t)
	for name, raw := range snapshot(t, "../../teams/source/base-team") {
		put(t, filepath.Join(root, "teams/source/base-team", filepath.FromSlash(name)), raw)
	}
	err := GenerateSources(t.Context(), root, false, []string{"teams/source/base-team"})
	if err == nil || !strings.Contains(err.Error(), "base-team is a base, not a team") {
		t.Fatalf("base built as team: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "teams/base-team")); statErr == nil {
		t.Fatal("base-team release folder created")
	}
}

// A child that only sets the base variables and adds its own stack files must
// package the current base inventory plus those files. Source may add files
// ahead of an immutable published release; base metadata is not distributable.
func TestBaseTeamChildBuildsCurrentBaseInventory(t *testing.T) {
	root := canonicalTemp(t)
	for name, raw := range snapshot(t, "../../teams/source/base-team") {
		put(t, filepath.Join(root, "teams/source/base-team", filepath.FromSlash(name)), raw)
	}
	own := snapshot(t, "../../teams/source/go-development")
	for name, raw := range own {
		put(t, filepath.Join(root, "teams/source/go-child", filepath.FromSlash(name)), raw)
	}
	if err := GenerateSources(t.Context(), root, false, []string{"teams/source/go-child"}); err != nil {
		t.Fatal(err)
	}
	built := snapshot(t, filepath.Join(root, "teams/go-development/v1.0.5"))
	want := snapshot(t, "../../teams/source/base-team")
	delete(want, "base.json")
	for name, raw := range own {
		if strings.Contains(name, "/") || name == "stack-profile.json" {
			want[name] = raw
		}
	}
	want["team.config"] = nil
	want["GENERATED.md"] = nil
	if len(built) != len(want) {
		t.Fatalf("built %d files, want %d", len(built), len(want))
	}
	for name := range want {
		if _, ok := built[name]; !ok {
			t.Errorf("%s missing from built release", name)
		}
	}
	if err := GenerateSources(t.Context(), root, true, []string{"teams/source/go-child"}); err != nil {
		t.Fatalf("drift check: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "teams/base-team")); err == nil {
		t.Fatal("base-team release folder created")
	}
}
