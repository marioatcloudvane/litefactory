package overview_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/overview"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type connectorOverviewReader struct {
	overview.Reader
	failRegistry, failHealth, failWaits, failAll, block bool
	activity                                            []store.OverviewActivity
	waits                                               *store.OverviewWaits
	publications                                        []store.OverviewPublication
}

var errConnectorSource = errors.New("private-source-error-marker")

func (r connectorOverviewReader) OverviewRepositories(ctx context.Context) ([]store.OverviewRepository, bool, error) {
	if r.block {
		<-ctx.Done()
		return nil, false, ctx.Err()
	}
	if r.failRegistry || r.failAll {
		return nil, false, errConnectorSource
	}
	return r.Reader.OverviewRepositories(ctx)
}
func (r connectorOverviewReader) OverviewHealth(ctx context.Context) (store.HealthSnapshot, error) {
	if r.failAll || r.failHealth {
		return store.HealthSnapshot{}, errConnectorSource
	}
	return r.Reader.OverviewHealth(ctx)
}
func (r connectorOverviewReader) ListOverviewActivity(ctx context.Context) ([]store.OverviewActivity, bool, error) {
	if r.failAll {
		return nil, false, errConnectorSource
	}
	if r.activity != nil {
		return r.activity, false, nil
	}
	return r.Reader.ListOverviewActivity(ctx)
}
func (r connectorOverviewReader) ListOverviewWaits(ctx context.Context) (store.OverviewWaits, error) {
	if r.failAll || r.failWaits {
		return store.OverviewWaits{}, errConnectorSource
	}
	if r.waits != nil {
		return *r.waits, nil
	}
	return r.Reader.ListOverviewWaits(ctx)
}
func (r connectorOverviewReader) ListOverviewPublications(ctx context.Context, from, to time.Time) (store.OverviewPublications, error) {
	if r.failAll {
		return store.OverviewPublications{}, errConnectorSource
	}
	if r.publications != nil {
		return store.OverviewPublications{Entries: r.publications}, nil
	}
	return r.Reader.ListOverviewPublications(ctx, from, to)
}

