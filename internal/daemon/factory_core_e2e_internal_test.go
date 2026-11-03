package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// External ticket APIs and paid execution are controlled. Admission, verified
// owner input, the live-attempt broker, SQLite receipts and report recovery use
// their production implementations. The Plan state transition is applied directly;
// this fixture does not exercise the graph runtime or daemon lifecycle setup.
// Neither optional transport is constructed.
func TestFactoryCoreTicketMainPathWithoutOptionalTransports(t *testing.T) {
	for _, provider := range []ticket.Provider{ticket.GitHub, ticket.Linear} {
		t.Run(string(provider), func(t *testing.T) {
			ctx := t.Context()
			check := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			gh := newFactoryCoreGitHub(t)
			var remote *linearWorkflowServer
			origin, destination, key, owner := ticket.Origin("https://api.github.com"), "code-owner/code", "1", "code-owner"
			if provider == ticket.Linear {
				remote = newLinearWorkflowServer(t)
				origin, destination, key, owner = ticket.Origin(remote.server.URL), "team-uuid/project-uuid", "parent-uuid", "human-uuid"
			}
			endpoint, err := url.Parse(gh.server.URL)
			check(err)
			dbPath := filepath.Join(t.TempDir(), "factory.db")
			db, err := store.Open(ctx, dbPath)
			check(err)
			defer func() { check(db.Close()) }()
			check(db.Migrate(ctx))
			repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "code-owner", Name: "code", RemoteURL: "git@github.com:code-owner/code.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
			check(err)
			code, err := db.SetConnection(ctx, store.ConnectionVersion{RepoID: repo.ID, Role: store.CodeConnection, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: repo.Slug(), RemoteID: "101", CredentialSource: "env", CredentialEnv: "CODE_TOKEN", Enabled: true, Provenance: "explicit", ClonePath: repo.Path, RemoteURL: repo.RemoteURL, BaseBranch: repo.BaseBranch, FeaturePrefix: repo.FeaturePrefix, WorktreeRoot: repo.WorktreeRoot}, 0)
			check(err)
			source := store.ConnectionVersion{RepoID: repo.ID, Role: store.TicketConnection, Provider: provider, Origin: origin, Destination: destination, CredentialSource: "env", CredentialEnv: "CODE_TOKEN", Enabled: true, Provenance: "explicit"}
			if provider == ticket.GitHub {
				source.RemoteID = "101"
			}
			if provider == ticket.Linear {
				source.CredentialEnv = "LINEAR_TOKEN"
				source.Linear = store.LinearConfiguration{WorkspaceID: "workspace-uuid", TeamID: "team-uuid", ProjectID: "project-uuid", AuthKind: "api_key", ModeLabelsJSON: `{"plan":"label-plan"}`, LifecycleStatesJSON: `{"open":"state-open","completed":"state-completed","retired":"state-retired"}`}
			}
			source, err = db.SetConnection(ctx, source, 0)
			check(err)
			factoryOptions := projectconnection.Options{DB: db, HTTPClient: &http.Client{Transport: linearMixedProviderTransport{githubURL: endpoint}}, Resolver: secrets.Resolver{LookupEnv: func(name string) (string, bool) {
				switch name {
				case "LINEAR_TOKEN":
					return "test-linear-token", true
				case "CODE_TOKEN":
					return "test-github-token", true
				default:
					t.Errorf("unexpected credential reference %s", name)
					return "", false
				}
			}}}
			factory := projectconnection.NewFactory(factoryOptions)
			_, err = db.UpsertIdentity(ctx, store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 7, Handle: "code-owner", Source: store.SourceGH})
			check(err)
			appointment, err := (projectconnection.ApproverService{DB: db, Factory: factory}).Set(ctx, repo.ID, owner, source.Revision, 0)
			check(err)
			clients, err := factory.Clients(ctx, source, 0)
			check(err)
			snapshot, err := clients.Tickets.Resolve(ctx, ticket.Locator{Provider: provider, Origin: origin, Container: destination, Key: key})
			check(err)
			_, err = db.AddTicketBinding(ctx, store.TicketBinding{RepoID: repo.ID, ConnectionVersionID: source.ID, Ref: snapshot.Ref, Enabled: true, Provenance: "verified-discovery"})
			check(err)
			session, err := AdmitProjectTicket(ctx, db, factory, repo, snapshot.Ref)
			check(err)
			pinned, err := db.GetExecutionSnapshot(ctx, session.ID)
			check(err)
			if pinned.TicketVersionID != source.ID || pinned.CodeVersionID != code.ID {
				t.Fatalf("admission lost retained routes: %+v", pinned)
			}
			actorID, botID := int64(7), int64(99)
			if provider == ticket.Linear {
				actorID, err = db.LocalTicketSlot(ctx, "actor", appointment.Actor)
				check(err)
				botID, err = db.LocalTicketSlot(ctx, "actor", clients.Publisher.Identity)
				check(err)
			}
			ref, err := naming.ParseRef(session.TaskRef)
			check(err)
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Logger: logger}), Limit: github.CommentSizeLimit, Logger: logger})
			check(err)
			executor := &factoryCoreReceiptExecutor{t: t, db: db}
			var reporter *turn.Reporter
			var workflow *turn.TicketWorkflowBridge
			build := func() *Daemon {
				t.Helper()
				factoryOptions.DB = db
				factory = projectconnection.NewFactory(factoryOptions)
				codeClients, e := factory.Clients(ctx, code, 99)
				check(e)
				startup, e := github.NewTickets(codeClients.Code)
				check(e)
				bridge, e := turn.NewRoutedGitHubTicketWorkflow(db, startup, 99, func(ctx context.Context, r github.Repo, number int, sessionID int64) (turn.WorkflowTickets, int64, error) {
					return selectTicketConnection(ctx, db, factory, r, number, sessionID)
				})
				check(e)
				workflow = bridge
				reporter, e = turn.NewReporter(turn.ReporterOptions{DB: db, Forge: bridge, Renderer: renderer, Logger: logger, BotUserID: 99})
				check(e)
				filter, e := turn.NewFilter(db, turn.Identities{OwnerUserID: actorID, BotUserID: botID, TicketOrigin: origin}, logger)
				check(e)
				executor.db = db
				d, e := New(Options{DB: db, Forge: codeClients.Code, Tickets: startup, Connections: factory, Filter: filter, Turn: executor, Reporter: reporter, DefaultAgent: "claude", Agents: turn.RunnerSet{"claude": nil}, Classifiers: map[agent.Kind]agent.Classifier{"claude": explicitWorkflowClassifier{t}}, ToolProfiles: config.DefaultToolProfiles().Permissions, Logger: logger, InstanceID: "factory-core-main-path"})
				check(e)
				if d.chat != nil || d.remoteConnector != nil {
					t.Fatal("ticket path constructed an optional transport")
				}
				return d
			}
			d := build()
			addReply := func(id, body string) {
				if remote != nil {
					remote.addReply(id, body, "human-uuid")
				} else {
					gh.addReply(id, body)
				}
			}
			verifiedReplies := map[int64]ticket.Reply{}
			admit := func(id string) turn.Admission {
				t.Helper()
				current, e := factory.Clients(ctx, source, botID)
				check(e)
				cursor, e := github.ReplyCursor(snapshot.Ref, 0)
				if provider == ticket.Linear {
					cursor, e = linear.ReplyCursor(snapshot.Ref), nil
				}
				check(e)
				page, e := current.Tickets.RepliesAfter(ctx, snapshot.Ref, cursor)
				check(e)
				for _, reply := range page.Replies {
					if reply.Identity.RemoteID == id {
						admitted, e := d.admitReply(ctx, session.ID, github.Comment{}, &reply)
						check(e)
						if !admitted.Triggers() {
							t.Fatalf("verified owner input rejected: %+v", admitted)
						}
						verifiedReplies[admitted.Comment.ForgeCommentID] = reply
						return admitted
					}
				}
				t.Fatalf("provider did not return reply %s", id)
				return turn.Admission{}
			}
			run := func(input turn.Admission) error {
				remoteIssue, e := workflow.GetIssue(ctx, github.RepoFromRef(ref), ref.Issue)
				check(e)
				verified := verifiedReplies[input.Comment.ForgeCommentID]
				trigger, e := d.projectTrigger(ctx, session.ID, github.Comment{ID: input.Comment.ForgeCommentID, Body: input.Comment.Body, Author: github.User{ID: input.Comment.AuthorUserID, Login: input.Comment.AuthorHandle}, CreatedAt: input.Comment.CreatedAt, QualifiedReply: &verified})
				check(e)
				if trigger.AuthorID != actorID || trigger.Actor != appointment.Actor || trigger.Source != verified.Identity {
					t.Fatalf("execution lost verified owner provenance: %+v", trigger)
				}
				return d.run(ctx, &task{repo: repo, session: session, ref: ref, forgeRepo: github.RepoFromRef(ref), issue: remoteIssue, log: logger, tickAt: time.Now().UTC()}, trigger, turn.ParseCommand(input.Comment.Body), logger)
			}
			addReply("1001", "/plan investigate the ticket")
			instruction := admit("1001")
			check(run(instruction))
			if executor.calls != 1 {
				t.Fatalf("initial executions = %d", executor.calls)
			}
			// The first result asks for owner input. Admission supplies the verified
			// actor; the existing Plan state machine decides what the answer can change.
			state := planbrief.PlanState{SharedRevision: "main-path-v1"}
			check(state.PutCandidate(planbrief.PlanDraft{Key: "story-001", Name: "Factory ticket work"}, "story", "candidate-1"))
			issueID := int64(ref.Issue)
			check(state.OpenPlanWait("story-001", "question-1", "question", issueID))
			revision, err := db.SavePlanWorkflowState(ctx, ref.String(), 0, state)
			check(err)
			addReply("1002", "/plan use the retained ticket and code connections")
			answer := admit("1002")
			restored, _, err := db.GetPlanWorkflowState(ctx, ref.String())
			check(err)
			promoted, err := restored.ApplyOwnerReply(planbrief.PlanOwnerReply{ID: answer.Comment.ForgeCommentID, OwnerID: actorID, ActorID: actorID, IssueID: issueID, Key: "story-001", Stage: "story", Candidate: "candidate-1", WaitID: "question-1", Body: answer.Comment.Body})
			check(err)
			if promoted || restored.Stories[0].Wait != nil {
				t.Fatal("owner answer failed to resume without approving the draft")
			}
			_, err = db.SavePlanWorkflowState(ctx, ref.String(), revision, restored)
			check(err)
			if remote != nil {
				remote.mu.Lock()
				remote.loseReply = true
				remote.mu.Unlock()
			} else {
				gh.mu.Lock()
				gh.loseReply = true
				gh.mu.Unlock()
			}
			check(run(answer))
			pending, found, err := db.FindPendingReportDelivery(ctx, session.ID, answer.Comment.ForgeCommentID)
			check(err)
			if !found || !pending.PossiblySent || pending.Completed {
				t.Fatalf("lost provider reply did not retain uncertain publication: %+v", pending)
			}
			if executor.calls != 2 {
				t.Fatalf("owner response executions = %d", executor.calls)
			}
			before, exists, err := db.GetWatermark(ctx, session.ID)
			check(err)
			if !exists || before.LastCommentID >= answer.Comment.ForgeCommentID {
				t.Fatal("uncertain publication consumed owner input")
			}
			calls, receiptID, binding := executor.calls, executor.requestID, executor.binding
			originalReceipt := executor.receipt
			// New configuration must not redirect pending work after restart. The
			// resolver rejects these credentials if recovery selects current routes.
			for _, retained := range []store.ConnectionVersion{source, code} {
				replacement := retained
				replacement.CredentialEnv = "REPLACEMENT_TOKEN"
				_, err = db.SetConnection(ctx, replacement, retained.Revision)
				check(err)
			}
			check(db.Close())
			db, err = store.Open(ctx, dbPath)
			check(err)
			check(db.Migrate(ctx))
			d = build()
			restartedPending, err := db.GetReportDelivery(ctx, pending.Key)
			check(err)
			if restartedPending != pending {
				t.Fatal("restart changed the uncertain publication intent")
			}
			restartedPlan, _, err := db.GetPlanWorkflowState(ctx, ref.String())
			check(err)
			if len(restartedPlan.Stories) != 1 || restartedPlan.Stories[0].Wait != nil {
				t.Fatal("restart lost the accepted owner answer")
			}
			reader, err := turn.OpenAgentResultReader(ctx, db, binding, agentresult.EditTargets("plan"))
			check(err)
			receipt, err := reader.Receipt(ctx, binding.RunID, receiptID)
			check(err)
			if receipt.Receipt == nil || receipt.Receipt.InputState != "accepted" || receipt.Receipt.RequestID != receiptID || receipt.Receipt.RunID != binding.RunID {
				t.Fatalf("durable result receipt lost: %+v", receipt)
			}
			if receipt.Receipt.ID != originalReceipt.ID || receipt.Receipt.RequestHash != originalReceipt.RequestHash || receipt.Receipt.AppliedRevision != originalReceipt.AppliedRevision || !bytes.Equal(receipt.Receipt.Payload, originalReceipt.Payload) {
				t.Fatal("restart replaced the accepted result or its request identity")
			}
			// The normal run entry first resumes its existing report. It must find
			// the published reply and commit its watermark without executing again.
			check(run(answer))
			if executor.calls != calls {
				t.Fatalf("publication recovery repeated paid execution: %d -> %d", calls, executor.calls)
			}
			confirmed, err := db.GetReportDelivery(ctx, pending.Key)
			check(err)
			if !confirmed.Completed || confirmed.CommentID == 0 || confirmed.ConfirmedAt.IsZero() || confirmed.Body != pending.Body || confirmed.Marker != pending.Marker || confirmed.Destination != pending.Destination || confirmed.RunID != pending.RunID {
				t.Fatalf("publication recovery did not confirm the original intent: %+v", confirmed)
			}
			after, _, err := db.GetWatermark(ctx, session.ID)
			check(err)
			if after.LastCommentID != answer.Comment.ForgeCommentID {
				t.Fatalf("publication recovery watermark = %d, want %d", after.LastCommentID, answer.Comment.ForgeCommentID)
			}
			runs, err := db.ListRuns(ctx, session.ID)
			check(err)
			if len(runs) != 2 {
				t.Fatalf("durable executions = %d, want 2", len(runs))
			}
			for _, run := range runs {
				if run.Status != store.RunReported {
					t.Fatalf("run %d has unconfirmed publication: %s", run.ID, run.Status)
				}
			}
			check(run(answer))
			posts := gh.postCount()
			if remote != nil {
				remote.mu.Lock()
				posts = remote.posts
				remote.mu.Unlock()
			}
			if executor.calls != 2 || posts != 2 {
				t.Fatalf("replay duplicated execution/publication: executions=%d publications=%d", executor.calls, posts)
			}
			retained, err := db.GetExecutionSnapshot(ctx, session.ID)
			check(err)
			if retained != pinned {
				t.Fatal("restart changed immutable ticket/code routing")
			}
		})
	}
}

