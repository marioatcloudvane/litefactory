package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/webhook"
)

type opaqueSink struct {
	ids     []string
	sources []webhook.Source
}

func (s *opaqueSink) Wake(int64, int) {}
func (s *opaqueSink) WakeOpaqueSubscription(source webhook.Source, id string) {
	s.ids = append(s.ids, id)
	s.sources = append(s.sources, source)
}
func TestLinearRawSignatureTimestampAndQualifiedRouting(t *testing.T) {
	now := time.Unix(1700000000, 0)
	source := webhook.Source{RepoID: 1, ConnectionVersionID: 3, Revision: 2, Selector: "linear-one", Provider: "linear", Origin: "https://api.linear.app/graphql", Container: "team/project", ContainerID: "project", WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", Secret: []byte("signing-secret")}
	sink := &opaqueSink{}
	handler, err := webhook.NewHandler(webhook.Options{Lookup: qualifiedLookup{"linear-one": source}, Sink: sink, Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, selector, workspace, team, project, event string
		timestamp                                       int64
		valid                                           bool
		status                                          int
	}{{"issue", "linear-one", "workspace", "team", "project", "Issue", now.UnixMilli(), true, 200}, {"threaded comment", "linear-one", "workspace", "", "", "Comment", now.UnixMilli(), true, 200}, {"wrong signature", "linear-one", "workspace", "team", "project", "Issue", now.UnixMilli(), false, 401}, {"wrong workspace", "linear-one", "other", "team", "project", "Issue", now.UnixMilli(), true, 401}, {"wrong project", "linear-one", "workspace", "team", "other", "Issue", now.UnixMilli(), true, 200}, {"wrong team", "linear-one", "workspace", "other", "project", "Issue", now.UnixMilli(), true, 200}, {"stale signed timestamp", "linear-one", "workspace", "team", "project", "Issue", now.Add(-2 * time.Minute).UnixMilli(), true, 401}, {"future signed timestamp", "linear-one", "workspace", "team", "project", "Issue", now.Add(2 * time.Minute).UnixMilli(), true, 401}, {"unknown subscription", "other", "workspace", "team", "project", "Issue", now.UnixMilli(), true, 401}} {
		t.Run(tt.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"type":%q,"organizationId":%q,"webhookTimestamp":%d,"data":{"id":"issue-uuid","issueId":"thread-parent-uuid","teamId":%q,"projectId":%q}}`, tt.event, tt.workspace, tt.timestamp, tt.team, tt.project)
			req := httptest.NewRequest(http.MethodPost, webhook.Path+"?subscription="+tt.selector, strings.NewReader(body))
			req.Header.Set("Linear-Event", tt.event)
			req.Header.Set("Linear-Delivery", tt.name)
			mac := hmac.New(sha256.New, source.Secret)
			_, _ = mac.Write([]byte(body))
			signature := hex.EncodeToString(mac.Sum(nil))
			if !tt.valid {
				signature = strings.Repeat("0", 64)
			}
			req.Header.Set("Linear-Signature", signature)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			if res.Code != tt.status {
				t.Fatalf("status=%d body=%s", res.Code, res.Body)
			}
			if tt.status == 401 && res.Body.String() != "unauthorized\n" {
				t.Fatal("authentication response differs")
			}
		})
	}
	if len(sink.ids) != 2 || sink.ids[0] != "issue-uuid" || sink.ids[1] != "thread-parent-uuid" || sink.sources[0].ConnectionVersionID != 3 {
		t.Fatalf("qualified routing=%+v", sink.ids)
	}
}

func TestLinearDeliveryOutcomes(t *testing.T) {
	t.Parallel()
	now := time.Unix(1700000000, 0)
	source := webhook.Source{Selector: "linear-one", Provider: "linear", WorkspaceID: "workspace", TeamID: "team", ProjectID: "project", Secret: []byte("never-log-secret")}
	for _, kind := range []string{"queued", "duplicate", "unsupported", "unknown", "missing signature", "invalid signature", "stale", "malformed", "oversize", "workspace", "team", "project", "no headers", "method", "path"} {
		t.Run(kind, func(t *testing.T) {
			var logs strings.Builder
			sink := &opaqueSink{}
			handler, err := webhook.NewHandler(webhook.Options{Lookup: qualifiedLookup{"linear-one": source}, Sink: sink, Logger: slog.New(slog.NewJSONHandler(&logs, nil)), Now: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			selector, event, workspace, team, project, timestamp := "linear-one", "Issue", "workspace", "team", "project", now.UnixMilli()
			outcome, reason, status := "rejected", "", 401
			switch kind {
			case "queued":
				outcome, reason, status = "queued", "verification_pending", 200
			case "duplicate":
				outcome, reason, status = "duplicate", "delivery_seen", 200
			case "unsupported":
				event = "Project"
				outcome, reason, status = "ignored", "unsupported_event", 200
			case "unknown":
				selector = strings.Repeat("x", 100)
				reason = "selector_unknown_or_unavailable"
			case "missing signature", "no headers":
				reason = "missing_signature"
			case "invalid signature":
				reason = "invalid_signature"
			case "stale":
				timestamp = now.Add(-2 * time.Minute).UnixMilli()
				reason = "invalid_timestamp"
			case "malformed":
				reason = "malformed_payload"
			case "oversize":
				reason, status = "payload too large", 413
			case "workspace":
				workspace = "other"
				reason = "workspace_mismatch"
			case "team":
				team = "other"
				outcome, reason, status = "ignored", "project_or_team_mismatch", 200
			case "project":
				project = "other"
				outcome, reason, status = "ignored", "project_or_team_mismatch", 200
			case "method":
				reason, status = "method not allowed", 405
			case "path":
				reason, status = "not found", 404
			}
			body := fmt.Sprintf(`{"type":%q,"organizationId":%q,"webhookTimestamp":%d,"data":{"id":"private-payload-id","teamId":%q,"projectId":%q}}`, event, workspace, timestamp, team, project)
			if kind == "malformed" {
				body = "{"
			}
			if kind == "oversize" {
				body = strings.Repeat("x", webhook.MaxBodyBytes+1)
			}
			method, path := http.MethodPost, webhook.Path
			if kind == "method" {
				method = http.MethodGet
			}
			if kind == "path" {
				path = "/wrong"
			}
			request := func() *http.Request {
				req := httptest.NewRequest(method, path+"?subscription="+selector, strings.NewReader(body))
				mac := hmac.New(sha256.New, source.Secret)
				_, _ = mac.Write([]byte(body))
				signature := hex.EncodeToString(mac.Sum(nil))
				if kind == "invalid signature" {
					signature = strings.Repeat("0", 64)
				}
				if kind != "missing signature" && kind != "no headers" {
					req.Header.Set("Linear-Signature", signature)
				}
				if kind != "no headers" {
					req.Header.Set("Linear-Event", event)
				}
				req.Header.Set("Linear-Delivery", "delivery")
				return req
			}
			if kind == "duplicate" {
				handler.ServeHTTP(httptest.NewRecorder(), request())
				logs.Reset()
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request())
			if response.Code != status {
				t.Fatalf("status=%d want=%d", response.Code, status)
			}
			if status == 401 && response.Body.String() != "unauthorized\n" {
				t.Fatal("authentication response changed")
			}
			var row map[string]any
			if err := json.Unmarshal([]byte(logs.String()), &row); err != nil {
				t.Fatalf("expected exactly one log: %s: %v", logs.String(), err)
			}
			bounded := selector
			if len(bounded) > 64 {
				bounded = bounded[:64]
			}
			if row["level"] != "WARN" || row["subscription"] != bounded || row["outcome"] != outcome || row["reason"] != reason {
				t.Fatalf("outcome: %s", logs.String())
			}
			if strings.Contains(logs.String(), "never-log-secret") || strings.Contains(logs.String(), "private-payload-id") {
				t.Fatal("private content logged")
			}
		})
	}
}
