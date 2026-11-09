package daemon_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/durableworkflow"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factorylib/factorylibtest"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/messaging/discord"
	"github.com/cloudvane/litefactory/internal/messagingstorage"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"github.com/cloudvane/litefactory/internal/turn"
)

// AC-001 main path: two kept drafts of a brainstorm, the owner's proposal of
// brainstorm_create_tasks over the connector and the spoken yes give exactly
// one bot issue per draft carrying its marker, a factory note on each issue as
// factory output, Plan started on each with the brief seeded before the first
// Plan agent, and one Discord summary.
//
// Real below the fixture: OAuth grant and MCP transport, the drafts scan
// through the durable worker, the daemon's owner-action service with the
// brainstorm_create_tasks handler, the batch processor and launch step (one
// round each, as the serving loop runs them), the daemon tick, the locked turn
// path, the Plan graph preparation, report delivery and notice delivery.
// Fakes: the forge and Discord HTTP servers, the Shaping and Plan runners.
func TestBrainstormTasksE2E_SpokenYesCreatesIssuesAndStartsPlanFromBrief(t *testing.T) {
	ctx := t.Context()
	f := newFixture(t)
	realRepo := gittest.New(t)
	btCheck(t, os.Rename(realRepo.Dir, f.repo.Path))
	f.repo.RemoteURL = realRepo.Origin
	var err error
	f.repo, err = f.db.UpsertRepo(ctx, f.repo)
	btCheck(t, err)
	f.plantCursor()
	repositoryRef := "github:" + testOwnerName + "/" + testRepoName
	at := f.clock.Now()

	// The real Plan graph runs on a real repository with a fake agent runner
	// that records, for the first Plan agent of each issue, its prompt and the
	// brief file it finds in its worktree.
	plan := &brainstormPlanRunner{}
	f.daemon = f.newDaemon(daemon.Options{Turn: daemon.NewExecutor(brainstormPlanOptions(t, f, plan)), Agents: turn.RunnerSet{"claude": plan}})

	// Discord: one owner channel that is the project's workflow notice channel.
	var discordMu sync.Mutex
	var discordPosts []string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/applications/@me" {
			_, _ = w.Write([]byte(`{"flags":262144}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/channels/3/messages" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		discordMu.Lock()
		discordPosts = append(discordPosts, body.Content)
		id := len(discordPosts)
		discordMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"` + strconv.Itoa(1000+id) + `"}`))
	}))
	t.Cleanup(api.Close)
	adapter := discord.New(discord.Options{Token: secrets.NewToken("test-token"), APIURL: api.URL, Client: api.Client()})
	chat := messaging.NewWithServices(messagingstorage.New(f.db), factory.NewMessaging(f.db), "discord", "7", []string{"3"}, adapter, factory.NewHealth(f.db))
	chat.Protect = func(_ context.Context, s string) (string, error) { return s, nil }

	// Connector with one grant holding repository:actions and repository:brainstorm.
	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = ownerID
	cfg.GitHubClientID = "fixture"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	cfg.WorkflowNotices = []config.WorkflowNoticeDestination{{Project: "github:" + testOwnerName + "/" + testRepoName, ChannelID: "3"}}
	token := brainstormTasksToken(t, f, cfg.ResourceURL, at)

	// The brainstorm service runs drafts scans through the shared durable worker
	// with a fake read-only Shaping runner.
	shaping := &brainstormTasksShaping{t: t, titles: []string{"Magic link sign-in", "Session expiry notice", "Drop me"}}
	protect := func(_ context.Context, s string) (string, error) { return s, nil }
	brainstorms, err := brainstorm.New(brainstorm.Options{DB: f.db, Now: f.clock.Now, Team: brainstormTasksTeam{}, Runner: shaping, Protect: protect, Deadline: time.Minute,
		Consent: func(ctx context.Context, p brainstorm.Principal, ref string) error {
			return f.db.CheckBrainstormConsent(ctx, store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID}, ref, f.clock.Now())
		}})
	btCheck(t, err)
	registry, err := durableworkflow.NewRegistry(durableworkflow.BuiltinDefinitions())
	btCheck(t, err)
	workflows, err := durableworkflow.New(durableworkflow.Options{DB: f.db, Registry: registry, Now: f.clock.Now, Protect: protect})
	btCheck(t, err)
	btCheck(t, workflows.Register(brainstorms))

	// Test seam (brainstorm_tasks_export_test.go): the daemon's own owner-action service with the
	// brainstorm task handlers bound to this brainstorm service, the notice
	// channel, and one round of the batch processor and launch step.
	harness := f.daemon.EnableBrainstormTasksForTest(cfg, brainstorms, chat, []string{"3"}, f.clock.Now)
	local, err := connector.NewLocalControl(ctx, connectorstorage.New(f.db), brainstormTasksControl{db: f.db, now: f.clock.Now})
	btCheck(t, err)
	server, err := connector.New(ctx, connector.Options{Guidance: factorylibtest.Connector(), Config: cfg, Storage: connectorstorage.New(f.db), Access: connectorstorage.New(f.db), Logger: slog.New(slog.DiscardHandler), LocalControl: local, GitHubSecret: "fixture-secret", CookieKey: bytes.Repeat([]byte{7}, 32), Now: f.clock.Now, Actions: harness.Actions})
	btCheck(t, err)
	server.SetBrainstorm(brainstorms)
	call := brainstormTasksCaller(t, server.Handler(), cfg.ResourceURL, token)

	// Drafts exist: the owner asks for drafts and the Shaping run answers.
	var opened brainstorm.View
	call("brainstorm_open", map[string]any{"repository": repositoryRef}, &opened)
	var drafting brainstorm.ScanView
	call("brainstorm_draft", map[string]any{"brainstorm": opened.Brainstorm.ID, "request_key": "drafts-1"}, &drafting)
	if ran, e := workflows.RunOnce(ctx); e != nil || !ran {
		t.Fatalf("drafts scan RunOnce = %v %v", ran, e)
	}
	var set brainstorm.DraftSetView
	call("brainstorm_drafts_get", map[string]any{"brainstorm": opened.Brainstorm.ID}, &set)
	if len(set.Drafts) != 3 || set.State != store.BrainstormDraftSetLatest {
		t.Fatalf("draft set %+v", set)
	}

	// The owner drops one draft and proposes; the client carries the identifiers.
	var proposal owneraction.Proposal
	call("action_propose", map[string]any{"action": "brainstorm_create_tasks", "arguments": map[string]string{"brainstorm": opened.Brainstorm.ID, "draft_set": set.DraftSetID, "drafts": set.Drafts[0].Key + "," + set.Drafts[1].Key}, "client_request_key": "owner-create-tasks"}, &proposal)
	if proposal.Tier != owneraction.Conversational || proposal.Facts.TargetRevision == "" {
		t.Fatalf("proposal %+v", proposal)
	}
	if len(brainstormTasksIssues(f)) != 0 || len(plan.firstAgents()) != 0 {
		t.Fatal("a proposal alone created an issue or started Plan")
	}

	// The spoken yes after the review window.
	f.clock.advance(5 * time.Second)
	call("action_confirm", map[string]any{"proposal_id": proposal.ID}, &proposal)

	// The owner-action step, the processor, the launch step, the daemon tick
	// and notice delivery run in rounds, as the serving loops would.
	round := func() {
		t.Helper()
		if _, e := harness.Actions.Step(ctx, proposal.ID); e != nil {
			t.Fatalf("action step: %v", e)
		}
		btCheck(t, harness.Step(ctx))
		f.settle()
		for {
			sent, e := chat.NoticeStep(ctx, f.clock.Now())
			btCheck(t, e)
			if !sent {
				break
			}
		}
		f.clock.advance(30 * time.Second)
	}
	for range 12 {
		round()
	}

	// Exactly one bot issue per kept draft, each body carrying its marker.
	created := brainstormTasksIssues(f)
	if len(created) != 2 {
		t.Fatalf("created %d issues, want 2", len(created))
	}
	batch, err := f.db.BrainstormTaskBatchForDraftSet(ctx, set.DraftSetID)
	btCheck(t, err)
	items, err := f.db.BrainstormTaskItems(ctx, batch.ID)
	btCheck(t, err)
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	for i, item := range items {
		issue := created[i]
		marker := brainstorm.Marker(opened.Brainstorm.ID, set.Drafts[i].ID, item.Generation)
		if issue.User.ID != botID || issue.Title != set.Drafts[i].Title || strings.Count(issue.Body, marker) != 1 || item.Marker != marker {
			t.Fatalf("issue %d: %+v item %+v", i, issue, item)
		}
		if item.State != store.BrainstormItemPlanStarted || item.IssueNumber != issue.Number {
			t.Fatalf("item %d not started: state=%s reason=%q comments=%+v", i, item.State, item.Reason, brainstormTasksComments(f, item.IssueNumber))
		}
	}

	// Each issue carries one factory note posted as factory output; nothing
	// posted on the forge is a /plan command.
	posted, _, _ := f.forge.snapshot()
	for _, issue := range created {
		notes := 0
		for _, body := range brainstormTasksComments(f, issue.Number) {
			if strings.HasPrefix(strings.TrimSpace(body), "/plan") {
				t.Fatalf("issue %d got a plan command: %q", issue.Number, body)
			}
			if strings.Contains(strings.ToLower(body), "brainstorm") {
				notes++
			}
		}
		if notes != 1 {
			t.Fatalf("issue %d has %d factory notes", issue.Number, notes)
		}
	}
	for _, body := range posted {
		if strings.HasPrefix(strings.TrimSpace(body), "/plan") {
			t.Fatalf("posted a plan command: %q", body)
		}
	}

	// Plan started on each issue, and the first Plan agent of each issue found
	// the seeded brief: the prompt names it and the file holds the ledger.
	first := plan.firstAgents()
	if len(first) != 2 {
		t.Fatalf("first Plan agents %d, want 2", len(first))
	}
	for i, item := range items {
		seen, ok := first[item.IssueNumber]
		if !ok {
			t.Fatalf("no Plan agent for issue %d", item.IssueNumber)
		}
		if !strings.Contains(seen.prompt, "RESEARCH_BRIEF.md") || seen.brief == "" || !strings.Contains(seen.brief, set.Drafts[i].Title) {
			t.Fatalf("issue %d brief not seeded before first agent: %+v", item.IssueNumber, seen)
		}
	}

	// Exactly one Discord summary, naming both issue numbers.
	summaries := func() []string {
		discordMu.Lock()
		defer discordMu.Unlock()
		var out []string
		for _, body := range discordPosts {
			if strings.Contains(body, "#"+strconv.Itoa(items[0].IssueNumber)) || strings.Contains(body, "#"+strconv.Itoa(items[1].IssueNumber)) {
				out = append(out, body)
			}
		}
		return out
	}
	got := summaries()
	if len(got) != 1 {
		t.Fatalf("summaries %d, want 1: %q", len(got), got)
	}
	for _, item := range items {
		if !strings.Contains(got[0], "#"+strconv.Itoa(item.IssueNumber)) {
			t.Fatalf("summary misses #%d: %s", item.IssueNumber, got[0])
		}
	}
	continuations, err := f.db.BrainstormTaskContinuations(ctx, batch.ID)
	btCheck(t, err)
	if len(continuations) != 1 || continuations[0].SummaryState != "sent" {
		t.Fatalf("continuations %+v", continuations)
	}

	// Replaying the yes and more rounds change nothing.
	call("action_confirm", map[string]any{"proposal_id": proposal.ID}, &proposal)
	for range 3 {
		round()
	}
	if issues := len(brainstormTasksIssues(f)); issues != 2 {
		t.Fatalf("replay created issues: %d", issues)
	}
	if len(summaries()) != 1 {
		t.Fatal("replay sent another summary")
	}
}