// This controlled paid boundary allocates the same durable run and submits to
// the real capability broker. It does not replace admission or publication.
type factoryCoreReceiptExecutor struct {
	t         *testing.T
	db        *store.DB
	calls     int
	requestID string
	binding   store.ResultBinding
	receipt   store.ResultReceipt
}

func (e *factoryCoreReceiptExecutor) Execute(ctx context.Context, req turn.Request) (turn.Result, error) {
	session, err := e.db.GetSession(ctx, req.SessionID)
	if err != nil {
		return turn.Result{}, err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return turn.Result{}, err
	}
	out := turn.Result{SessionID: session.ID, TriggerCommentID: req.Trigger.CommentID, Ref: ref, Session: session, Outcome: harness.OutcomeSucceeded}
	if existing, found, err := e.db.GetRunByTrigger(ctx, session.ID, req.Trigger.CommentID); err != nil {
		return out, err
	} else if found {
		out.Run, out.Disposition = existing, turn.DispositionAlreadyRan
		if existing.Status == store.RunReported {
			out.Disposition = turn.DispositionAlreadyReported
		}
		return out, nil
	}
	segment, err := e.db.GetOrCreateSegment(ctx, session.ID, 1, "main-path", store.AgentClaude)
	if err != nil {
		return out, err
	}
	run, err := e.db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "plan", Agent: store.AgentClaude, TriggerCommentID: req.Trigger.CommentID, LogPath: func(n int) string { return fmt.Sprintf("main-path-%d.log", n) }})
	if err != nil {
		return out, err
	}
	e.calls++
	service, err := turn.BindAgentResultService(ctx, turn.AgentResultServiceOptions{DB: e.db, Attempt: store.ResultAttempt{Task: session.TaskRef, Mode: "plan", WorkContext: fmt.Sprintf("ticket-owner-work-%d", run.ID), RunID: run.ID, Attempt: fmt.Sprintf("attempt-%d", run.ID), SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, Resolve: func(context.Context) (agentresult.Context, error) {
		return agentresult.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}}, nil
	}, ReadTargets: agentresult.EditTargets("plan"), EditTargets: agentresult.EditTargets("plan")})
	if err != nil {
		return out, err
	}
	authority, err := turn.NewAttemptAuthority(ctx, e.db)
	if err != nil {
		return out, err
	}
	defer func() {
		if err := authority.Close(context.WithoutCancel(ctx)); err != nil {
			e.t.Error(err)
		}
	}()
	guard, err := authority.Bind(ctx, service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		return out, err
	}
	dispatcher, err := turn.NewToolDispatcher(guard, service)
	if err != nil {
		return out, err
	}
	directory, err := os.MkdirTemp("/tmp", "lf-core-broker-")
	if err != nil {
		return out, err
	}
	defer func() {
		if err := os.RemoveAll(directory); err != nil {
			e.t.Error(err)
		}
	}()
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return out, err
	}
	broker, err := submission.Start(ctx, submission.Options{Directory: directory, AllowTools: true, MaxWorkers: 2, RequestTimeout: 5 * time.Second, Handler: dispatcher.Handler})
	if err != nil {
		return out, err
	}
	defer func() {
		if err := broker.Close(); err != nil {
			e.t.Error(err)
		}
	}()
	client := submission.Client{DescriptorPath: broker.DescriptorPath()}
	data := []byte(`{"result":{"outcome":"incomplete","reason":"Owner review remains","comment":"Which retained target should this work use?","payload":{"plan":{}}}}`)
	preflight, err := client.Call(ctx, "communication_preflight", data)
	if err != nil {
		return out, err
	}
	var diagnostics agentresult.UpdateDiagnostics
	if err := json.Unmarshal(preflight.Result, &diagnostics); err != nil {
		return out, err
	}
	if preflight.Error != "" || !diagnostics.Valid || !diagnostics.Complete {
		return out, fmt.Errorf("result preflight failed: %s %s", preflight.Error, preflight.Result)
	}
	submitted, err := client.Call(ctx, "communication_submit", data)
	if err != nil {
		return out, err
	}
	if submitted.Error != "" {
		return out, fmt.Errorf("result submit failed: %s", submitted.Error)
	}
	e.requestID, e.binding = submitted.RequestID, service.Binding()
	// Recover by the original request identity, as a runner must after a lost
	// submit reply; no alternate submit is used to force another result.
	receiptData, err := json.Marshal(map[string]string{"request_id": submitted.RequestID})
	if err != nil {
		return out, err
	}
	reply, err := client.Call(ctx, "communication_receipt", receiptData)
	if err != nil {
		return out, err
	}
	var receipt turn.AgentResultSubmission
	if err := json.Unmarshal(reply.Result, &receipt); err != nil {
		return out, err
	}
	if reply.Error != "" || receipt.Receipt == nil || receipt.Receipt.InputState != "accepted" {
		return out, fmt.Errorf("result receipt not accepted: %s %s", reply.Error, reply.Result)
	}
	e.receipt = *receipt.Receipt
	exitCode := int64(0)
	out.Run, err = e.db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exitCode})
	if err != nil {
		return out, err
	}
	out.Disposition = turn.DispositionRan
	out.Harness = &harness.RunResult{Outcome: harness.OutcomeSucceeded, HasResult: true, ResultText: "Which retained target should this work use?", StartedAt: run.StartedAt, FinishedAt: time.Now()}
	return out, nil
}

