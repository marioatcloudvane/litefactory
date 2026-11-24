package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// A backlog that takes longer to reread than the discovery phase allows must
// not fail discovery: the drain stops at its budget, keeps the rest pending and
// leaves discovery a live context, so the tick goes on to dispatch (#1528).
func TestRecoveryDrainStopsAtTimeBudget(t *testing.T) {
	f := newDiscoveryHTTPFixture(t)
	var origin string
	var rereads atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4900")
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(24*time.Hour).Unix(), 10))
		switch {
		case r.URL.Path == "/user":
			_, _ = w.Write([]byte(`{"id":99,"login":"bot"}`))
		case r.URL.Path == "/repos/acme/code":
			_, _ = w.Write([]byte(`{"id":2,"full_name":"acme/code"}`))
		case r.URL.Path == "/repos/acme/admission":
			_, _ = w.Write([]byte(`{"id":1,"full_name":"acme/admission"}`))
		case strings.HasPrefix(r.URL.Path, "/repos/acme/admission/issues/"):
			n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/admission/issues/"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			rereads.Add(1)
			select {
			case <-time.After(150 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": n * 10, "number": n, "title": "Pending", "state": "open", "url": origin + r.URL.Path})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	origin = server.URL
	v, _ := subscriptionCacheFixture(t, f, "admission", origin)
	v.RemoteID, v.CredentialSource, v.CredentialEnv = "1", "env", "TEST_ADMISSION_TOKEN"
	var err error
	v, err = f.d.db.SetConnection(t.Context(), v, v.Revision)
	if err != nil {
		t.Fatal(err)
	}
	code := v
	code.ID, code.Revision, code.Role, code.Destination, code.RemoteID = 0, 0, store.CodeConnection, "acme/code", "2"
	if _, err = f.d.db.SetConnection(t.Context(), code, 0); err != nil {
		t.Fatal(err)
	}
	f.d.connections = projectconnection.NewFactory(projectconnection.Options{ExpectedActorID: 99, Logger: f.d.log, Resolver: secrets.Resolver{LookupEnv: func(string) (string, bool) { return "offline-token", true }}})
	repo, err := f.d.db.GetRepo(t.Context(), v.RepoID)
	if err != nil {
		t.Fatal(err)
	}
	const rows = 12
	for n := 1; n <= rows; n++ {
		if err := f.d.db.HandoffTicketObservation(t.Context(), store.PendingTicketObservation{RepoID: v.RepoID, ConnectionVersionID: v.ID, Provider: v.Provider, Origin: v.Origin, Container: v.Destination, ContainerID: "1", TicketID: strconv.Itoa(n * 10), TicketKey: strconv.Itoa(n), ObservedAt: f.now}); err != nil {
			t.Fatal(err)
		}
	}
	// The whole phase fits only a few rereads; all twelve would take ~1.8 s.
	ctx, cancel := context.WithTimeout(f.d.discoveryContext(t.Context()), 800*time.Millisecond)
	defer cancel()
	q := &repoQueue{repo: repo, forgeRepo: githubRepo(v.Destination), log: f.d.log}
	err = f.d.drainTicketObservationsWithinBudget(ctx, map[int64]*repoQueue{repo.ID: q}, map[int64]*candidate{})
	if err != nil {
		t.Fatalf("drain over budget failed discovery: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("drain used up the discovery deadline: %v", ctx.Err())
	}
	if got := rereads.Load(); got == 0 || got >= rows {
		t.Fatalf("rereads = %d, want some but not all %d", got, rows)
	}
	pending, err := f.d.db.ListPendingTicketObservations(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != rows {
		t.Fatalf("pending = %d, want every open row kept for a later tick", len(pending))
	}
	if !strings.Contains(f.logs.String(), "drain stopped at its time budget") {
		t.Fatalf("budget stop not logged: %s", f.logs.String())
	}
}