// --- helpers (filled in when the B, C1, C2 and D APIs land) ---

func btCheck(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type brainstormFirstAgent struct {
	prompt string
	brief  string
}

// brainstormPlanRunner is the fake Plan agent runner. It records the first
// agent request per issue and reads RESEARCH_BRIEF.md from its worktree.
type brainstormPlanRunner struct {
	mu    sync.Mutex
	first map[int]brainstormFirstAgent
}

func (*brainstormPlanRunner) SupportsMCP() bool { return true }

func (r *brainstormPlanRunner) Run(ctx context.Context, req agent.Request) (*agent.RunResult, error) {
	if req.Graph == nil || req.Graph.Node != "set_agent" {
		return nil, fmt.Errorf("unexpected agent run %+v", req.Graph)
	}
	brief := ""
	_ = filepath.WalkDir(req.WorktreePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || (d.IsDir() && d.Name() == ".git") {
			return fs.SkipDir
		}
		if !d.IsDir() && d.Name() == "RESEARCH_BRIEF.md" {
			if b, e := os.ReadFile(path); e == nil {
				brief = string(b)
			}
		}
		return nil
	})
	r.mu.Lock()
	if r.first == nil {
		r.first = map[int]brainstormFirstAgent{}
	}
	if _, ok := r.first[req.Ref.Issue]; !ok {
		r.first[req.Ref.Issue] = brainstormFirstAgent{prompt: req.Prompt, brief: brief}
	}
	r.mu.Unlock()
	// Plan asks its owner a normal question and waits; the spoken yes approved
	// no Plan stage.
	result := agentresult.Result{Outcome: "incomplete", Comment: "Which draft outcome should the story set cover first?", Questions: []agentresult.Question{{ID: "Q1", Audience: "business", Text: "Which outcome comes first?"}}, Payload: agentresult.Payload{Plan: &agentresult.Plan{}}}
	payload, err := json.Marshal(map[string]any{"result": result})
	if err != nil {
		return nil, err
	}
	response, err := (submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}).Call(ctx, "communication_submit", payload)
	if err != nil {
		return nil, err
	}
	if response.Error != "" {
		return nil, errors.New(response.Error)
	}
	return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
}

