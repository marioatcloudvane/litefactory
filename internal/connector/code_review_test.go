package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type codeProtocolProvider func(context.Context, store.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error)

func (f codeProtocolProvider) Evidence(ctx context.Context, t store.ConnectorCodeTarget, r CodeEvidenceRequest) (CodeEvidencePage, error) {
	return f(ctx, t, r)
}

func seedCodeProtocolTarget(t *testing.T, db *store.DB) store.ConnectorCodeTarget {
	t.Helper()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: filepath.Join(t.TempDir(), "repo"), Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	code := store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: "acme/api", CredentialSource: "env", CredentialEnv: "TEST_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: repo.WorktreeRoot}
	code, err = db.SetConnection(t.Context(), code, 0)
	if err != nil {
		t.Fatal(err)
	}
	tickets := code
	tickets.ID = 0
	tickets.Revision = 0
	tickets.Role = store.TicketConnection
	tickets, err = db.SetConnection(t.Context(), tickets, 0)
	if err != nil {
		t.Fatal(err)
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: "7"}, Locator: ticket.Locator{Provider: ticket.GitHub, Origin: "https://api.github.com", Container: "acme/api", Key: "7"}}
	_, err = db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := db.AdmitSession(t.Context(), repo, ref, code.Revision, tickets.Revision, 42, "owner", "github:acme/api#7")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.FreezeSessionSlug(t.Context(), s.ID, "review", "lf/7-review")
	if err != nil {
		t.Fatal(err)
	}
	target, err := db.ResolveConnectorCodeTarget(t.Context(), s.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func issueCodeProtocolTokens(t *testing.T, f *connectorFixture, client connectorTestClient, target store.ConnectorCodeTarget, expires time.Time) (string, string) {
	t.Helper()
	owner, err := f.db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	id, err := randomCredential()
	if err != nil {
		t.Fatal(err)
	}
	scope := DiscussionScope + " " + CodeReviewScope
	b := store.ConnectorBrowser{ID: id, ClientID: client.ClientID, RedirectURI: "https://client.example/callback", Resource: f.options.Config.ResourceURL, Scope: scope, PKCEChallenge: digest("verifier"), NonceDigest: digest(id + "nonce"), UpstreamStateDigest: digest(id + "upstream"), OwnerGeneration: owner.Generation, ExpiresAt: f.now.Add(10 * time.Minute)}
	if err = f.db.CreateConnectorBrowser(t.Context(), b, 42, f.now); err != nil {
		t.Fatal(err)
	}
	if err = f.db.VerifyConnectorBrowser(t.Context(), id, b.NonceDigest, b.UpstreamStateDigest, 42, f.now); err != nil {
		t.Fatal(err)
	}
	discussion, err := f.db.ResolveConnectorDiscussionTarget(t.Context(), target.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	g := store.ConnectorGrant{ID: id, ClientID: client.ClientID, Resource: b.Resource, Scope: scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: f.now, ExpiresAt: expires, Target: &discussion, CodeTarget: &target}
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

func TestCodeReviewStrictArguments(t *testing.T) {
	for _, raw := range []string{`{}`, `null`, `[]`, `{"task_ref":"a","task_ref":"b"}`, `{"task_ref":null}`, `{"task_ref":"a","owner_id":42}`, `{"task_ref":"a","path":"../x"}`, `{"task_ref":"a","path":"/x"}`, `{"task_ref":"a","path":":(glob)*"}`, `{"task_ref":"a","path":"a\\b"}`, `{"task_ref":"a","path":"a\tb"}`, `{"task_ref":"a","branch":"HEAD"}`, `{"task_ref":"a","limit":0}`, `{"task_ref":"a","limit":51}`, `{"task_ref":"a","start_line":0}`, `{"task_ref":"a","lines":401}`, `{"task_ref":"a","cursor":"x"}`, `{"task_ref":"a"} {}`, `{"cursor":""}`, `{"cursor":null}`} {
		if _, err := parseCodeArguments("repo_read", json.RawMessage(raw)); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, tc := range []struct{ name, raw string }{{"repo_grep", `{"task_ref":"a"}`}, {"repo_read", `{"task_ref":"a","pattern":"x"}`}, {"repo_diff", `{"task_ref":"a","mode":"commit","commit":"HEAD"}`}, {"repo_diff", `{"task_ref":"a","mode":"base_to_task","commit":"` + strings.Repeat("a", 40) + `"}`}, {"standards_read", `{"task_ref":"a","categories":["x","x"]}`}, {"standards_role_counts", `{"task_ref":"a","role":"worker"}`}} {
		if _, err := parseCodeArguments(tc.name, json.RawMessage(tc.raw)); err == nil {
			t.Errorf("accepted %s %s", tc.name, tc.raw)
		}
	}
	a, err := parseCodeArguments("repo_read", json.RawMessage(`{"task_ref":"a","path":"literal[1].txt"}`))
	if err != nil || a.Branch != "task" || a.Limit != 50 {
		t.Fatalf("valid args %+v %v", a, err)
	}
	a, err = parseCodeArguments("repo_read", json.RawMessage(`{"cursor":"opaque"}`))
	if err != nil || a.Cursor != "opaque" {
		t.Fatal(a, err)
	}
}

type codeProtocolOutput struct {
	Evidence struct {
		Cursor string `json:"next_cursor"`
		View   string `json:"view_token"`
	} `json:"evidence"`
}

func codeProtocolDecode(t *testing.T, r ToolResult) codeProtocolOutput {
	t.Helper()
	var o codeProtocolOutput
	if err := json.Unmarshal(r.StructuredContent, &o); err != nil {
		t.Fatal(err)
	}
	return o
}

func TestCodeReviewTokenBindingAndContinuation(t *testing.T) {
	f := newConnectorFixture(t, nil)
	target := seedCodeProtocolTarget(t, f.db)
	calls := 0
	var requests []CodeEvidenceRequest
	f.options.CodeEvidence = codeProtocolProvider(func(_ context.Context, _ store.ConnectorCodeTarget, r CodeEvidenceRequest) (CodeEvidencePage, error) {
		calls++
		requests = append(requests, r)
		pin := r.Pin
		if pin.ObservedAt.IsZero() {
			pin = CodeEvidencePin{TaskCommit: strings.Repeat("a", 40), BaseCommit: strings.Repeat("b", 40), ObservedAt: f.now, Policy: "test-v1"}
		}
		return CodeEvidencePage{Pin: pin, Items: []map[string]any{{"offset": r.Offset}}, HasMore: r.Offset == 0, NextOffset: r.Offset + 1}, nil
	})
	s, err := New(t.Context(), f.options)
	if err != nil {
		t.Fatal(err)
	}
	client := registerConnectorClient(t, s.Handler(), "code", "none")
	credential, _ := issueCodeProtocolTokens(t, f, client, target, f.now.Add(time.Hour))
	other, _ := issueCodeProtocolTokens(t, f, client, target, f.now.Add(time.Hour))
	a := codeArguments{TaskRef: target.TaskRef, Branch: "base", Path: "literal.txt", Limit: 2}
	result, err := s.readCode(t.Context(), credential, "repo_read", a)
	if err != nil {
		t.Fatal(err)
	}
	first := codeProtocolDecode(t, result)
	if first.Evidence.Cursor == "" || first.Evidence.View == "" {
		t.Fatal("missing tokens")
	}
	for _, tc := range []struct {
		cred, name string
		a          codeArguments
	}{{credential, "repo_read", codeArguments{Cursor: first.Evidence.Cursor + "x"}}, {other, "repo_read", codeArguments{Cursor: first.Evidence.Cursor}}, {credential, "repo_log", codeArguments{Cursor: first.Evidence.Cursor}}, {credential, "repo_read", codeArguments{TaskRef: target.TaskRef, Limit: 2, ViewToken: first.Evidence.Cursor}}, {credential, "repo_read", codeArguments{TaskRef: "github:acme/api#8", Limit: 2, ViewToken: first.Evidence.View}}} {
		if _, err = s.readCode(t.Context(), tc.cred, tc.name, tc.a); err == nil {
			t.Error("invalid token accepted")
		}
	}
	if calls != 1 {
		t.Fatalf("invalid requests acquired evidence: %d", calls)
	}
	next, err := s.readCode(t.Context(), credential, "repo_read", codeArguments{Cursor: first.Evidence.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	if codeProtocolDecode(t, next).Evidence.Cursor != "" || requests[1].Offset != 1 || requests[1].Path != a.Path || requests[1].Branch != a.Branch || requests[1].Pin.TaskCommit != strings.Repeat("a", 40) {
		t.Fatalf("continuation %+v", requests)
	}
	// A view can switch evidence operations while preserving its authorized pin.
	_, err = s.readCode(t.Context(), credential, "repo_log", codeArguments{TaskRef: target.TaskRef, Branch: "task", ViewToken: first.Evidence.View, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if requests[2].Pin != requests[1].Pin {
		t.Fatal("view pin changed")
	}
	token, err := s.openCodeToken(first.Evidence.View)
	if err != nil {
		t.Fatal(err)
	}
	token.Target.RepositoryRef = "github:other/repo"
	changed, err := s.sealCodeToken(token)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.readCode(t.Context(), credential, "repo_read", codeArguments{TaskRef: target.TaskRef, ViewToken: changed, Limit: 2}); err == nil {
		t.Fatal("changed target accepted")
	}
	f.now = f.now.Add(15 * time.Minute)
	if _, err = s.openCodeToken(first.Evidence.View); err == nil {
		t.Fatal("15 minute view stayed live")
	}
}

func TestCodeReviewGrantExpiryAndReleaseLifecycle(t *testing.T) {
	for _, scenario := range []string{"grant expiry", "revoked before", "revoked after", "owner before", "owner after", "pin changed", "nonadvancing", "oversized"} {
		t.Run(scenario, func(t *testing.T) {
			f := newConnectorFixture(t, nil)
			target := seedCodeProtocolTarget(t, f.db)
			calls := 0
			grantID := ""
			invalidate := func(ctx context.Context) error {
				if strings.HasPrefix(scenario, "owner") {
					_, err := f.db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 99, Handle: "replacement", Source: store.SourceGH})
					return err
				}
				return f.db.RevokeConnectorGrant(ctx, 42, grantID, f.now)
			}
			f.options.CodeEvidence = codeProtocolProvider(func(ctx context.Context, _ store.ConnectorCodeTarget, r CodeEvidenceRequest) (CodeEvidencePage, error) {
				calls++
				if strings.HasSuffix(scenario, "after") {
					if err := invalidate(ctx); err != nil {
						return CodeEvidencePage{}, err
					}
				}
				pin := r.Pin
				if pin.ObservedAt.IsZero() {
					pin = CodeEvidencePin{ObservedAt: f.now, Policy: "test"}
				}
				p := CodeEvidencePage{Pin: pin, Items: []map[string]any{{"private": "never release"}}}
				switch scenario {
				case "pin changed":
					p.Pin.Policy = "changed"
				case "nonadvancing":
					p.HasMore = true
				case "oversized":
					p.Items = []map[string]any{{"text": strings.Repeat("界", 32000)}}
				}
				return p, nil
			})
			s, err := New(t.Context(), f.options)
			if err != nil {
				t.Fatal(err)
			}
			client := registerConnectorClient(t, s.Handler(), "code", "none")
			credential, id := issueCodeProtocolTokens(t, f, client, target, f.now.Add(time.Minute))
			grantID = id
			a := codeArguments{TaskRef: target.TaskRef, Branch: "task", Limit: 50}
			if scenario == "grant expiry" {
				first, err := s.readCode(t.Context(), credential, "repo_read", a)
				if err != nil {
					t.Fatal(err)
				}
				token, err := s.openCodeToken(codeProtocolDecode(t, first).Evidence.View)
				if err != nil {
					t.Fatal(err)
				}
				if token.Expires != f.now.Add(time.Minute).Unix() {
					t.Fatal("view exceeded grant")
				}
				f.now = f.now.Add(time.Minute)
			}
			if strings.HasSuffix(scenario, "before") {
				if err = invalidate(t.Context()); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "pin changed" {
				token, err := s.sealCodeToken(codeToken{Grant: id, Target: target, Expires: f.now.Add(time.Minute).Unix(), Pin: CodeEvidencePin{ObservedAt: f.now, Policy: "initial"}})
				if err != nil {
					t.Fatal(err)
				}
				a.ViewToken = token
			}
			if _, err = s.readCode(t.Context(), credential, "repo_read", a); err == nil {
				t.Fatalf("%s released evidence", scenario)
			}
			if strings.HasSuffix(scenario, "before") && calls != 0 {
				t.Fatal("acquired before lifecycle check")
			}
		})
	}
}

func TestCodeReviewGuidanceCapabilities(t *testing.T) {
	provider := codeProtocolProvider(func(context.Context, store.ConnectorCodeTarget, CodeEvidenceRequest) (CodeEvidencePage, error) {
		return CodeEvidencePage{}, nil
	})
	files := fstest.MapFS{}
	for _, name := range append([]string{"factory_overview", "task_evidence_read", "usage_read"}, codeToolNames...) {
		files["guidance/"+name+".md"] = &fstest.MapFile{Data: []byte(fmt.Sprintf("fixture for %s", name))}
	}
	target := &store.ConnectorCodeTarget{TaskRef: "task", SessionID: 1, RepoID: 1}
	discussion := &store.ConnectorGrantTarget{TaskRef: "task", SessionID: 1, RepoID: 1}
	for _, tc := range []struct {
		scope              string
		installed, binding bool
		count              int
	}{{DiscussionScope + " " + CodeReviewScope, true, true, 6}, {DiscussionScope + " " + CodeReviewScope, false, true, 0}, {DiscussionScope, true, true, 0}, {CodeReviewScope, true, true, 0}, {Scope, true, true, 0}, {DiscussionScope + " " + CodeReviewScope, true, false, 0}} {
		s := &Server{}
		if tc.installed {
			s.codeEvidence = provider
		}
		g := store.ConnectorGrant{Scope: tc.scope, Target: discussion}
		if tc.binding {
			g.CodeTarget = target
		}
		names := s.availableTools(g)
		if len(names) != tc.count {
			t.Fatalf("scope %s capabilities %v", tc.scope, names)
		}
		if _, err := loadGuidance(files, names); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{codeEvidence: provider, overview: connectorFixtureOverview(func(context.Context, json.RawMessage) (ToolResult, error) { return ToolResult{}, nil }), taskEvidence: fixtureEvidence(func(context.Context, store.ConnectorGrantTarget, EvidenceRequest) (EvidencePage, error) {
		return EvidencePage{}, nil
	})}
	names := s.availableTools(store.ConnectorGrant{Scope: Scope + " " + DiscussionScope + " " + CodeReviewScope, Target: discussion, CodeTarget: target})
	if !reflect.DeepEqual(names, append([]string{"factory_overview", "task_evidence_read"}, codeToolNames...)) {
		t.Fatalf("combined guidance %v", names)
	}
	delete(files, "guidance/repo_read.md")
	if _, err := loadGuidance(files, names); err == nil {
		t.Fatal("missing installed guidance accepted")
	}
}
