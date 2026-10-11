package daemon_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestCodeDeliveryKeepsOriginalDestinationAndBlocksIdentityDrift(t *testing.T) {
	var remoteID atomic.Int64
	remoteID.Store(91)
	var headReads atomic.Int64
	head := strings.Repeat("a", 40)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_, _ = w.Write([]byte(`{"id":7,"login":"worker"}`))
		case "/repos/owner/original":
			_, _ = fmt.Fprintf(w, `{"id":%d,"full_name":"owner/original","has_issues":true}`, remoteID.Load())
		case "/repos/owner/original/issues/9":
			_, _ = fmt.Fprintf(w, `{"id":909,"number":9,"url":"http://%s/repos/owner/original/issues/9","title":"Original","state":"open"}`, r.Host)
		case "/repos/owner/original/branches/feature":
			headReads.Add(1)
			_, _ = fmt.Fprintf(w, `{"name":"feature","commit":{"sha":"%s"}}`, head)
		default:
			t.Errorf("unexpected target %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: "/original", Forge: "github", Owner: "owner", Name: "original", RemoteURL: "git@github.com:owner/original.git"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 9)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Defaults()
	cfg.Providers.GitHub.APIBase = server.URL
	cfg.Providers.GitHub.TokenEnv = "TOKEN"
	cfg.Providers.GitHub.TokenKeychain = config.Keychain{}
	factory := projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 7, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "opaque-code-token", true }}})
	if err := projectconnection.Initialize(t.Context(), db, cfg, factory); err != nil {
		t.Fatal(err)
	}
	code, err := db.CurrentConnection(t.Context(), repo.ID, store.CodeConnection)
	if err != nil {
		t.Fatal(err)
	}
	code.Destination = "owner/replacement"
	code.Provenance = "explicit"
	code.Enabled = false
	if _, err := db.SetConnection(t.Context(), code, code.Revision); err != nil {
		t.Fatal(err)
	}
	gateway := daemon.NewProjectCodeDelivery(db, factory).ForSession(session.ID)
	got, err := gateway.GetDeliveryHead(t.Context(), github.Repo{Owner: "owner", Name: "original"}, "feature")
	if err != nil || got != head {
		t.Fatalf("original destination %q %v", got, err)
	}
	if _, err := gateway.GetDeliveryHead(t.Context(), github.Repo{Owner: "owner", Name: "replacement"}, "feature"); err == nil {
		t.Fatal("replacement redirected saved delivery")
	}
	remoteID.Store(92)
	if _, err := gateway.GetDeliveryHead(t.Context(), github.Repo{Owner: "owner", Name: "original"}, "feature"); err == nil {
		t.Fatal("changed original identity admitted")
	}
	if headReads.Load() != 1 {
		t.Fatalf("blocked identity performed branch access: %d", headReads.Load())
	}
}
