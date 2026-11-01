package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
)

type controlledTaskAdvisoryProvider struct {
	calls     []agent.JudgeRequest
	receipts  []string
	committed string
}

func (p *controlledTaskAdvisoryProvider) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	p.calls = append(p.calls, q)
	if q.Invocation.Usage == nil {
		return agent.JudgeRunResult{}, errors.New("canonical usage capture missing")
	}
	coverage := agent.UsageCoverage{Status: agent.CollectionPartial, Diagnostics: []string{"controlled_missing_input_tokens"}}
	invocation := agent.UsageInvocation{AttemptID: q.Invocation.UsageAttemptID, ID: "root", SourceID: "controlled", SourceInvocationID: "root", Coverage: coverage}
	if err := q.Invocation.Usage.Emit(ctx, agent.UsageEvent{Invocation: &invocation}); err != nil {
		return agent.JudgeRunResult{}, err
	}
	tokens := int64(7)
	model := q.Invocation.Model
	observation := agent.UsageObservation{AttemptID: q.Invocation.UsageAttemptID, SourceID: "controlled", EventKey: "output", InvocationID: "root", ActualModel: &model, ModelProvenance: "controlled", Mode: agent.CounterIncremental, Scope: agent.UsageExclusive, Tokens: agent.TokenUsage{Output: &tokens}, Coverage: coverage}
	for range 2 {
		if err := q.Invocation.Usage.Emit(ctx, agent.UsageEvent{Observation: &observation}); err != nil {
			return agent.JudgeRunResult{}, err
		}
	}

	if err := q.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	content, err := os.ReadFile(filepath.Join(q.Invocation.WorkingDirectory, "README.md"))
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	p.committed = string(content)
	client := submission.Client{DescriptorPath: q.Invocation.MCPLaunch.DescriptorPath()}
	call := func(name string, input any) (submission.Response, error) {
		raw, e := json.Marshal(input)
		if e != nil {
			return submission.Response{}, e
		}
		response, e := client.Call(ctx, name, raw)
		if e == nil && response.Error != "" {
			e = errors.New(response.Error)
		}
		return response, e
	}
	answer := taskdiscussion.Answer{Answer: "The captured analysis supports the approach.", Sources: []string{"task.json", "README.md"}, Gaps: []string{"The original model session was not resumed."}}
	response, err := call("communication_preflight", answer)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var diagnostics agentresult.Diagnostics
	if err = json.Unmarshal(response.Result, &diagnostics); err != nil || !diagnostics.Valid {
		return agent.JudgeRunResult{}, fmt.Errorf("preflight rejected result: %s", response.Result)
	}
	if _, err = call("action_confirm", map[string]string{}); err == nil {
		return agent.JudgeRunResult{}, errors.New("workflow tool unexpectedly available")
	}
	response, err = call("communication_submit", answer)
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	if _, err = call("communication_receipt", map[string]string{"request_id": response.RequestID}); err != nil {
		return agent.JudgeRunResult{}, err
	}
	p.receipts = append(p.receipts, response.RequestID)
	return agent.JudgeRunResult{ObservedModel: q.Invocation.Model + "-observed", ProviderVersion: "controlled", Usage: &coverage}, errors.New("controlled lost provider completion reply")
}

