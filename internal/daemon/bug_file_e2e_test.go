package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/connector"
	"github.com/cloudvane/litefactory/internal/connectorstorage"
	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// AC-001 main path: the owner's bug is proposed through bug_file_propose, the
// client's retry through action_propose with the same key and arguments gets
// the same proposal back, one spoken yes files exactly one issue whose body is
// the description, the filed-for-the-owner line and the hidden marker, the bug
// label is written by its label ID only, and Bug analysis starts on its own:
// one standalone Bug with an analyst attempt, no comment posted for the start,
// and repair still waiting for /approve-bug.
//
// Real below the fixture: OAuth grant and MCP transport, the daemon's
// owner-action service with the bug_file handler, the filing processor and the
// Bug start step (one round each, as the serving loop runs them), the daemon
// tick, the locked turn path, the real Bug graph runtime, report delivery,
// SQLite and Git. Fakes: the forge HTTP server and the Codex agent runner.
func TestBugFileE2E_ProposeConfirmFilesOneLabelledIssueAndStartsAnalysis(t *testing.T) {
	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	btCheck(t, err)
	t.Setenv("TMPDIR", tempRoot)
	ctx := t.Context()
	f := newFixture(t)
	labels := bugFileForgeLabels(f, map[string]string{"bug": "LA_bug", "enhancement": "LA_enhancement"})

	realRepo := gittest.New(t)
	btCheck(t, os.Rename(realRepo.Dir, f.repo.Path))
	realRepo.Dir = f.repo.Path
	realRepo.WriteFile("behavior.txt", "original behavior\n")
	realRepo.Commit("seed Bug fixture")
	realRepo.Git("push", "origin", "main")
	f.repo.RemoteURL = realRepo.Origin
	f.repo, err = f.db.UpsertRepo(ctx, f.repo)
	btCheck(t, err)
	bugFileBugRuntime(t, f, realRepo)
	f.plantCursor()
	repository := testOwnerName + "/" + testRepoName
	at := f.clock.Now()

	cfg := config.DefaultRemoteConnector()
	cfg.Enabled = true
	cfg.OwnerID = ownerID
	cfg.GitHubClientID = "fixture"
	cfg.GitHubSecretRef.Env = "TEST_GITHUB_SECRET"
	cfg.CookieSecretRef.Env = "TEST_COOKIE_SECRET"
	token := brainstormTasksToken(t, f, cfg.ResourceURL, at)
	harness := f.daemon.EnableBugFilingForTest(cfg, f.clock.Now)
	local, err := connector.NewLocalControl(ctx, connectorstorage.New(f.db), brainstormTasksControl{db: f.db, now: f.clock.Now})
	btCheck(t, err)
	server, err := connector.New(ctx, connector.Options{Config: cfg, Storage: connectorstorage.New(f.db), Access: connectorstorage.New(f.db), Logger: slog.New(slog.DiscardHandler), LocalControl: local, GitHubSecret: "fixture-secret", CookieKey: bytes.Repeat([]byte{7}, 32), Now: f.clock.Now, Actions: harness.Actions})
	btCheck(t, err)
	call := brainstormTasksCaller(t, server.Handler(), cfg.ResourceURL, token)

	// The owner's bug through the dedicated tool, then the client's retry
	// through the generic route with the same key: one proposal.
	const title = "Saved filters vanish after reload"
	const description = "Expected: saved filters stay after a reload.\nActual: the list is empty.\nSteps: save a filter, reload.\nEvidence: behavior.txt"
	var proposal owneraction.Proposal
	call("bug_file_propose", map[string]any{"repository": repository, "title": title, "description": description, "client_request_key": "owner-bug-1"}, &proposal)
	if proposal.ID == "" || proposal.Tier != owneraction.Conversational || proposal.Facts.Invocation.Command != "bug_file" {
		t.Fatalf("proposal %+v", proposal)
	}
	var retried owneraction.Proposal
	call("action_propose", map[string]any{"action": "bug_file", "arguments": map[string]string{"repository": repository, "title": title, "description": description}, "client_request_key": "owner-bug-1"}, &retried)
	if retried.ID != proposal.ID || retried.Facts.TargetRevision != proposal.Facts.TargetRevision {
		t.Fatalf("retry through action_propose gave another proposal: %+v vs %+v", retried, proposal)
	}
	if n := len(brainstormTasksIssues(f)); n != 0 || labels.creates() != 0 {
		t.Fatalf("a proposal alone filed %d issues", n)
	}

	// The one spoken yes after the review window.
	f.clock.advance(5 * time.Second)
	call("action_confirm", map[string]any{"proposal_id": proposal.ID}, &proposal)

	// The owner-action step, the filing processor and start step, the daemon
	// tick and its turns run in rounds, as the serving loops would.
	round := func() {
		t.Helper()
		if _, e := harness.Actions.Step(ctx, proposal.ID); e != nil {
			t.Fatalf("action step: %v", e)
		}
		harness.Step(ctx)
		btCheck(t, f.daemon.Quiesce(ctx))
		f.settle()
		f.clock.advance(30 * time.Second)
	}
	var inspected owneraction.Proposal
	for i := 0; i < 20; i++ {
		round()
		call("action_inspect", map[string]any{"proposal_id": proposal.ID}, &inspected)
		if inspected.Result.State == "succeeded" || inspected.Result.State == "failed" {
			break
		}
	}

	// Exactly one issue, filed by the bot, carrying the exact body.
	created := brainstormTasksIssues(f)
	if len(created) != 1 || labels.creates() != 1 {
		t.Fatalf("created %d issues (%d create calls), want 1; result %+v logs=%s", len(created), labels.creates(), inspected.Result, f.logs)
	}
	issue := created[0]
	filing, err := f.db.BugFilingByExecution(ctx, inspected.ExecutionID)
	btCheck(t, err)
	markerLine := regexp.MustCompile(`^<!-- lf:bug-filing v=1 marker=[0-9a-f]{32} -->$`)
	if !markerLine.MatchString(filing.Marker) {
		t.Fatalf("marker %q", filing.Marker)
	}
	wantBody := description + "\n\nFiled for the owner from a chat client.\n\n" + filing.Marker
	if issue.User.ID != botID || issue.Title != title || issue.Body != wantBody || filing.Body != wantBody {
		t.Fatalf("issue %+v\nwant body %q\nfiling body %q", issue, wantBody, filing.Body)
	}

	// The label went on by label ID, the bug label only, and never by name.
	writes := labels.writes()
	if len(writes) != 1 || writes[0].labelable != bugFileNodeID(issue.ID) || len(writes[0].labelIDs) != 1 || writes[0].labelIDs[0] != "LA_bug" ||
		!strings.Contains(writes[0].query, "labelIds") || strings.Contains(writes[0].query, "labelNames") {
		t.Fatalf("label writes %+v", writes)
	}
	if byName := labels.restWrites(); len(byName) != 0 {
		t.Fatalf("labels written by name: %v", byName)
	}
	if len(issue.Labels) != 1 || issue.Labels[0].Name != "bug" {
		t.Fatalf("issue labels %+v", issue.Labels)
	}

	// The recorded result: Bug started, with the issue link.
	if inspected.Result.State != "succeeded" || inspected.Result.Summary != "Bug started" || inspected.Result.Reference != issue.HTMLURL || filing.IssueLink != issue.HTMLURL {
		t.Fatalf("result %+v filing %+v link %q logs=%s", inspected.Result, filing, issue.HTMLURL, f.logs)
	}
	if filing.State != store.BugFilingDispatched || filing.IssueNumber != issue.Number {
		t.Fatalf("filing %+v", filing)
	}

	// The filing started the Bug as the owner's scheduled work on the issue's
	// session; the factory posted no start sentence and no command.
	session := f.sessionOf(issue.Number)
	if filing.SessionID != session.ID {
		t.Fatalf("filing session %d, issue session %d", filing.SessionID, session.ID)
	}
	for _, c := range bugFileComments(f, issue.Number) {
		if c.User.ID != botID || strings.HasPrefix(strings.TrimSpace(c.Body), "/") {
			t.Fatalf("an unexpected comment exists on the filed issue: %+v", c)
		}
	}

	// The repeated yes and more rounds change nothing.
	call("action_confirm", map[string]any{"proposal_id": proposal.ID}, &proposal)
	for range 3 {
		round()
	}
	if n := len(brainstormTasksIssues(f)); n != 1 || labels.creates() != 1 || len(labels.writes()) != 1 {
		t.Fatalf("replay changed the filing: issues=%d creates=%d label writes=%d", n, labels.creates(), len(labels.writes()))
	}
}

