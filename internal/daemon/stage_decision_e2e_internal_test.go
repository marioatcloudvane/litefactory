package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/stagedecision"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

func stageDecisionToken(t *testing.T, db *store.DB, resource string, at time.Time, ownerID int64, repo store.Repo) string {
	t.Helper()
	ctx := t.Context()
	if _, err := db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: ownerID, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ReconcileConnectorOwner(ctx, ownerID, at)
	if err != nil {
		t.Fatal(err)
	}
	client := store.ConnectorClient{ID: "stage-client", Name: "Owner's client", AuthMethod: "none", Redirects: []string{"https://client.example/callback"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	if err = db.RegisterConnectorClient(ctx, client, at); err != nil {
		t.Fatal(err)
	}
	browser := store.ConnectorBrowser{ID: "stage-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: resource, Scope: connector.RepositoryActionsScope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: at.Add(10 * time.Minute)}
	if err = db.CreateConnectorBrowser(ctx, browser, ownerID, at); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, ownerID, at); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: "stage-grant", ClientID: client.ID, Resource: resource, Scope: browser.Scope, OwnerID: ownerID, OwnerGeneration: owner.Generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour), ActionCoverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repo.ID, RepositoryRef: "github:" + repo.Owner + "/" + repo.Name}}}}
	code := store.ConnectorCode{Digest: "stage-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	if err = db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, ownerID, at, grant, code, true); err != nil {
		t.Fatal(err)
	}
	access := "controlled-stage-access"
	_, err = db.ExchangeConnectorCode(ctx, store.ConnectorExchange{Digest: code.Digest, ClientID: client.ID, RedirectURI: code.RedirectURI, Resource: resource, Scope: code.Scope, PKCEChallenge: code.PKCEChallenge, OwnerID: ownerID, Now: at, Pair: store.ConnectorTokenPair{AccessDigest: plainJourneyDigest(access), RefreshDigest: plainJourneyDigest("controlled-stage-refresh"), AccessExpiresAt: at.Add(15 * time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	return access
}

func stageDecisionCall(t *testing.T, handler http.Handler, resource, token, name string, arguments any) json.RawMessage {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": arguments}})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, resource, bytes.NewReader(body))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("MCP-Protocol-Version", "2025-11-25")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("%s HTTP %d %s", name, response.Code, response.Body.String())
	}
	return response.Body.Bytes()
}

// The controlled client maps ordinary owner words to a strict proposal. Real
// OAuth, HTTP, confirmation, SQLite admission and receipt recovery run below.
func TestStageDecisionConfirmedRevisionJourney(t *testing.T) {
	f := stageDecisionResearchFixture(t)
	ctx := t.Context()
	stageDecisionPublishQuestion(t, f)
	at := time.Now().UTC().Truncate(time.Millisecond)
	cfg := config.Defaults().RemoteConnector
	cfg.Enabled = true
	cfg.OwnerID = 7
	cfg.GitHubClientID = "stage-app"
	cfg.GitHubSecretRef.Env = "STAGE_APP"
	cfg.CookieSecretRef.Env = "STAGE_COOKIE"
	repo, err := f.db.GetRepo(ctx, f.session.RepoID)
	researchRecoveryCheck(t, err)
	token := stageDecisionToken(t, f.db, cfg.ResourceURL, at, 7, repo)
	control, err := connector.NewLocalControl(ctx, f.db, stageDecisionControl{db: f.db, now: func() time.Time { return at }})
	researchRecoveryCheck(t, err)
	actions := owneraction.New(f.db, stageDecisionFallback{}, func() time.Time { return at })
	publisher := &stageDecisionPublisher{}
	source := &doctorTestForge{issue: github.Issue{Number: f.session.IssueNumber, State: github.StateOpen, Labels: []string{"Mode: Research"}}}
	boundary := stageDecisionBoundary{daemon: &Daemon{db: f.db, forge: source, log: slog.New(slog.DiscardHandler)}}
	decisions, err := stagedecision.New(stagedecision.Options{DB: f.db, Now: func() time.Time { return at }, Protect: func(_ context.Context, text string) (string, error) { return text, nil }, Publisher: publisher, Boundary: boundary})
	researchRecoveryCheck(t, err)
	decisions.BindActions(actions)
	server, err := connector.New(ctx, connector.Options{Config: cfg, DB: f.db, Logger: slog.New(slog.DiscardHandler), LocalControl: control, GitHubSecret: "fixture", CookieKey: bytes.Repeat([]byte{7}, 32), Now: func() time.Time { return at }, StageDecisions: decisions})
	researchRecoveryCheck(t, err)
	server.SetActions(actions)
	handler := server.Handler()
	change := "Use the existing owner admission boundary and retain its error context."
	request := stagedecision.Request{TaskRef: f.session.TaskRef, Stage: "investigation", Change: change, ClientRequestKey: "owner-revision", Summary: taskdiscussion.Summary{Question: "Which boundary should the investigation use?", Conclusion: "Revise the investigation to use the existing owner admission boundary.", AlternativesOrUncertainty: "Replacement findings have not been produced yet.", EvidenceBasis: "The saved current investigation question.", Attribution: "Owner requested revision; agent recommends preserving error context."}}
	decodeProposal := func(raw json.RawMessage) owneraction.Proposal {
		t.Helper()
		var out struct {
			Result struct {
				IsError  bool                 `json:"isError"`
				Proposal owneraction.Proposal `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Result.IsError || out.Result.Proposal.ID == "" {
			t.Fatalf("proposal response: %s %v", raw, err)
		}
		return out.Result.Proposal
	}
	proposed := decodeProposal(stageDecisionCall(t, handler, cfg.ResourceURL, token, "revise_task", request))
	if proposed.Tier != owneraction.Conversational || !strings.Contains(proposed.Facts.Summary, change) {
		t.Fatalf("incomplete preview: %+v", proposed)
	}
	before, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if before.State != "waiting" || publisher.calls != 0 {
		t.Fatalf("proposal changed workflow or published: %+v calls=%d", before, publisher.calls)
	}
	early := stageDecisionCall(t, handler, cfg.ResourceURL, token, "action_confirm", map[string]string{"proposal_id": proposed.ID})
	var earlyResult struct {
		Result struct {
			Proposal owneraction.Proposal `json:"structuredContent"`
		} `json:"result"`
	}
	researchRecoveryCheck(t, json.Unmarshal(early, &earlyResult))
	if earlyResult.Result.Proposal.State == "admitted" {
		t.Fatal("confirmation bypassed minimum review time")
	}
	at = proposed.ConfirmNotBefore
	confirmed := decodeProposal(stageDecisionCall(t, handler, cfg.ResourceURL, token, "action_confirm", map[string]string{"proposal_id": proposed.ID}))
	if confirmed.State != "admitted" {
		t.Fatalf("confirmation=%+v", confirmed)
	}
	finished, err := actions.Step(ctx, proposed.ID)
	researchRecoveryCheck(t, err)
	if finished.State != "succeeded" {
		t.Fatalf("execution=%+v", finished)
	}
	if source.issueReads < 3 {
		t.Fatalf("proposal, confirmation and execution did not consult live task availability: %d", source.issueReads)
	}
	wait, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if wait.State != "claimed" || wait.ReplyBody != "Requested revision:\n"+change || wait.ReplyAuthorID != 7 {
		t.Fatalf("revision did not use exact owner continuation: %+v", wait)
	}
	read := func() stagedecision.View {
		t.Helper()
		raw := stageDecisionCall(t, handler, cfg.ResourceURL, token, "decision_get", map[string]string{"decision_ref": proposed.ID})
		var out struct {
			Result struct {
				IsError bool               `json:"isError"`
				View    stagedecision.View `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(raw, &out); err != nil || out.Result.IsError {
			t.Fatalf("decision response %s %v", raw, err)
		}
		return out.Result.View
	}
	view := read()
	if view.Transition == nil || view.Transition.State != "applied" || view.DeliveryState != "pending" || view.Summary == "" {
		t.Fatalf("decision did not separate transition and delivery: %+v", view)
	}
	repeated := decodeProposal(stageDecisionCall(t, handler, cfg.ResourceURL, token, "revise_task", request))
	if repeated.ID != proposed.ID {
		t.Fatal("same request created another decision")
	}
	_, err = actions.Step(ctx, proposed.ID)
	researchRecoveryCheck(t, err)
	after, err := f.db.GetGraphOwnerWait(ctx, f.instance.ID)
	researchRecoveryCheck(t, err)
	if after.ReplyID != wait.ReplyID || after.ReplyBody != "Requested revision:\n"+change || publisher.calls != 0 {
		t.Fatalf("repeated request replayed transition or sent unrequested summary: %+v", after)
	}
	// The equivalent authenticated GitHub guidance reaches the same owner wait
	// state without granting approval to replacement work.
	githubFixture := stageDecisionResearchFixture(t)
	stageDecisionPublishQuestion(t, githubFixture)
	_, err = AdmitResearchOwnerReply(ctx, githubFixture.db, githubFixture.instance.ID, 7, githubFixture.session.TaskRef, githubFixture.session.TaskRef, "research", github.Comment{ID: 102, Author: github.User{ID: 7}, Body: "Requested revision:\n" + change}, at)
	researchRecoveryCheck(t, err)
	githubWait, err := githubFixture.db.GetGraphOwnerWait(ctx, githubFixture.instance.ID)
	researchRecoveryCheck(t, err)
	if githubWait.State != wait.State || githubWait.ReplyBody != wait.ReplyBody || githubWait.ReplyAuthorID != wait.ReplyAuthorID || githubWait.ContinuationAttemptID != wait.ContinuationAttemptID {
		t.Fatalf("chat and GitHub guidance have different owner effects: chat=%+v GitHub=%+v", wait, githubWait)
	}
	// A fresh service recovers the same receipt, without another confirmation.
	recovered, err := stagedecision.New(stagedecision.Options{DB: f.db, Now: func() time.Time { return at }, Protect: func(_ context.Context, text string) (string, error) { return text, nil }, Publisher: publisher, Boundary: boundary})
	researchRecoveryCheck(t, err)
	fresh := owneraction.New(f.db, stageDecisionFallback{}, func() time.Time { return at })
	recovered.BindActions(fresh)
	server.SetActions(fresh)
	server.SetStageDecisions(recovered)
	restored := read()
	if restored.Transition == nil || restored.Transition.State != "applied" || restored.DecisionRef != view.DecisionRef || restored.Summary != view.Summary {
		t.Fatalf("reconnect lost reviewed decision: %+v", restored)
	}
	researchRecoveryCheck(t, recovered.PublishPending(ctx))
	researchRecoveryCheck(t, recovered.PublishPending(ctx))
	delivered := read()
	if publisher.calls != 1 || publisher.rendered != view.Summary || delivered.DeliveryState != "delivered" || delivered.CommentURL == "" || delivered.Transition == nil || delivered.Transition.State != "applied" {
		t.Fatalf("reviewed summary was not delivered exactly once: calls=%d decision=%+v", publisher.calls, delivered)
	}
}

type stageDecisionPublisher struct {
	calls    int
	rendered string
}

func (p *stageDecisionPublisher) Publish(_ context.Context, conclusion store.DiscussionConclusion) (string, error) {
	p.calls++
	p.rendered = conclusion.Rendered
	return "https://github.com/acme/research/issues/342#issuecomment-201", nil
}
func (*stageDecisionPublisher) Recover(context.Context, store.DiscussionConclusion) (string, bool, error) {
	return "", false, nil
}

type stageDecisionFallback struct{}

func (stageDecisionFallback) Resolve(context.Context, owneraction.Principal, owneraction.Invocation, time.Time) (owneraction.Facts, error) {
	return owneraction.Facts{}, errors.New("unrelated action")
}
func (stageDecisionFallback) Recheck(context.Context, owneraction.Principal, owneraction.Facts, time.Time) error {
	return errors.New("unrelated action")
}
func (stageDecisionFallback) Execute(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, error) {
	return owneraction.Result{}, errors.New("unrelated action")
}
func (stageDecisionFallback) Recover(context.Context, owneraction.Principal, owneraction.Facts, string, time.Time) (owneraction.Result, bool, error) {
	return owneraction.Result{}, false, errors.New("unrelated action")
}

type stageDecisionControl struct {
	db  *store.DB
	now func() time.Time
}

func (stageDecisionControl) Ready(context.Context) error { return nil }
func (c stageDecisionControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.db.ListConnectorGrants(ctx, 7, c.now())
}
func (c stageDecisionControl) Revoke(ctx context.Context, id string) error {
	return c.db.RevokeConnectorGrant(ctx, 7, id, c.now())
}
func (c stageDecisionControl) RevokeAll(ctx context.Context) error {
	return c.db.RevokeAllConnectorGrants(ctx, 7, c.now())
}

func stageDecisionPublishQuestion(t *testing.T, f *researchRecoveryFixture) {
	t.Helper()
	ctx := t.Context()
	researchRecoveryCheck(t, f.db.CompleteGraphNodeAttempt(ctx, f.attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: f.at, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: []byte(`{"response":"ask"}`)}}}))
	researchRecoveryCheck(t, f.db.RetainGraphComment(ctx, store.GraphCommentIntent{Key: "decision-question", InstanceID: f.instance.ID, SourceAttemptID: f.attempt.ID, Node: "comment", Kind: "question", Candidate: []byte(`{"outcome":"incomplete","questions":[{"id":"Q-1","text":"Which boundary should the investigation use?"}]}`)}))
	_, err := f.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: "decision-question", SessionID: f.session.ID, RunID: f.run.ID, TriggerID: 20, Task: f.session.TaskRef, Destination: f.session.TaskRef, TriggerNamespace: "issue-comment", Purpose: "research-question", BotUserID: 99, Body: "Which boundary should the investigation use?", Marker: "decision-question"})
	researchRecoveryCheck(t, err)
	_, err = f.db.MarkReportDeliverySent(ctx, "decision-question")
	researchRecoveryCheck(t, err)
	_, err = f.db.ConfirmReportDelivery(ctx, "decision-question", 101)
	researchRecoveryCheck(t, err)
	_, err = f.db.CompleteReportDelivery(ctx, "decision-question")
	researchRecoveryCheck(t, err)
}

