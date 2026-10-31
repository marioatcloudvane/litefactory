package stagedecision

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

type decisionFixture struct {
	db        *store.DB
	service   *Service
	actions   *owneraction.Service
	principal owneraction.Principal
	now       time.Time
	session   store.Session
	instance  store.GraphInstance
	repo      store.Repo
}

func decisionCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type decisionTestPublisher struct{}

func (decisionTestPublisher) Publish(context.Context, store.DiscussionConclusion) (string, error) {
	return "", nil
}
func (decisionTestPublisher) Recover(context.Context, store.DiscussionConclusion) (string, bool, error) {
	return "", false, nil
}

type decisionTestDelivery struct{}

func (decisionTestDelivery) Send(context.Context, owneraction.Proposal) (string, error) {
	return "approval-message", nil
}

type decisionFallback struct{}

func (decisionFallback) Resolve(context.Context, owneraction.Principal, owneraction.Invocation, time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{}, owneraction.ErrUnavailable
}
func (decisionFallback) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return owneraction.ErrUnavailable
}
func (decisionFallback) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	return owneraction.Result{}, owneraction.ErrUnavailable
}
func (decisionFallback) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, bool, error) {
	return owneraction.Result{}, false, owneraction.ErrUnavailable
}

func newDecisionFixture(t *testing.T) *decisionFixture {
	t.Helper()
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "decisions.db"))
	decisionCheck(t, err)
	t.Cleanup(func() { decisionCheck(t, db.Close()) })
	decisionCheck(t, db.Migrate(ctx))
	f := &decisionFixture{db: db, now: time.Date(2026, 10, 31, 22, 0, 0, 0, time.UTC)}
	_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH})
	decisionCheck(t, err)
	owner, err := db.ReconcileConnectorOwner(ctx, 42, f.now)
	decisionCheck(t, err)
	f.repo, err = db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	decisionCheck(t, err)
	f.session, err = db.GetOrCreateSession(ctx, f.repo, 7)
	decisionCheck(t, err)
	_, err = db.AdmitResearchPath(ctx, f.session.ID, "graph")
	decisionCheck(t, err)
	graph := `digraph research {
 graph [team="test",mode="research",profile_version="1",start="shape"];
 shape [lf_action="lf_agent",lf_agent_markdown="shaping",lf_outcomes="success"];
 review [lf_action="lf_ask",lf_source="shape",lf_outcomes="published"];
 settled [lf_action="lf_if",lf_outcomes="true,false",lf_field="response",lf_condition="approved",lf_source="shape"];
 final [lf_action="lf_comment",lf_kind="final",lf_source="shape",lf_outcomes="published"];
 waiting [lf_terminal="true"];
 shape -> review [lf_on="success"];
 review -> settled [lf_on="published"];
 settled -> final [lf_on="true"];
 settled -> waiting [lf_on="false"];
 final -> waiting [lf_on="published"];
 }`
	files, err := json.Marshal(map[string][]byte{"research.dot": []byte(graph), "shaping.md": []byte("Fixture shaping role")})
	decisionCheck(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), TeamSnapshotDigest: strings.Repeat("b", 64)})
	decisionCheck(t, err)
	launch, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "research", SessionID: f.session.ID, TriggerID: 20, DefinitionID: definition.ID, Start: "shape", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: files})
	decisionCheck(t, err)
	f.instance = launch.Instance
	segment, err := db.GetOrCreateSegment(ctx, f.session.ID, 1, "research", store.AgentClaude)
	decisionCheck(t, err)
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "shape.log" }})
	decisionCheck(t, err)
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, LaunchID: launch.LaunchID, Node: "shape", RunID: run.ID, StartedAt: f.now})
	decisionCheck(t, err)
	decisionCheck(t, db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.now, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"done"}`)}}}))
	decisionCheck(t, db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "review", InstanceID: f.instance.ID, SourceAttemptID: attempt.ID, Node: "review", Kind: "review", Candidate: []byte(`{"outcome":"completed","comment":"Current shaping proposal"}`)}))
	_, err = db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "review", SessionID: f.session.ID, RunID: run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: f.session.TaskRef, TriggerNamespace: "issue-comment", Purpose: "review", BotUserID: 99, Body: "Current shaping proposal", Marker: "review"})
	decisionCheck(t, err)
	_, err = db.MarkReportDeliverySent(ctx, "review")
	decisionCheck(t, err)
	_, err = db.ConfirmReportDelivery(ctx, "review", 101)
	decisionCheck(t, err)
	_, err = db.CompleteReportDelivery(ctx, "review")
	decisionCheck(t, err)
	f.principal = f.grant(t, "origin", owner.Generation)
	f.actions = owneraction.New(db, decisionFallback{}, func() time.Time { return f.now })
	f.actions.SetDiscord("100", []string{"200"})
	f.actions.SetDelivery(decisionTestDelivery{})
	f.service, err = New(Options{DB: db, Now: func() time.Time { return f.now }, Protect: func(_ context.Context, v string) (string, error) { return v, nil }, Publisher: decisionTestPublisher{}})
	decisionCheck(t, err)
	f.service.BindActions(f.actions)
	return f
}
func (f *decisionFixture) grant(t *testing.T, id string, generation int64) owneraction.Principal {
	t.Helper()
	ctx := t.Context()
	at := f.now
	client := store.ConnectorClient{ID: "client-" + id, Name: "Test client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	decisionCheck(t, f.db.RegisterConnectorClient(ctx, client, at))
	b := store.ConnectorBrowser{ID: "browser-" + id, ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: "https://factory.test/mcp", Scope: "repository:actions", PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: generation, ExpiresAt: at.Add(10 * time.Minute)}
	decisionCheck(t, f.db.CreateConnectorBrowser(ctx, b, 42, at))
	decisionCheck(t, f.db.VerifyConnectorBrowser(ctx, b.ID, "nonce", "upstream", 42, at))
	g := store.ConnectorGrant{ID: id, ClientID: client.ID, Resource: b.Resource, Scope: b.Scope, OwnerID: 42, OwnerGeneration: generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour), ActionCoverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: f.repo.ID, RepositoryRef: "github:acme/app"}}}}
	c := store.ConnectorCode{Digest: "code-" + id, GrantID: id, ClientID: client.ID, RedirectURI: b.RedirectURI, Resource: b.Resource, Scope: b.Scope, PKCEChallenge: b.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	decisionCheck(t, f.db.ConsentConnectorBrowser(ctx, b.ID, "nonce", 42, at, g, c, true))
	return owneraction.Principal{OwnerID: "42", OwnerGeneration: generation, Transport: "mcp", ClientID: client.ID, GrantID: id}
}
func (f *decisionFixture) request() Request {
	return Request{TaskRef: f.session.TaskRef, Stage: "shaping", Change: "Keep error context", ClientRequestKey: "decision", DiscordChannelID: "200", Summary: taskdiscussion.Summary{Question: "Which boundary?", Conclusion: "Keep the existing boundary", AlternativesOrUncertainty: "Replacement still requires review", EvidenceBasis: "Current saved shaping", Attribution: "Owner requested revision; agent recommended context"}}
}
func (f *decisionFixture) propose(t *testing.T, op string, r Request) owneraction.Proposal {
	t.Helper()
	p, err := f.service.Propose(t.Context(), f.principal, op, r)
	decisionCheck(t, err)
	return p
}

func TestStageDecisionOriginTimingAndRequestIdentity(t *testing.T) {
	f := newDecisionFixture(t)
	r := f.request()
	p := f.propose(t, "revise_task", r)
	repeated := f.propose(t, "revise_task", r)
	if repeated.ID != p.ID {
		t.Fatal("equal request did not recover original proposal")
	}
	r.Change = "different change"
	if _, err := f.service.Propose(t.Context(), f.principal, "revise_task", r); !errors.Is(err, owneraction.ErrRequestConflict) {
		t.Fatalf("changed request key: %v", err)
	}
	early, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	if early.State != "awaiting_confirmation" || early.Reason != "too_early" {
		t.Fatalf("early consent accepted: %+v", early)
	}
	f.now = f.now.Add(5 * time.Second)
	other := f.grant(t, "replacement", f.principal.OwnerGeneration)
	rejected, err := f.actions.Confirm(t.Context(), other, p.ID)
	if err == nil && rejected.State == "admitted" {
		t.Fatal("replacement grant confirmed original proposal")
	}
	current, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	if current.State != "admitted" {
		t.Fatalf("original confirmation: %+v", current)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	decisionCheck(t, err)
	if wait.State != "waiting" {
		t.Fatal("confirmation executed transition inline")
	}
}

func TestStageDecisionApprovalRetainsDiscordAndRevocationBoundary(t *testing.T) {
	f := newDecisionFixture(t)
	r := f.request()
	r.Change = ""
	p := f.propose(t, "approve_stage", r)
	if p.Tier != owneraction.Binding {
		t.Fatalf("approval tier=%s", p.Tier)
	}
	f.now = f.now.Add(5 * time.Second)
	confirmed, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	if confirmed.State != "awaiting_discord" {
		t.Fatalf("approval skipped Discord: %+v", confirmed)
	}
	delivered, err := f.actions.Step(t.Context(), p.ID)
	decisionCheck(t, err)
	if delivered.DeliveryState != "delivered" {
		t.Fatalf("approval message: %+v", delivered)
	}
	decisionCheck(t, f.db.RevokeConnectorGrant(t.Context(), 42, f.principal.GrantID, f.now))
	reaction, err := f.actions.Reaction(t.Context(), owneraction.Reaction{ChannelID: "200", MessageID: "approval-message", UserID: "100", Emoji: "✅"})
	if err == nil && reaction.State == "admitted" {
		t.Fatal("Discord tap revived revoked stage authority")
	}
	if _, err = f.service.Get(t.Context(), f.principal, p.ID); err == nil {
		t.Fatal("revoked grant retrieved private decision")
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	decisionCheck(t, err)
	if wait.State != "waiting" {
		t.Fatal("revoked approval consumed wait")
	}
}

func TestStageDecisionStaleWaitNeverSubstitutesCurrentTarget(t *testing.T) {
	f := newDecisionFixture(t)
	r := f.request()
	p := f.propose(t, "revise_task", r)
	_, err := f.db.ClaimGraphOwnerReply(t.Context(), store.GraphOwnerReply{InstanceID: f.instance.ID, OwnerID: 42, AuthorID: 42, CommentID: 102, QuestionCommentID: 101, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: "review", Body: "GitHub owner guidance", At: f.now})
	decisionCheck(t, err)
	f.now = f.now.Add(5 * time.Second)
	result, err := f.actions.Confirm(t.Context(), f.principal, p.ID)
	decisionCheck(t, err)
	if result.State != "invalidated" {
		t.Fatalf("stale confirmation=%+v", result)
	}
	wait, err := f.db.GetGraphOwnerWait(t.Context(), f.instance.ID)
	decisionCheck(t, err)
	if wait.ReplyID != 102 || wait.ReplyBody != "GitHub owner guidance" {
		t.Fatalf("stale chat replaced owner decision: %+v", wait)
	}
}

func TestStageDecisionConclusionMustBeDeliveredAndSameTask(t *testing.T) {
	f := newDecisionFixture(t)
	ctx := t.Context()
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: f.principal.OwnerGeneration, RepositoryRef: "github:acme/app", TaskRef: f.session.TaskRef}
	seed := func(id string, s store.DiscussionScope) store.DiscussionConclusion {
		t.Helper()
		_, err := f.db.OpenDiscussion(ctx, store.DiscussionConversation{DiscussionScope: s, ID: "conversation-" + id, ClientID: f.principal.ClientID, RequestKey: "conversation-" + id, CreatedAt: f.now})
		decisionCheck(t, err)
		c, err := f.db.ReserveDiscussionConclusion(ctx, store.DiscussionConclusion{DiscussionScope: s, ID: id, ConversationID: "conversation-" + id, ConclusionKey: id, Digest: "digest-" + id, Summary: `{"conclusion":"Retain the existing boundary"}`, Rendered: "Earlier conclusion", GrantID: f.principal.GrantID, CreatedAt: f.now})
		decisionCheck(t, err)
		return c
	}
	same := seed("same-task", scope)
	r := f.request()
	r.ConclusionRef = same.ID
	r.ConversationRef = same.ConversationID
	if _, err := f.service.Propose(ctx, f.principal, "revise_task", r); err == nil {
		t.Fatal("unpublished conclusion referenced as public")
	}
	link := "https://github.com/acme/app/issues/7#issuecomment-101"
	decisionCheck(t, f.db.UpdateDiscussionConclusion(ctx, scope, same.ID, "pending", "sending", "", "", f.now))
	decisionCheck(t, f.db.UpdateDiscussionConclusion(ctx, scope, same.ID, "sending", "delivered", "", link, f.now))
	proposal := f.propose(t, "revise_task", r)
	var value frozen
	decisionCheck(t, json.Unmarshal([]byte(proposal.Facts.Payload), &value))
	if !strings.Contains(value.Rendered, link) || strings.Contains(value.Rendered, "Earlier conclusion") {
		t.Fatalf("summary republished conclusion or omitted reference: %s", value.Rendered)
	}
	foreignScope := scope
	foreignScope.TaskRef = "github:acme/app#8"
	foreign := seed("foreign-task", foreignScope)
	decisionCheck(t, f.db.UpdateDiscussionConclusion(ctx, foreignScope, foreign.ID, "pending", "sending", "", "", f.now))
	decisionCheck(t, f.db.UpdateDiscussionConclusion(ctx, foreignScope, foreign.ID, "sending", "delivered", "", link, f.now))
	r.ClientRequestKey = "foreign-decision"
	r.ConclusionRef = foreign.ID
	r.ConversationRef = ""
	if _, err := f.service.Propose(ctx, f.principal, "revise_task", r); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("foreign-task conclusion accepted: %v", err)
	}
}
