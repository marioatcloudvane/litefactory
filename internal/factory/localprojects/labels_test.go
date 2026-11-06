package localprojects

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type fakeGitHub struct {
	mu       sync.Mutex
	labels   []string
	creates  []string
	failOn   string
	repoID   int64
	listFail bool
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch {
	case r.URL.Path == "/user":
		fmt.Fprint(w, `{"id":2,"login":"bot"}`)
	case r.URL.Path == "/repos/o/r" && r.Method == http.MethodGet:
		fmt.Fprintf(w, `{"id":%d,"full_name":"o/r","has_issues":true}`, f.repoID)
	case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodGet:
		if f.listFail {
			http.Error(w, "boom", 500)
			return
		}
		const size = 100
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		lo, hi := (page-1)*size, min(page*size, len(f.labels))
		if lo > len(f.labels) {
			lo = len(f.labels)
		}
		if hi < len(f.labels) {
			w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?page=%d>; rel="next"`, r.Host, r.URL.Path, page+1))
		}
		var out []map[string]string
		for _, n := range f.labels[lo:hi] {
			out = append(out, map[string]string{"name": n})
		}
		_ = json.NewEncoder(w).Encode(out)
	case r.URL.Path == "/repos/o/r/labels" && r.Method == http.MethodPost:
		var body struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Name == f.failOn {
			http.Error(w, `{"message":"forbidden"}`, 403)
			return
		}
		f.creates = append(f.creates, body.Name)
		f.labels = append(f.labels, body.Name)
		w.WriteHeader(201)
		fmt.Fprint(w, `{"name":"x"}`)
	default:
		http.NotFound(w, r)
	}
}

func labelFixture(t *testing.T, fake *fakeGitHub, remoteID string) (*Service, *store.DB, store.ConnectionVersion) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	t.Setenv("LABEL_TEST_TOKEN", "test-token-value")
	home := t.TempDir()
	db, err := store.Open(t.Context(), config.NewPaths(home).DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, id := range []store.Identity{{Forge: store.ForgeGitHub, Role: store.RoleOwner, Handle: "owner", UserID: 1}, {Forge: store.ForgeGitHub, Role: store.RoleBot, Handle: "bot", UserID: 2}} {
		if _, err := db.UpsertIdentity(t.Context(), id); err != nil {
			t.Fatal(err)
		}
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(home, "repo"), Forge: store.ForgeGitHub, Owner: "o", Name: "r", RemoteURL: "git@github.com:o/r.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(home, "wt")})
	if err != nil {
		t.Fatal(err)
	}
	v := store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: ticket.GitHub, Origin: ticket.Origin(server.URL), Destination: "o/r", RemoteID: remoteID, CredentialSource: "env", CredentialEnv: "LABEL_TEST_TOKEN", Enabled: true, Provenance: "explicit"}
	saved, err := db.SetConnection(t.Context(), v, 0)
	if err != nil {
		t.Fatal(err)
	}
	service, err := Open(t.Context(), db, repo.ID, Options{LabelAgents: []string{"claude", "codex"}})
	if err != nil {
		t.Fatal(err)
	}
	return service, db, saved
}

func revision(t *testing.T, db *store.DB, repoID int64) int64 {
	t.Helper()
	v, err := db.CurrentConnection(t.Context(), repoID, store.TicketConnection)
	if err != nil {
		t.Fatal(err)
	}
	return v.Revision
}

// The main path: a wanted label on page two is found, only the truly missing
// labels are created, and a rerun writes nothing and keeps the revision.
func TestLabelSetupMainPathAcrossPages(t *testing.T) {
	fake := &fakeGitHub{repoID: 77}
	for i := 0; i < 100; i++ {
		fake.labels = append(fake.labels, fmt.Sprintf("other-%d", i))
	}
	fake.labels = append(fake.labels, "mode: plan", "Coding Agent: Claude", "Bug")
	service, db, v := labelFixture(t, fake, "77")
	before := revision(t, db, v.RepoID)

	setup, err := service.PlanLabelSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(setup.Found) != 103 || len(fake.creates) != 0 {
		t.Fatalf("dry run: found %d creates %v", len(setup.Found), fake.creates)
	}
	if len(setup.Plan.Warnings) != 2 {
		t.Fatalf("want warnings for mode: plan and Bug, got %+v", setup.Plan.Warnings)
	}
	res := setup.Apply(t.Context())
	want := []string{"Mode: Research", "Mode: Implement", "Mode: Validate", "Mode: Spike", "Coding Agent: Codex"}
	if res.Failed != "" || strings.Join(res.Created, ",") != strings.Join(want, ",") || strings.Join(fake.creates, ",") != strings.Join(want, ",") {
		t.Fatalf("apply: %+v creates %v", res, fake.creates)
	}
	again, err := service.PlanLabelSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Plan.Create) != 0 || len(fake.creates) != len(want) || revision(t, db, v.RepoID) != before {
		t.Fatalf("rerun: %+v creates %v", again.Plan, fake.creates)
	}
}

func TestLabelSetupCreateFailureReportsAndRerunCreatesRest(t *testing.T) {
	fake := &fakeGitHub{repoID: 77, failOn: "Mode: Implement"}
	service, db, v := labelFixture(t, fake, "77")
	before := revision(t, db, v.RepoID)
	setup, err := service.PlanLabelSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	res := setup.Apply(t.Context())
	if res.Failed != "Mode: Implement" || res.FailedWhy == "" || len(res.Created) != 2 || len(res.NotTried) != 5 {
		t.Fatalf("report: %+v", res)
	}
	if revision(t, db, v.RepoID) != before {
		t.Fatal("revision changed")
	}
	fake.failOn = ""
	setup, err = service.PlanLabelSetup(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(setup.Plan.Create) != 6 {
		t.Fatalf("rerun should create only the rest: %+v", setup.Plan.Create)
	}
}

func TestLabelSetupRefusalsMakeNoWrites(t *testing.T) {
	t.Run("identity mismatch", func(t *testing.T) {
		fake := &fakeGitHub{repoID: 99}
		service, _, _ := labelFixture(t, fake, "77")
		if _, err := service.PlanLabelSetup(context.Background()); err == nil || len(fake.creates) != 0 {
			t.Fatalf("err %v creates %v", err, fake.creates)
		}
	})
	t.Run("incomplete read", func(t *testing.T) {
		fake := &fakeGitHub{repoID: 77, listFail: true}
		service, _, _ := labelFixture(t, fake, "77")
		if _, err := service.PlanLabelSetup(context.Background()); err == nil || len(fake.creates) != 0 {
			t.Fatalf("err %v creates %v", err, fake.creates)
		}
	})
}