func (r *brainstormPlanRunner) firstAgents() map[int]brainstormFirstAgent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[int]brainstormFirstAgent{}
	for k, v := range r.first {
		out[k] = v
	}
	return out
}

func brainstormPlanOptions(t *testing.T, f *fixture, runner agent.Runner) turn.Options {
	t.Helper()
	binary, err := git.Resolve(t.Context())
	btCheck(t, err)
	authority, err := turn.NewAttemptAuthority(t.Context(), f.db)
	btCheck(t, err)
	t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(t.Context())) })
	// The MCP socket directory stays short, as on a real host.
	mcpDir, err := os.MkdirTemp("/tmp", "lf-bt-")
	btCheck(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(mcpDir) })
	mcpDir, err = filepath.EvalSymlinks(mcpDir)
	btCheck(t, err)
	mcpPaths := config.NewPaths(mcpDir)
	btCheck(t, os.MkdirAll(mcpPaths.StoryProgressLocksDir(), 0o700))
	opts := turn.Options{DB: f.db, Git: binary, Paths: f.paths, InstanceID: "brainstorm-tasks-e2e", Logger: f.logger, CloneGate: make(lifecycleCloneGate, 1), Runners: turn.RunnerSet{"claude": runner}, Author: git.Author{Name: "Test", Email: "test@example.invalid"}, ResearchReporter: f.reporter, SubIssueCreator: f.client, MCP: &turn.MCPRuntime{Authority: authority, Paths: mcpPaths, Executable: "/factory/litefactory", Redactor: redact.New(redact.Options{}), Comments: f.client, BotID: botID, Now: time.Now}}
	judgeConfig, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	btCheck(t, err)
	opts.Judge = &turn.JudgeService{Library: factorylibtest.Library(t), DB: f.db, Config: judgeConfig, Providers: map[agent.Kind]agent.JudgeRunner{}, Paths: mcpPaths, Executable: "/usr/bin/false", Redactor: redact.New(redact.Options{})}
	opts.Tiers.ModelByTier = map[config.Kind]map[config.Tier]string{"claude": {"balanced": "test-model", "flagship": "test-model"}}
	opts.WorkflowSource = func(ctx context.Context, _ store.Repo, _ agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
		def, err := teamsourcetest.CapturePlan(ctx, specs)
		if err != nil {
			return nil, err
		}
		configBytes := []byte("plan: plan/plan.dot\n")
		digest := sha256.Sum256(configBytes)
		def.Origin = teamsource.WorkflowOrigin{Kind: "local", Source: "local", Slug: "fixture", Version: "1.0.0", GraphPath: "plan/plan.dot", GraphFile: "plan.dot", Team: def.Profile.Team, TeamConfig: configBytes, ConfigDigest: hex.EncodeToString(digest[:])}
		return def, nil
	}
	return opts
}