func connectorOverviewDB(t *testing.T) *store.DB {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "overview.db"))
	mustOverview(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mustOverview(t, db.Migrate(t.Context()))
	_, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	mustOverview(t, err)
	return db
}
func invokeConnectorOverview(ctx context.Context, t *testing.T, handler http.Handler, token, resource string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, resource, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"factory_overview","arguments":{}}}`)).WithContext(ctx)
	request.RemoteAddr = "127.0.0.1:1234"
	for key, value := range map[string]string{"Authorization": "Bearer " + token, "MCP-Protocol-Version": "2025-11-25", "Content-Type": "application/json", "Accept": "application/json"} {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestComposedOverviewSourceFailures(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                                 string
		registry, health, waits, all, cancel bool
		source                               string
	}{{name: "registry unknown", registry: true, source: "registry"}, {name: "health unavailable", health: true, source: "health"}, {name: "waits unavailable", waits: true, source: "waits"}, {name: "all unavailable", all: true}, {name: "cancelled", cancel: true}} {
		t.Run(tc.name, func(t *testing.T) {
			db := connectorOverviewDB(t)
			now := time.Now().UTC()
			reader := connectorOverviewReader{Reader: db, failRegistry: tc.registry, failHealth: tc.health, failWaits: tc.waits, failAll: tc.all, block: tc.cancel}
			provider := overview.New(reader, redact.New(redact.Options{Environ: []string{}}), func() time.Time { return now })
			handler, token, resource := overviewConnector(t, db, provider, now)
			ctx := t.Context()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			response := invokeConnectorOverview(ctx, t, handler, token, resource)
			if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "private-source-error-marker") {
				t.Fatalf("unsafe source failure: %d %s", response.Code, response.Body.String())
			}
			var result struct {
				Result connector.ToolResult `json:"result"`
			}
			mustOverview(t, json.Unmarshal(response.Body.Bytes(), &result))
			if tc.all || tc.cancel {
				if !result.Result.IsError || len(result.Result.StructuredContent) > 0 {
					t.Fatal("unavailable overview appeared complete")
				}
				return
			}
			var value overview.Result
			mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &value))
			if result.Result.IsError || value.Coverage.Sources[tc.source].Status != "unavailable" || value.Coverage.Sources["activity"].Status == "unavailable" {
				t.Fatalf("partial sections or coverage lost: %+v", value.Coverage)
			}
		})
	}
}

func TestComposedOverviewIndependentByteBudgets(t *testing.T) {
	t.Parallel()
	db := connectorOverviewDB(t)
	now := time.Now().UTC()
	reader := connectorOverviewReader{Reader: db}
	for i := range 51 {
		reader.activity = append(reader.activity, store.OverviewActivity{ID: fmt.Sprint(i), Repository: strings.Repeat("r", 2048), TaskRef: strings.Repeat("t", 2048), TaskURL: "https://example.test/" + strings.Repeat("a", 2000), Workflow: strings.Repeat("w", 2048), Stage: strings.Repeat("n", 2048), State: "running", StartedAt: now})
	}
	// The wait and publication remain present even when activity exhausts its own
	// byte allowance; their summary exercises a multi-byte truncation boundary.
	reader.publications = []store.OverviewPublication{{Key: "retained-publication", Summary: strings.Repeat("界", 300), Repository: "owner/repo", URL: "https://example.test/comment", ConfirmedAt: now}}
	reader.waits = &store.OverviewWaits{Entries: []store.OverviewWait{{WaitID: "retained-wait", Kind: "question", Summary: "Owner decision", Repository: "owner/repo", URL: "https://example.test/question", Since: &now, SinceProvenance: "confirmed_at"}}}
	provider := overview.New(reader, redact.New(redact.Options{Environ: []string{}}), func() time.Time { return now })
	handler, token, resource := overviewConnector(t, db, provider, now)
	response := invokeConnectorOverview(t.Context(), t, handler, token, resource)
	var result struct {
		Result connector.ToolResult `json:"result"`
	}
	mustOverview(t, json.Unmarshal(response.Body.Bytes(), &result))
	if response.Code != http.StatusOK || result.Result.IsError || response.Body.Len() > connector.MaxResponseBytes {
		t.Fatalf("bounded result unavailable: %d %s", response.Code, response.Body.String())
	}
	var value overview.Result
	mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &value))
	if !value.Activity.HasMore || !value.Activity.Truncated || len(value.Activity.Items) == 0 || len(value.Activity.Items) >= 50 {
		t.Fatal("activity byte trimming was not disclosed")
	}
	if len(value.RecentPublications.Items) != 1 {
		t.Fatal("activity displaced publication")
	}
	if len(value.OwnerWaits.Items) != 1 {
		t.Fatal("activity displaced owner wait")
	}
	summary, ok := value.RecentPublications.Items[0]["summary"].(string)
	if !ok || len(summary) > 512 || !utf8.ValidString(summary) {
		t.Fatal("UTF-8 summary exceeds its independent byte cap")
	}
	var text any
	mustOverview(t, json.Unmarshal([]byte(result.Result.Content[0].Text), &text))
	var structured any
	mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &structured))
	if !reflect.DeepEqual(text, structured) {
		t.Fatal("byte trimming produced unequal representations")
	}
}

func TestComposedOverviewLargeRequestIDFitsEnvelope(t *testing.T) {
	t.Parallel()
	db := connectorOverviewDB(t)
	now := time.Now().UTC()
	reader := connectorOverviewReader{Reader: db, waits: &store.OverviewWaits{}}
	large := strings.Repeat("quoted \"text\" \\ with <markup> ", 100)
	for i := range 51 {
		id := fmt.Sprintf("entry-%03d", i)
		reader.activity = append(reader.activity, store.OverviewActivity{ID: id, Repository: large, TaskRef: large, Workflow: large, Stage: large, State: "running", StartedAt: now})
		reader.waits.Entries = append(reader.waits.Entries, store.OverviewWait{WaitID: id, Kind: "review", Repository: large, TaskRef: large, Workflow: large, Stage: large, Summary: large, URL: "https://example.test/wait", Since: &now, SinceProvenance: "publication_confirmation"})
		reader.publications = append(reader.publications, store.OverviewPublication{Key: id, Kind: "review", Repository: large, TaskRef: large, Namespace: large, Purpose: large, Stage: large, Summary: large, URL: "https://example.test/publication", ConfirmedAt: now})
	}
	provider := overview.New(reader, redact.New(redact.Options{Environ: []string{}}), func() time.Time { return now })
	handler, token, resource := overviewConnector(t, db, provider, now)
	body := map[string]any{"jsonrpc": "2.0", "id": "", "method": "tools/call", "params": map[string]any{"name": "factory_overview", "arguments": map[string]any{}}}
	empty, err := json.Marshal(body)
	mustOverview(t, err)
	id := strings.Repeat("i", connector.MaxRequestBytes-len(empty))
	body["id"] = id
	encoded, err := json.Marshal(body)
	mustOverview(t, err)
	if len(encoded) != connector.MaxRequestBytes {
		t.Fatal("fixture does not exercise largest legal request")
	}
	request := httptest.NewRequest(http.MethodPost, resource, bytes.NewReader(encoded)).WithContext(t.Context())
	request.RemoteAddr = "127.0.0.1:1234"
	for key, value := range map[string]string{"Authorization": "Bearer " + token, "MCP-Protocol-Version": "2025-11-25", "Content-Type": "application/json", "Accept": "application/json"} {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	var result struct {
		ID     string               `json:"id"`
		Result connector.ToolResult `json:"result"`
		Error  json.RawMessage      `json:"error"`
	}
	mustOverview(t, json.Unmarshal(response.Body.Bytes(), &result))
	if response.Code != http.StatusOK || result.Result.IsError || len(result.Error) > 0 || result.ID != id || response.Body.Len() > connector.MaxResponseBytes {
		t.Fatalf("large legal id lost bounded overview: status=%d bytes=%d error=%s", response.Code, response.Body.Len(), result.Error)
	}
	var value overview.Result
	mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &value))
	for name, list := range map[string]overview.List{"activity": value.Activity, "waits": value.OwnerWaits, "publications": value.RecentPublications} {
		if len(list.Items) == 0 || !list.HasMore || !list.Truncated {
			t.Fatalf("%s lost entries or truncation under envelope pressure", name)
		}
	}
	var text, structured any
	mustOverview(t, json.Unmarshal([]byte(result.Result.Content[0].Text), &text))
	mustOverview(t, json.Unmarshal(result.Result.StructuredContent, &structured))
	if !reflect.DeepEqual(text, structured) {
		t.Fatal("large id response representations differ")
	}
}
