package projectread

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRepositoryContinuationCannotReplayOrChangePolicy(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "reads.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	fixture := gittest.New(t)
	fixture.WriteFile("evidence.txt", strings.Repeat("ä🙂\r\n", 10000))
	var expectedDirectory strings.Builder
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("%03d-%s", i, strings.Repeat("x", 190))
		fixture.WriteFile("directory/"+name, "retained")
		expectedDirectory.WriteString(name)
		if i < 119 {
			expectedDirectory.WriteByte('\n')
		}
	}
	fixture.WriteFile("credential-to-withhold.txt", "authorized text")
	commit := fixture.Commit("Evidence")
	fixture.WriteFile("directory/later.txt", "newer source")
	fixture.Commit("Directory after pinned source")
	registered, err := db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "app", RemoteURL: "git@github.com:acme/app.git", BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(ctx)
	if err != nil {
		t.Fatal(err)
	}
	views := NewViews()
	t.Cleanup(func() { _ = views.Close() })
	reader := Reader{DB: db, Git: binary, Redactor: redact.New(redact.Options{}), Views: views}
	project := Project{ID: registered.ID, Name: "acme/app", BaseBranch: gittest.DefaultBranch, Commit: commit}
	call := func(in Request) map[string]any {
		t.Helper()
		b, _, e := reader.Call(ctx, project, "repo_read", in)
		if e != nil {
			t.Fatal(e)
		}
		var out map[string]any
		if e = json.Unmarshal(b, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	first := call(Request{Path: "evidence.txt"})
	token, ok := first["continuation"].(string)
	if !ok || token == "" {
		t.Fatalf("no continuation: %v", first)
	}
	conflicting := call(Request{Continuation: token, StartLine: 2})
	if conflicting["status"] != "invalid_request" {
		t.Fatalf("selectors accepted: %v", conflicting)
	}
	second := call(Request{Continuation: token})
	if second["status"] != "ok" {
		t.Fatalf("second page: %v", second)
	}
	replay := call(Request{Continuation: token})
	if replay["status"] != "expired_cursor" {
		t.Fatalf("replayed mutable position: %v", replay)
	}
	next, ok := second["continuation"].(string)
	if !ok || next == "" {
		t.Fatalf("second continuation missing: %v", second)
	}
	var directory strings.Builder
	request := Request{Path: "directory"}
	for pages := 0; pages < 100; pages++ {
		result := call(request)
		if result["status"] == "ok" {
			content, ok := result["content"].(string)
			if !ok {
				t.Fatalf("directory content: %v", result)
			}
			directory.WriteString(content)
		}
		if result["retained_eof"] == true {
			break
		}
		continuation, ok := result["continuation"].(string)
		if !ok || continuation == "" {
			t.Fatalf("directory continuation: %v", result)
		}
		request = Request{Continuation: continuation}
	}
	if directory.String() != expectedDirectory.String() {
		t.Fatal("directory pages omitted, duplicated, or mixed pinned entries")
	}
	selected := call(Request{Path: "evidence.txt", StartLine: 1, Lines: 1})
	if selected["selection_complete"] != true || selected["retained_eof"] != false || selected["limitation"] == nil {
		t.Fatalf("line selection incorrectly claims retained EOF: %v", selected)
	}
	locator, ok := selected["locator"].(string)
	if !ok {
		t.Fatal("selected interval lost its retained position")
	}
	if conflict := call(Request{Locator: locator, Query: "ignored selector"}); conflict["status"] != "invalid_request" {
		t.Fatalf("conflicting locator selector accepted: %v", conflict)
	}
	reader.Redactor = redact.New(redact.Options{Environ: []string{"API_TOKEN=credential-to-withhold"}})
	changed := call(Request{Continuation: next})
	if changed["status"] != "policy_changed" {
		t.Fatalf("changed policy accepted: %v", changed)
	}
	unsafe := call(Request{Path: "credential-to-withhold.txt"})
	if unsafe["status"] != "unavailable" || strings.Contains(fmt.Sprint(unsafe), "credential-to-withhold") {
		t.Fatalf("unsafe metadata disclosed: %v", unsafe)
	}
}