func brainstormTasksIssues(f *fixture) []fakeIssue {
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	return append([]fakeIssue(nil), f.forge.state(testOwnerName, testRepoName).issues...)
}

// brainstormTasksComments is every comment the factory posted on one issue.
func brainstormTasksComments(f *fixture, issue int) []string {
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	var out []string
	for k, body := range f.forge.postedBodies {
		if f.forge.postedIssues[k] == issue {
			out = append(out, body)
		}
	}
	return out
}

type brainstormTasksTeam struct{}

func (brainstormTasksTeam) Capture(context.Context, string) (brainstorm.TeamCapture, error) {
	return brainstorm.TeamCapture{Version: "fixture-1", Role: "research role fixture", Files: map[string]string{"skills/brainstorm-drafts/SKILL.md": "drafts skill fixture"}}, nil
}

// brainstormTasksShaping is the fake read-only Shaping runner: a drafts scan
// answers with one draft per title, each with a valid O-/N-/K- ledger.
type brainstormTasksShaping struct {
	t      *testing.T
	titles []string
}

func (r *brainstormTasksShaping) Run(_ context.Context, req brainstorm.RunRequest) (brainstorm.RunResult, error) {
	if req.Purpose != brainstorm.PurposeDrafts {
		r.t.Errorf("unexpected scan purpose %q", req.Purpose)
	}
	var drafts []any
	for i, title := range r.titles {
		ledger := map[string]any{"request": "drafts", "decisions": []any{map[string]any{"id": "O-01", "text": "Outcome of " + title + "."}, map[string]any{"id": "N-01", "text": "No mobile client."}, map[string]any{"id": "K-01", "text": "Keep the API."}}, "assumptions": []any{}}
		drafts = append(drafts, map[string]any{"key": string(rune('a' + i)), "title": title, "size": "M", "impact": "Owners sign in faster.", "related_issues": []string{}, "duplicate_warning": "", "ledger": ledger})
	}
	raw, err := json.Marshal(map[string]any{"result": map[string]any{"outcome": "completed", "comment": "Here are the drafts.", "payload": map[string]any{"brainstorm_drafts": map[string]any{"drafts": drafts}}}})
	if err != nil {
		return brainstorm.RunResult{}, err
	}
	result, diagnostics := brainstorm.ValidateDraftsResult(raw)
	if !diagnostics.Valid {
		return brainstorm.RunResult{}, fmt.Errorf("fixture drafts invalid: %+v", diagnostics)
	}
	result.Commit = "main"
	return result, nil
}