type factoryCoreGitHub struct {
	t         *testing.T
	mu        sync.Mutex
	server    *httptest.Server
	comments  map[string]map[string]any
	posts     int
	loseReply bool
}

func newFactoryCoreGitHub(t *testing.T) *factoryCoreGitHub {
	t.Helper()
	s := &factoryCoreGitHub{t: t, comments: map[string]map[string]any{}}
	s.server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.server.Close)
	return s
}

func (s *factoryCoreGitHub) addReply(id, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments[id] = s.comment(id, body, 7, "code-owner")
}

func (s *factoryCoreGitHub) comment(id, body string, actor int, login string) map[string]any {
	var numeric int
	if _, err := fmt.Sscan(id, &numeric); err != nil {
		s.t.Fatal(err)
	}
	kind := "User"
	if actor == 99 {
		kind = "Bot"
	}
	return map[string]any{"id": numeric, "body": body, "user": map[string]any{"id": actor, "login": login, "type": kind}, "url": "https://api.github.com/repos/code-owner/code/issues/comments/" + id, "issue_url": "https://api.github.com/repos/code-owner/code/issues/1", "html_url": "https://github.com/code-owner/code/issues/1#issuecomment-" + id, "created_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339), "updated_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)}
}

func (s *factoryCoreGitHub) postCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.posts
}

