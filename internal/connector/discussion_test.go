package connector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDiscussionArguments(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `{"task_ref":"a","task_ref":"b","section":"status"}`, `{"task_ref":"a","section":"status","owner_id":42}`, `{"task_ref":"a","section":"status","limit":0}`, `{"task_ref":"a","section":"status","limit":51}`, `{"task_ref":"a","section":"code"}`, `{"task_ref":"a","section":"status"} {}`} {
		if _, err := parseEvidenceArguments(json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	a, err := parseEvidenceArguments(json.RawMessage(`{"task_ref":"github:o/r#1","section":"questions","limit":2}`))
	if err != nil || a.Limit != 2 {
		t.Fatal(a, err)
	}
}

func TestDiscussionSerialization(t *testing.T) {
	s := &Server{}
	for _, summary := range []string{strings.Repeat("界", 1100), strings.Repeat("\"\n\\", 400)} {
		result, err := DiscussionResult(summary, map[string]any{"escaped": "\"\n\\界"})
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Summary string `json:"summary"`
		}
		if err = json.Unmarshal(result.StructuredContent, &out); err != nil {
			t.Fatal(err)
		}
		if utf8.RuneCountInString(out.Summary) > 1000 {
			t.Fatal("summary exceeds budget")
		}
		for _, id := range []json.RawMessage{json.RawMessage(`1`), json.RawMessage(`"` + strings.Repeat("x", 64000) + `"`)} {
			w := httptest.NewRecorder()
			if !s.writeDiscussionRPC(w, id, result) {
				t.Fatal("small result refused")
			}
			if w.Body.Len() > DiscussionMaxResponseBytes || !json.Valid(w.Body.Bytes()) {
				t.Fatal("invalid full response")
			}
		}
	}
	huge := ToolResult{Content: []TextContent{{Type: "text", Text: strings.Repeat("界", 100000)}}}
	w := httptest.NewRecorder()
	if s.writeDiscussionRPC(w, json.RawMessage(`"`+strings.Repeat("x", 140000)+`"`), huge) {
		t.Fatal("oversize response accepted")
	}
	if w.Body.Len() > DiscussionMaxResponseBytes || !json.Valid(w.Body.Bytes()) || bytes.Contains(w.Body.Bytes(), []byte("界")) {
		t.Fatal("unsafe minimal refusal")
	}
	if _, err := DiscussionResult("safe", map[string]any{"large": strings.Repeat("x", 128000)}); err == nil {
		t.Fatal("combined forms not bounded")
	}
}

func TestDiscussionSerializationSuccessPartialAndError(t *testing.T) {
	s := &Server{}
	for _, state := range []string{"complete", "partial", "error"} {
		t.Run(state, func(t *testing.T) {
			evidence := map[string]any{"state": state, "items": []any{map[string]any{"id": "stable", "text": strings.Repeat("界\"\n", 2000)}}, "sources": []any{map[string]any{"revision": "bound", "observed_at": "2026-10-27T00:00:00Z"}}, "gaps": []string{}, "next_cursor": strings.Repeat("c", 1000)}
			if state != "complete" {
				evidence["gaps"] = []string{"source_unavailable"}
			}
			if state == "error" {
				evidence["items"] = []any{}
			}
			result, err := DiscussionResult(strings.Repeat("界", 1001), evidence)
			if err != nil {
				t.Fatal(err)
			}
			result.IsError = state == "error"
			if len(result.Content) != 1 || result.Content[0].Text != string(result.StructuredContent) {
				t.Fatal("output forms disagree")
			}
			var decoded map[string]any
			if err = json.Unmarshal(result.StructuredContent, &decoded); err != nil {
				t.Fatal(err)
			}
			summary, ok := decoded["summary"].(string)
			if !ok || utf8.RuneCountInString(summary) > 1000 {
				t.Fatal("summary exceeds code-point limit")
			}
			w := httptest.NewRecorder()
			if !s.writeDiscussionRPC(w, json.RawMessage(`"`+strings.Repeat("i", 60000)+`"`), result) {
				t.Fatal("bounded response refused")
			}
			if w.Body.Len() > 131072 || !json.Valid(w.Body.Bytes()) {
				t.Fatal("final UTF-8 envelope exceeds budget")
			}
		})
	}
}