func (*brainstormTasksShaping) Recover(context.Context, brainstorm.RunRequest) (brainstorm.RunResult, bool, error) {
	return brainstorm.RunResult{}, false, nil
}

type brainstormTasksControl struct {
	db  *store.DB
	now func() time.Time
}

func (brainstormTasksControl) Ready(context.Context) error { return nil }
func (c brainstormTasksControl) List(ctx context.Context) ([]store.ConnectorGrantSummary, error) {
	return c.db.ListConnectorGrants(ctx, ownerID, c.now())
}
func (c brainstormTasksControl) Revoke(ctx context.Context, id string) error {
	return c.db.RevokeConnectorGrant(ctx, ownerID, id, c.now())
}
func (c brainstormTasksControl) RevokeAll(ctx context.Context) error {
	return c.db.RevokeAllConnectorGrants(ctx, ownerID, c.now())
}

func brainstormTasksDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func brainstormTasksToken(t *testing.T, f *fixture, resource string, at time.Time) string {
	t.Helper()
	ctx := t.Context()
	db := f.db
	owner, err := db.ReconcileConnectorOwner(ctx, ownerID, at)
	btCheck(t, err)
	scope := connector.RepositoryActionsScope + " " + connector.BrainstormScope
	client := store.ConnectorClient{ID: "bt-client", Name: "Owner's client", AuthMethod: "none", Redirects: []string{"https://client.example/callback"}, CreatedAt: at, ExpiresAt: at.Add(time.Hour)}
	btCheck(t, db.RegisterConnectorClient(ctx, client, at))
	browser := store.ConnectorBrowser{ID: "bt-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], Resource: resource, Scope: scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: at.Add(10 * time.Minute)}
	btCheck(t, db.CreateConnectorBrowser(ctx, browser, ownerID, at))
	btCheck(t, db.VerifyConnectorBrowser(ctx, browser.ID, browser.NonceDigest, browser.UpstreamStateDigest, ownerID, at))
	coverage := &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: f.repo.ID, RepositoryRef: "github:" + f.repo.Owner + "/" + f.repo.Name}}}
	grant := store.ConnectorGrant{ID: "bt-grant", ClientID: client.ID, Resource: resource, Scope: scope, OwnerID: ownerID, OwnerGeneration: owner.Generation, CreatedAt: at, ExpiresAt: at.Add(time.Hour), Coverage: coverage, ActionCoverage: coverage}
	code := store.ConnectorCode{Digest: "bt-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: resource, Scope: scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: at.Add(5 * time.Minute)}
	btCheck(t, db.ConsentConnectorBrowser(ctx, browser.ID, browser.NonceDigest, ownerID, at, grant, code, true))
	access := "bt-access-token"
	_, err = db.ExchangeConnectorCode(ctx, store.ConnectorExchange{Digest: code.Digest, ClientID: client.ID, RedirectURI: code.RedirectURI, Resource: resource, Scope: scope, PKCEChallenge: code.PKCEChallenge, OwnerID: ownerID, Now: at, Pair: store.ConnectorTokenPair{AccessDigest: brainstormTasksDigest(access), RefreshDigest: brainstormTasksDigest("bt-refresh"), AccessExpiresAt: at.Add(15 * time.Minute)}})
	btCheck(t, err)
	return access
}