func (s *factoryCoreGitHub) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Ratelimit-Limit", "5000")
	w.Header().Set("X-Ratelimit-Remaining", "4900")
	w.Header().Set("X-Ratelimit-Resource", "core")
	w.Header().Set("X-Ratelimit-Reset", "4102444800")
	if r.Header.Get("Authorization") != "Bearer test-github-token" {
		s.t.Error("GitHub received wrong credential")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var response any
	switch {
	case r.URL.Path == "/user":
		response = map[string]any{"id": 99, "login": "github-publisher", "type": "Bot"}
	case r.URL.Path == "/users/code-owner":
		response = map[string]any{"id": 7, "login": "code-owner", "type": "User"}
	case r.URL.Path == "/repos/code-owner/code":
		response = map[string]any{"id": 101, "full_name": "code-owner/code"}
	case r.URL.Path == "/repos/code-owner/code/issues/1":
		response = map[string]any{"id": 10001, "number": 1, "title": "Factory work", "body": "/plan investigate the ticket", "state": "open", "url": "https://api.github.com/repos/code-owner/code/issues/1", "html_url": "https://github.com/code-owner/code/issues/1", "user": map[string]any{"id": 7, "login": "code-owner"}, "labels": []any{map[string]any{"name": "Mode: Plan"}}, "created_at": "2026-11-02T22:00:00Z", "updated_at": "2026-11-02T22:00:00Z"}
	case r.URL.Path == "/repos/code-owner/code/issues/1/labels":
		response = []any{map[string]any{"name": "Mode: Plan"}}
	case r.URL.Path == "/repos/code-owner/code/issues/1/comments" && r.Method == http.MethodPost:
		var request struct {
			Body string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			s.t.Error(err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		s.posts++
		id := fmt.Sprintf("%d", 2000+s.posts)
		comment := s.comment(id, request.Body, 99, "github-publisher")
		response = comment
		s.comments[id] = comment
		if s.loseReply {
			s.loseReply = false
			http.Error(w, "lost publication reply", http.StatusServiceUnavailable)
			return
		}
	case r.URL.Path == "/repos/code-owner/code/issues/1/comments":
		keys := make([]string, 0, len(s.comments))
		for id := range s.comments {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		comments := make([]any, 0, len(keys))
		for _, id := range keys {
			comments = append(comments, s.comments[id])
		}
		response = comments
	case r.URL.Path == "/repos/code-owner/code/issues/1/parent":
		http.NotFound(w, r)
		return
	case strings.HasPrefix(r.URL.Path, "/repos/code-owner/code/issues/comments/"):
		response = s.comments[strings.TrimPrefix(r.URL.Path, "/repos/code-owner/code/issues/comments/")]
	default:
		s.t.Errorf("unexpected GitHub operation %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
		return
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.t.Error(err)
	}
}