func TestTaskAdvisoryRunnerBoundBrokerAndLostReplyRecovery(t *testing.T) {
	runCtx, runCancel := context.WithTimeout(t.Context(), time.Minute)
	defer runCancel()
	db, session, _ := agentResultFixture(t, "research")
	now := time.Now().UTC()
	if _, err := db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := db.GetRepoBySlug(t.Context(), "github", "example", "results")
	if err != nil {
		t.Fatal(err)
	}
	client := store.ConnectorClient{ID: "advisory-client", Name: "Controlled client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = db.RegisterConnectorClient(t.Context(), client, now); err != nil {
		t.Fatal(err)
	}
	browser := store.ConnectorBrowser{ID: "advisory-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], State: "state", Resource: "https://factory.test/litefactory/mcp", Scope: "task:advisory", PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: now.Add(time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), browser, 42, now); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), browser.ID, "nonce", "upstream", 42, now); err != nil {
		t.Fatal(err)
	}
	grant := store.ConnectorGrant{ID: "advisory-grant", ClientID: client.ID, Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Coverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repo.ID, RepositoryRef: "github:example/results"}}}}
	code := store.ConnectorCode{Digest: "advisory-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: now.Add(time.Minute)}
	if err = db.ConsentConnectorBrowser(t.Context(), browser.ID, "nonce", 42, now, grant, code, true); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp("/tmp", "lf-tadv-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	provider := &controlledTaskAdvisoryProvider{}
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture := gittest.New(t)
	clone, err := binary.Open(t.Context(), fixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := clone.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(fixture.Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.WriteFile("README.md", "Later dirty bytes must stay private to the source checkout.\n")
	runner := &TaskAdvisoryRunner{DB: db, Paths: config.NewPaths(root), Executable: "/usr/bin/false", Providers: map[agent.Kind]agent.JudgeRunner{agent.Kind("codex"): provider}, Git: binary, Provider: agent.Kind("codex"), Model: "captured-model", Thinking: "high", Deadline: time.Minute}
	guidance, err := runner.CaptureGuidance(t.Context(), "Captured current analysis", fixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	scope := store.DiscussionScope{OwnerID: 42, OwnerGeneration: owner.Generation, RepositoryRef: "github:example/results", TaskRef: session.TaskRef}
	conversation, err := db.OpenDiscussion(t.Context(), store.DiscussionConversation{DiscussionScope: scope, ID: "runner-conversation", ClientID: client.ID, RequestKey: "open", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	basis := taskdiscussion.Basis{TaskRef: session.TaskRef, RepositoryRef: scope.RepositoryRef, Stage: "investigation", Role: "research", Revision: "saved-revision", Commit: commit, Evidence: json.RawMessage(`{"finding":"Saved evidence"}`), Guidance: guidance, Sources: []string{"saved-result"}, Gaps: []string{}}
	basisBytes, err := json.Marshal(basis)
	if err != nil {
		t.Fatal(err)
	}
	admission := store.AdvisoryAdmission{DiscussionScope: scope, ID: "runner-admission", ConversationID: conversation.ID, Operation: "ask", RequestKey: "question", Digest: "digest", Basis: string(basisBytes), Context: `[]`, Question: "Why this approach?", GrantID: grant.ID, ConsentRevision: "captured", JobID: "runner-job", ExecutionID: "runner-execution", CreatedAt: now}
	if _, err = db.AdmitAdvisory(t.Context(), admission); err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.ClaimWorkflowAttempt(t.Context(), now, time.Minute); err != nil {
		t.Fatal(err)
	}
	// Changes to installed defaults after admission cannot alter the captured invocation.
	runner.Provider = agent.Kind("claude")
	runner.Model = "later-model"
	runner.Thinking = "low"
	runner.Deadline = time.Nanosecond
	request := taskdiscussion.RunRequest{AdmissionID: admission.ID, JobID: admission.JobID, ExecutionID: admission.ExecutionID, Operation: "ask", Question: admission.Question, Basis: basis, Context: json.RawMessage(`[]`)}
	wrong := request
	wrong.JobID = "another-task-job"
	if _, err = runner.Run(runCtx, wrong); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("cross-job invocation: %v", err)
	}
	wrong = request
	wrong.Basis.Revision = "substituted-revision"
	if _, err = runner.Run(runCtx, wrong); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("replaced captured basis: %v", err)
	}
	answer, err := runner.Run(runCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.calls) != 1 || len(provider.receipts) != 1 || provider.committed != string(committed) {
		t.Fatalf("isolated invocation: calls=%d receipts=%d committed=%q", len(provider.calls), len(provider.receipts), provider.committed)
	}
	invocation := provider.calls[0]
	meter, err := db.GetMeterAttempt(t.Context(), invocation.Invocation.UsageAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	if meter.AdvisoryExecutionID != admission.ExecutionID || meter.RunID != 0 || meter.JudgeAttemptID != "" || meter.DoctorAttemptID != "" || meter.PriceRevisionID <= 0 || meter.Outcome != "failure" {
		t.Fatalf("canonical advisory usage attribution: %+v", meter)
	}
	if invocation.Invocation.Usage == nil || invocation.Invocation.Usage.AttemptID != meter.ID || meter.ID == admission.ExecutionID {
		t.Fatal("provider usage did not bind canonical metering attempt")
	}

	snapshot, err := db.NewMeterSnapshot(t.Context(), store.MeterFilter{}, store.MeterSnapshotOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := snapshot.Next(t.Context(), 256)
	if err != nil {
		t.Fatal(err)
	}
	if err = snapshot.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Tokens.Output == nil || *rows[0].Tokens.Output != 7 || rows[0].CollectionStatus != "partial" {
		t.Fatalf("canonical usage evidence lost or fabricated: %+v", rows)
	}
	if invocation.Invocation.Model != "captured-model" || invocation.Thinking != "high" || invocation.Invocation.Attempt != admission.ExecutionID {
		t.Fatal("captured identity or settings changed")
	}
	if invocation.Invocation.WorkingDirectory == fixture.Dir || len(invocation.Invocation.ReadableDirectories) != 1 {
		t.Fatal("provider received mutable source checkout")
	}
	if _, err = os.Stat(invocation.Invocation.WorkingDirectory); !os.IsNotExist(err) {
		t.Fatal("isolated checkout was retained")
	}
	recovered, found, err := runner.Recover(t.Context(), request)
	if err != nil || !found || !reflect.DeepEqual(recovered, answer) {
		t.Fatalf("receipt recovery: %+v %v %v", recovered, found, err)
	}
	again, err := runner.Run(runCtx, request)
	if err != nil || !reflect.DeepEqual(again, answer) || len(provider.calls) != 1 {
		t.Fatalf("paid rerun after saved result: calls=%d error=%v", len(provider.calls), err)
	}
}

func TestTaskAdvisorySummaryCannotActAsOwnerInput(t *testing.T) {
	db, session, _ := agentResultFixture(t, "research")
	for _, command := range []string{"/approve-plan story-001 technical candidate", "/implement start work", "Approved; continue"} {
		body := command + "\n\n<!-- lf:discussion-summary v=1 key=conclusion -->"
		if got := ParseCommand(body); got.Kind != KindNone {
			t.Fatalf("discussion summary parsed as command: %+v", got)
		}
		if _, err := db.RetainOwnerInstruction(t.Context(), session.ID, 123, 42, body, store.OwnerInputTarget{}); err == nil {
			t.Fatal("discussion summary admitted as owner instruction")
		}
	}
}