// bugFileBugRuntime installs the real Bug graph runtime with the controlled
// Codex runner on the fixture's daemon, as TestCodexBugAgentMainPath does.
func bugFileBugRuntime(t *testing.T, f *fixture, repo *gittest.Repo) *bugPathRunner {
	t.Helper()
	ctx := t.Context()
	binary, err := git.Resolve(ctx)
	btCheck(t, err)
	home, err := os.MkdirTemp("/tmp", "lf-bug-file-")
	btCheck(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	home, err = filepath.EvalSymlinks(home)
	btCheck(t, err)
	paths := config.NewPaths(home)
	authority, err := turn.NewAttemptAuthority(ctx, f.db)
	btCheck(t, err)
	t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(ctx)) })
	cfg := config.Defaults()
	claude, codexConfig := config.DefaultClaude(), config.DefaultCodex()
	cfg.Agents = config.Agents{Claude: &claude, Codex: &codexConfig}
	judge := &turn.JudgeService{DB: f.db, Config: &cfg, Providers: map[agent.Kind]agent.JudgeRunner{"codex": &bugPathJudge{controlledJudge: &controlledJudge{}}}, Paths: paths, Executable: "/usr/bin/false", Git: binary, Redactor: redact.New(redact.Options{Logger: f.logger})}
	runner := &bugPathRunner{repo: repo}
	runners := turn.RunnerSet{"codex": runner, "claude": bugPathUnexpectedRunner{}}
	opts := turn.Options{DB: f.db, Git: binary, Runners: runners, CloneGate: &bugPathGate{}, Paths: paths, InstanceID: "daemon-instance-test", Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}, Logger: f.logger, ResearchReporter: f.reporter, Judge: judge, ConflictContentMaxBytes: 65536, Tiers: cfg.Tiers,
		MCP: &turn.MCPRuntime{Authority: authority, Paths: paths, Executable: "/usr/bin/false", Now: time.Now, Comments: f.client, BotID: botID, Redactor: redact.New(redact.Options{Logger: f.logger})}, WorkflowSource: bugPathWorkflowSource}
	runtime, err := turn.NewGraphBugRuntime(ctx, opts, turn.BugRuntimeConfig{Code: &bugPathForge{f: f, repo: repo}, Tickets: &bugPathForge{f: f, repo: repo}, OwnerID: ownerID, Permissions: func(_ agent.Kind, role string) (turn.BugRolePermissions, error) {
		if role == "bug-fixer" {
			return turn.BugRolePermissions{Sandbox: "workspace-write"}, nil
		}
		return turn.BugRolePermissions{Sandbox: "read-only"}, nil
	}})
	btCheck(t, err)
	opts.Bug = runtime
	f.turns.set(func(ctx context.Context, req turn.Request) (turn.Result, error) { return turn.Execute(ctx, opts, req) })
	f.daemon = f.newDaemon(daemon.Options{Bug: runtime, BugAgent: "codex", Agents: runners, Judge: judge})
	return runner
}