func stageDecisionResearchFixture(t *testing.T) *researchRecoveryFixture {
	t.Helper()
	ctx := t.Context()
	db := authorDB(t)
	f := &researchRecoveryFixture{db: db, at: time.Now().UTC().Truncate(time.Millisecond)}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "research"})
	researchRecoveryCheck(t, err)
	f.session, err = db.GetOrCreateSession(ctx, repo, 342)
	researchRecoveryCheck(t, err)
	_, err = db.AdmitResearchPath(ctx, f.session.ID, "graph")
	researchRecoveryCheck(t, err)
	graph := `digraph research {
 graph [team="test",mode="research",profile_version="1",start="research"];
 research [lf_action="lf_agent",lf_agent_markdown="research",lf_outcomes="success"];
 comment [lf_action="lf_ask",lf_source="research",lf_outcomes="published"];
 waiting [lf_terminal="true"];
 research -> comment [lf_on="success"];
 comment -> waiting [lf_on="published"];
 }`
	files, err := json.Marshal(map[string][]byte{"research.dot": []byte(graph), "research.md": []byte("Fixture research role")})
	researchRecoveryCheck(t, err)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "research", ProfileVersion: 1, Path: "research.dot", Digest: strings.Repeat("a", 64), TeamSnapshotDigest: strings.Repeat("b", 64)})
	researchRecoveryCheck(t, err)
	admission, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "research", SessionID: f.session.ID, TriggerID: 20, DefinitionID: definition.ID, Start: "research", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: files})
	researchRecoveryCheck(t, err)
	f.instance = admission.Instance
	segment, err := db.GetOrCreateSegment(ctx, f.session.ID, 1, "research", store.AgentClaude)
	researchRecoveryCheck(t, err)
	f.run, err = db.AllocateRun(ctx, store.AllocateRunParams{SessionID: f.session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 20, LogPath: func(int) string { return "research.log" }})
	researchRecoveryCheck(t, err)
	f.attempt, err = db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: f.instance.ID, LaunchID: admission.LaunchID, Node: "research", RunID: f.run.ID, StartedAt: f.at})
	researchRecoveryCheck(t, err)
	return f
}