func TestGuidanceCapabilityComposition(t *testing.T) {
	files := fstest.MapFS{"guidance/factory_overview.md": {Data: []byte("overview fixture")}, "guidance/task_evidence_read.md": {Data: []byte("task fixture")}}
	for _, tc := range []struct {
		scope          string
		overview, task bool
		want           string
	}{
		{Scope, true, true, "overview fixture"}, {DiscussionScope, true, true, "task fixture"}, {Scope + " " + DiscussionScope, true, true, "overview fixture\n\ntask fixture"}, {Scope, false, true, ""}, {DiscussionScope, true, false, ""},
	} {
		s := &Server{}
		if tc.overview {
			s.overview = connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil })
		}
		if tc.task {
			s.taskEvidence = fixtureEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
				return EvidencePage{}, nil
			})
		}
		got, err := loadGuidance(files, s.availableTools(store.ConnectorGrant{Scope: tc.scope, Target: &store.ConnectorGrantTarget{}}))
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q %v", tc.scope, got, err)
		}
	}
	if _, err := loadGuidance(fstest.MapFS{}, []string{"task_evidence_read"}); err == nil {
		t.Fatal("missing guidance accepted")
	}
	if _, err := loadGuidance(files, []string{"arbitrary"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

type fixtureEvidence func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error)

func (f fixtureEvidence) Evidence(ctx context.Context, target store.ConnectorGrantTarget, r EvidenceRequest) (EvidencePage, error) {
	return f(ctx, target, r)
}

func TestDiscussionCursorAndRelease(t *testing.T) {
	fixture := newConnectorFixture(t, nil)
	target := seedDiscussionTarget(t, fixture.db)
	calls := 0
	fixture.options.TaskEvidence = fixtureEvidence(func(ctx context.Context, _ store.ConnectorGrantTarget, r EvidenceRequest) (EvidencePage, error) {
		calls++
		return EvidencePage{Revision: "rev-1", Items: []map[string]any{{"id": fmt.Sprint(r.Offset)}}, HasMore: r.Offset < 2, NextOffset: r.Offset + 1}, nil
	})
	server, err := New(t.Context(), fixture.options)
	if err != nil {
		t.Fatal(err)
	}
	client := registerConnectorClient(t, server.Handler(), "client", "none")
	token, grantID := issueDiscussionTokens(t, fixture, client, target, DiscussionScope)
	read := func(args map[string]any) *httptest.ResponseRecorder {
		fixture.now = fixture.now.Add(1000000000)
		return connectorJSON(t, server.Handler(), fixture.options.Config.ResourceURL, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "task_evidence_read", "arguments": args}}, connectorBearer(token))
	}
	first := read(map[string]any{"task_ref": target.TaskRef, "section": "status"})
	var out struct {
		Result struct {
			Structured struct {
				Evidence struct {
					Cursor string `json:"next_cursor"`
				} `json:"evidence"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	connectorDecode(t, first, &out)
	cursor := out.Result.Structured.Evidence.Cursor
	if cursor == "" || calls != 1 {
		t.Fatalf("first %s", first.Body.String())
	}
	for _, args := range []map[string]any{{"task_ref": target.TaskRef, "section": "questions", "cursor": cursor}, {"task_ref": target.TaskRef, "section": "status", "cursor": cursor + "x"}, {"task_ref": "github:o/r#99", "section": "status", "cursor": cursor}} {
		response := read(args)
		if !strings.Contains(response.Body.String(), "unavailable") || calls != 1 {
			t.Fatalf("bad cursor released %s", response.Body.String())
		}
	}
	next := read(map[string]any{"task_ref": target.TaskRef, "section": "status", "cursor": cursor})
	if calls != 2 || strings.Contains(next.Body.String(), "unavailable") {
		t.Fatalf("valid cursor failed: %s", next.Body.String())
	}
	fixture.options.TaskEvidence = fixtureEvidence(func(ctx context.Context, _ store.ConnectorGrantTarget, r EvidenceRequest) (EvidencePage, error) {
		if err := fixture.db.RevokeConnectorGrant(ctx, 42, grantID, fixture.now); err != nil {
			return EvidencePage{}, err
		}
		return EvidencePage{Revision: "rev-1", Items: []map[string]any{{"secret": "must never release"}}}, nil
	})
	server.taskEvidence = fixture.options.TaskEvidence
	response := read(map[string]any{"task_ref": target.TaskRef, "section": "status"})
	if strings.Contains(response.Body.String(), "must never release") || !strings.Contains(response.Body.String(), "Access unavailable") {
		t.Fatalf("release-time revocation failed: %s", response.Body.String())
	}
}

func seedDiscussionTarget(t *testing.T, db *store.DB) store.ConnectorGrantTarget {
	t.Helper()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatal(err)
	}
	target, err := db.ResolveConnectorDiscussionTarget(t.Context(), session.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	return target
}
func issueDiscussionTokens(t *testing.T, f *connectorFixture, client connectorTestClient, target store.ConnectorGrantTarget, scope string) (string, string) {
	t.Helper()
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomCredential()
	if err != nil {
		t.Fatal(err)
	}
	b := store.ConnectorBrowser{ID: id, ClientID: client.ClientID, RedirectURI: "https://client.example/callback", Resource: f.options.Config.ResourceURL, Scope: scope, PKCEChallenge: digest("verifier"), NonceDigest: digest(id + "nonce"), UpstreamStateDigest: digest(id + "upstream"), OwnerGeneration: owner.Generation, ExpiresAt: f.now.Add(10 * time.Minute)}
	if err = f.db.CreateConnectorBrowser(t.Context(), b, 42, f.now); err != nil {
		t.Fatal(err)
	}
	if err = f.db.VerifyConnectorBrowser(t.Context(), id, b.NonceDigest, b.UpstreamStateDigest, 42, f.now); err != nil {
		t.Fatal(err)
	}
	g := store.ConnectorGrant{ID: id, ClientID: client.ClientID, Resource: b.Resource, Scope: scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: f.now, ExpiresAt: f.now.Add(24 * time.Hour), Target: &target}
	if !store.ConnectorScopeHas(scope, DiscussionScope) {
		g.Target = nil
	}
	c := store.ConnectorCode{Digest: digest(id + "code"), GrantID: id, ClientID: client.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: f.now.Add(5 * time.Minute)}
	if err = f.db.ConsentConnectorBrowser(t.Context(), id, b.NonceDigest, 42, f.now, g, c, true); err != nil {
		t.Fatal(err)
	}
	token := id + "access"
	_, err = f.db.ExchangeConnectorCode(t.Context(), store.ConnectorExchange{Digest: c.Digest, ClientID: client.ClientID, RedirectURI: b.RedirectURI, Resource: b.Resource, OwnerID: 42, PKCEChallenge: b.PKCEChallenge, Now: f.now, Pair: store.ConnectorTokenPair{AccessDigest: digest(token), RefreshDigest: digest(id + "refresh"), AccessExpiresAt: f.now.Add(15 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	return token, id
}