func bugFileComments(f *fixture, issue int) []fakeComment {
	f.forge.mu.Lock()
	defer f.forge.mu.Unlock()
	var out []fakeComment
	for _, c := range f.forge.state(testOwnerName, testRepoName).comments {
		if c.issue == issue {
			out = append(out, c)
		}
	}
	return out
}

func bugFileNodeID(issueID int64) string { return "I_" + strconv.FormatInt(issueID, 10) }

type bugFileLabelWrite struct {
	query     string
	labelable string
	labelIDs  []string
}

// bugFileLabels is the part of GitHub the fixture forge does not serve: issue
// node ids, the repository's labels with their node ids, and the GraphQL
// addLabelsToLabelable mutation. Everything else reaches the fixture forge.
type bugFileLabels struct {
	f       *fixture
	ids     map[string]string
	mu      sync.Mutex
	created int
	byID    []bugFileLabelWrite
	byName  [][]string
}

func (l *bugFileLabels) creates() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.created
}

func (l *bugFileLabels) writes() []bugFileLabelWrite {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]bugFileLabelWrite(nil), l.byID...)
}

func (l *bugFileLabels) restWrites() [][]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out [][]string
	for _, names := range l.byName {
		for _, name := range names {
			if !strings.HasPrefix(name, "Coding Agent:") {
				out = append(out, names)
				break
			}
		}
	}
	return out
}

// bugFileForgeLabels wraps the fixture forge's handler before any request.
func bugFileForgeLabels(f *fixture, ids map[string]string) *bugFileLabels {
	l := &bugFileLabels{f: f, ids: ids}
	inner := f.forge.server.Config.Handler
	f.forge.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.serve(inner, w, r)
	})
	return l
}

var (
	bugFileIssuesPath = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues$`)
	bugFileIssuePath  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/[0-9]+$`)
	bugFileLabelsPath = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues/[0-9]+/labels$`)
	bugFileLabelPath  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/labels/([^/]+)$`)
)

