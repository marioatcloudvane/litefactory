package doctor

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/cimonitor"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestCIAccessDoctorWaitAndFactoryScope(t *testing.T) {
	s, results := readResultsFixture(t)
	at := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	var requests atomic.Int64
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Error("diagnostic attempted a write")
		}
		w.Header().Set("Retry-After", "7200")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limit exceeded"}`))
	}))
	defer api.Close()
	client, err := github.New(github.Options{Token: secrets.NewToken("offline-ci"), APIBase: api.URL, Now: func() time.Time { return at }})
	if err != nil {
		t.Fatal(err)
	}
	target := config.CITarget{Repository: "acme/api", Branch: "main", Workflow: ".github/workflows/check.yml"}
	observer := &cimonitor.Observer{Reader: client, Target: target, Origin: api.URL, Gate: &cimonitor.Gate{Store: s.DB, Key: "ci:" + api.URL, Now: func() time.Time { return at }}, Now: func() time.Time { return at }}
	results.ciProbe, results.ciRepository = observer, "other/repository"
	if _, err := readTool(t, results, "ci_access", `{}`); err == nil || requests.Load() != 0 {
		t.Fatal("foreign factory context admitted")
	}
	results.ciRepository = target.Repository
	if _, err := readTool(t, results, "ci_access", `{"repository":"other/repository"}`); err == nil || requests.Load() != 0 {
		t.Fatal("arbitrary diagnostic target admitted")
	}
	before, err := s.DB.ReadAlerts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := readTool(t, results, "ci_access", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	var probe cimonitor.ProbeResult
	if err = json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.Ready || requests.Load() != 1 {
		t.Fatalf("probe=%s calls=%d", raw, requests.Load())
	}
	after, err := s.DB.ReadAlerts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostic modified alert or recovery state")
	}
	// Independent store/observer models a standalone doctor restart. Its durable
	// gate must refuse a second request for the complete server-provided wait.
	reopened, err := store.Open(t.Context(), s.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	observer.Gate = &cimonitor.Gate{Store: reopened, Key: "ci:" + api.URL, Now: func() time.Time { return at.Add(61 * time.Minute) }}
	raw, err = readTool(t, results, "ci_access", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if probe.NextRetry == nil || !probe.NextRetry.Equal(at.Add(2*time.Hour)) || requests.Load() != 1 {
		t.Fatalf("lost server wait: %s calls=%d", raw, requests.Load())
	}
	unchanged, err := s.DB.ReadAlerts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, unchanged) {
		t.Fatal("deferred probe modified incidents")
	}
	observer.Engine = &alerts.Engine{DB: reopened, Settings: config.DefaultAlerts()}
	if err = observer.Poll(t.Context(), at.Add(61*time.Minute)); err == nil || requests.Load() != 1 {
		t.Fatal("scheduled polling bypassed doctor wait")
	}
	// Only an available, factory-scoped probe appears in the catalog.
	raw, err = results.handle(t.Context(), submission.Request{Method: "tools/list"})
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err = json.Unmarshal(raw, &catalog); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tool := range catalog.Tools {
		found = found || tool.Name == "ci_access"
	}
	if !found {
		t.Fatal("factory probe absent")
	}
	results.repairID = "repair"
	if _, err = readTool(t, results, "ci_access", `{}`); err == nil {
		t.Fatal("repair admitted diagnostic tool")
	}
}

// Ensure the production seam stays a read-only diagnostic capability.
var _ CIProber = (*cimonitor.Observer)(nil)

func TestCIAccessRejectsUnverifiedQualifiedContext(t *testing.T) {
	for _, task := range []string{"github:acme/api#406~0123456789abcdef", "linear:acme/api#406", "github:other/repo#406"} {
		if ciFactoryContext(task, "acme/api") {
			t.Fatalf("unverified factory context admitted: %s", task)
		}
	}
	if !ciFactoryContext("github:acme/api#406", "acme/api") {
		t.Fatal("configured context refused")
	}
}
