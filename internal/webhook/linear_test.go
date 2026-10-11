package webhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