func (l *bugFileLabels) serve(inner http.Handler, w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/graphql":
		raw, _ := io.ReadAll(r.Body)
		var body struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}
		_ = json.Unmarshal(raw, &body)
		switch {
		case strings.Contains(body.Query, "addLabelsToLabelable"):
			l.addByID(w, body.Query, body.Variables)
			return
		case strings.Contains(body.Query, "label(") || strings.Contains(body.Query, "labels("):
			l.lookup(w, body.Variables)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		inner.ServeHTTP(w, r)
	case r.Method == http.MethodGet && bugFileLabelPath.MatchString(r.URL.Path):
		name := bugFileLabelPath.FindStringSubmatch(r.URL.Path)[1]
		id, ok := l.ids[name]
		if !ok {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": 7, "node_id": id, "name": name})
	case r.Method == http.MethodPost && bugFileIssuesPath.MatchString(r.URL.Path):
		l.mu.Lock()
		l.created++
		l.mu.Unlock()
		l.withNodeID(inner, w, r)
	case r.Method == http.MethodGet && bugFileIssuePath.MatchString(r.URL.Path):
		l.withNodeID(inner, w, r)
	case r.Method == http.MethodPost && bugFileLabelsPath.MatchString(r.URL.Path):
		raw, _ := io.ReadAll(r.Body)
		var names []string
		_ = json.Unmarshal(raw, &names)
		l.mu.Lock()
		l.byName = append(l.byName, names)
		l.mu.Unlock()
		r.Body = io.NopCloser(bytes.NewReader(raw))
		inner.ServeHTTP(w, r)
	default:
		inner.ServeHTTP(w, r)
	}
}

// withNodeID adds GitHub's node_id to a single issue the forge answers.
func (l *bugFileLabels) withNodeID(inner http.Handler, w http.ResponseWriter, r *http.Request) {
	rec := httptest.NewRecorder()
	inner.ServeHTTP(rec, r)
	for k, v := range rec.Header() {
		w.Header()[k] = v
	}
	var issue map[string]any
	if rec.Code < 300 && json.Unmarshal(rec.Body.Bytes(), &issue) == nil {
		if id, ok := issue["id"].(float64); ok {
			issue["node_id"] = bugFileNodeID(int64(id))
			writeJSON(w, rec.Code, issue)
			return
		}
	}
	w.WriteHeader(rec.Code)
	_, _ = w.Write(rec.Body.Bytes())
}

func (l *bugFileLabels) lookup(w http.ResponseWriter, raw json.RawMessage) {
	var v map[string]any
	_ = json.Unmarshal(raw, &v)
	name, _ := v["name"].(string)
	if s, ok := v["label"].(string); ok {
		name = s
	}
	if s, ok := v["labelName"].(string); ok {
		name = s
	}
	var label any
	if id, ok := l.ids[name]; ok {
		label = map[string]any{"id": id, "name": name}
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"label": label}}})
}

func (l *bugFileLabels) addByID(w http.ResponseWriter, query string, raw json.RawMessage) {
	var v struct {
		Labelable   string   `json:"labelable"`
		IDs         []string `json:"ids"`
		LabelableID string   `json:"labelableId"`
		LabelIDs    []string `json:"labelIds"`
		Input       *struct {
			LabelableID string   `json:"labelableId"`
			LabelIDs    []string `json:"labelIds"`
		} `json:"input"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.Input != nil {
		v.LabelableID, v.LabelIDs = v.Input.LabelableID, v.Input.LabelIDs
	}
	if v.Labelable != "" {
		v.LabelableID, v.LabelIDs = v.Labelable, v.IDs
	}
	l.mu.Lock()
	l.byID = append(l.byID, bugFileLabelWrite{query: query, labelable: v.LabelableID, labelIDs: v.LabelIDs})
	l.mu.Unlock()
	names := map[string]string{}
	for name, id := range l.ids {
		names[id] = name
	}
	f := l.f.forge
	f.mu.Lock()
	rs := f.state(testOwnerName, testRepoName)
	found := false
	for i := range rs.issues {
		if bugFileNodeID(rs.issues[i].ID) != v.LabelableID {
			continue
		}
		found = true
		for _, id := range v.LabelIDs {
			if name, ok := names[id]; ok {
				rs.issues[i].Labels = append(rs.issues[i].Labels, fakeLabel{Name: name})
			}
		}
		rs.issues[i].UpdatedAt = f.now()
	}
	f.mu.Unlock()
	if !found {
		writeJSON(w, http.StatusOK, map[string]any{"data": nil, "errors": []any{map[string]any{"type": "NOT_FOUND", "message": "Could not resolve to a node"}}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"addLabelsToLabelable": map[string]any{"clientMutationId": nil, "labelable": map[string]any{"__typename": "Issue", "id": v.LabelableID}}}})
}