func brainstormTasksCaller(t *testing.T, handler http.Handler, resource, token string) func(string, any, any) {
	return func(tool string, args any, target any) {
		t.Helper()
		raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}})
		btCheck(t, err)
		req := httptest.NewRequest(http.MethodPost, resource, bytes.NewReader(raw)).WithContext(t.Context())
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("MCP-Protocol-Version", "2025-11-25")
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, req)
		var envelope struct {
			Result struct {
				Structured json.RawMessage `json:"structuredContent"`
				IsError    bool            `json:"isError"`
			}
			Error json.RawMessage `json:"error"`
		}
		if err = json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != http.StatusOK || envelope.Result.IsError || len(envelope.Error) > 0 {
			t.Fatalf("%s: HTTP %d %s (%v)", tool, response.Code, response.Body.String(), err)
		}
		// Brainstorm tools wrap their value as evidence; action tools do not.
		var wrapped struct {
			Evidence json.RawMessage `json:"evidence"`
		}
		structured := envelope.Result.Structured
		if strings.HasPrefix(tool, "brainstorm_") && json.Unmarshal(structured, &wrapped) == nil && len(wrapped.Evidence) > 0 {
			structured = wrapped.Evidence
		}
		btCheck(t, json.Unmarshal(structured, target))
	}
}
