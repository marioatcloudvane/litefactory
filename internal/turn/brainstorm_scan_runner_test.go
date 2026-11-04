package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/brainstorm"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func brainstormTestTeam() brainstorm.TeamCapture {
	return brainstorm.TeamCapture{Version: "fixture/team@1.0.0", Role: "---\nname: research\n---\nROLE BODY FIXTURE\n", Files: map[string]string{
		"skills/research-evidence/SKILL.md":    "---\nname: research-evidence\ndescription: fixture\nresources: [evidence.md]\n---\nEVIDENCE BODY FIXTURE\n",
		"skills/research-evidence/evidence.md": "EVIDENCE RESOURCE FIXTURE\n",
		"skills/brainstorm/SKILL.md":           "---\nname: brainstorm\ndescription: fixture\n---\nBRAINSTORM BODY FIXTURE\n",
	}}
}

func TestBrainstormScanInstructionsUseCapturedRoleAndSkills(t *testing.T) {
	prompt, err := brainstormScanInstructions(brainstormTestTeam())
	if err != nil {
		t.Fatal(err)
	}
	order := []string{"ROLE BODY FIXTURE", "EVIDENCE BODY FIXTURE", "EVIDENCE RESOURCE FIXTURE", "BRAINSTORM BODY FIXTURE"}
	last := -1
	for _, want := range order {
		at := strings.Index(prompt, want)
		if at <= last {
			t.Fatalf("%q missing or out of order in %q", want, prompt)
		}
		last = at
	}
	if strings.Contains(prompt, "name: research") {
		t.Fatalf("role header leaked: %q", prompt)
	}
	broken := brainstormTestTeam()
	delete(broken.Files, "skills/brainstorm/SKILL.md")
	if _, err = brainstormScanInstructions(broken); err == nil {
		t.Fatal("missing brainstorm skill accepted")
	}
}

type controlledBrainstormProvider struct {
	calls      []agent.JudgeRequest
	readme     string
	contextErr error
	answer     string
}

func (p *controlledBrainstormProvider) RunJudge(ctx context.Context, q agent.JudgeRequest) (agent.JudgeRunResult, error) {
	p.calls = append(p.calls, q)
	if err := q.Validate(); err != nil {
		return agent.JudgeRunResult{}, err
	}
	raw, err := os.ReadFile(filepath.Join(q.Invocation.WorkingDirectory, "README.md"))
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	p.readme = string(raw)
	index := filepath.Join(q.Invocation.ReadableDirectories[0], "brainstorm", "index.json")
	if info, e := os.Stat(index); e != nil || info.Mode().Perm()&0o222 != 0 {
		p.contextErr = fmt.Errorf("context file not read-only: %v %w", info, e)
	} else if e = os.WriteFile(filepath.Join(filepath.Dir(index), "new.json"), []byte("{}"), 0o600); e == nil {
		p.contextErr = errors.New("context directory is writable")
	}
	client := submission.Client{DescriptorPath: q.Invocation.MCPLaunch.DescriptorPath()}
	call := func(name string, input string) (submission.Response, error) {
		response, e := client.Call(ctx, name, json.RawMessage(input))
		if e == nil && response.Error != "" {
			e = errors.New(response.Error)
		}
		return response, e
	}
	result := func(comment string) string {
		return `{"result":{"outcome":"completed","comment":` + strconvQuote(comment) + `,"payload":{"research":{"request":"q","findings":[{"id":"F1","text":"two retries","evidence":[{"path":"README.md","line":1}]}]}}}}`
	}
	response, err := call("communication_preflight", result("See https://example.com"))
	if err != nil {
		return agent.JudgeRunResult{}, err
	}
	var d agentresult.Diagnostics
	if err = json.Unmarshal(response.Result, &d); err != nil || d.Valid {
		return agent.JudgeRunResult{}, fmt.Errorf("preflight accepted a URL: %s", response.Result)
	}
	if _, err = call("communication_submit", result("See https://example.com")); err == nil {
		return agent.JudgeRunResult{}, errors.New("invalid submit accepted")
	}
	if response, err = call("communication_submit", result(p.answer)); err != nil {
		return agent.JudgeRunResult{}, err
	}
	if _, err = call("communication_receipt", `{"request_id":`+strconvQuote(response.RequestID)+`}`); err != nil {
		return agent.JudgeRunResult{}, err
	}
	return agent.JudgeRunResult{ObservedModel: q.Invocation.Model, ProviderVersion: "controlled"}, errors.New("controlled lost provider completion reply")
}

func strconvQuote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func TestBrainstormScanRunnerReadOnlyRunAndReceiptRecovery(t *testing.T) {
	runCtx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	root, err := os.MkdirTemp("/tmp", "lf-bscan-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(t.Context(), filepath.Join(root, "scan.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	fixture := gittest.New(t)
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	clone, err := binary.Open(t.Context(), fixture.Dir)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := clone.RevParse(t.Context(), "refs/heads/"+fixture.Branch()+"^{commit}", git.RevParseOptions{})
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(fixture.Dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.WriteFile("README.md", "Uncommitted bytes stay in the source checkout.\n")
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: fixture.Dir, Forge: store.ForgeGitHub, Owner: "example", Name: "scan", RemoteURL: "git@github.com:example/scan.git", BaseBranch: fixture.Branch(), FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = db.UpsertIdentity(t.Context(), store.Identity{Forge: store.ForgeGitHub, Role: store.RoleOwner, UserID: 42, Handle: "owner", Source: store.SourceGH}); err != nil {
		t.Fatal(err)
	}
	owner, err := db.ConnectorOwner(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	client := store.ConnectorClient{ID: "scan-client", Name: "Controlled client", AuthMethod: "none", Redirects: []string{"https://client.test/callback"}, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err = db.RegisterConnectorClient(t.Context(), client, now); err != nil {
		t.Fatal(err)
	}
	browser := store.ConnectorBrowser{ID: "scan-browser", ClientID: client.ID, RedirectURI: client.Redirects[0], State: "state", Resource: "https://factory.test/litefactory/mcp", Scope: brainstorm.Scope, PKCEChallenge: "challenge", NonceDigest: "nonce", UpstreamStateDigest: "upstream", OwnerGeneration: owner.Generation, ExpiresAt: now.Add(time.Minute)}
	if err = db.CreateConnectorBrowser(t.Context(), browser, 42, now); err != nil {
		t.Fatal(err)
	}
	if err = db.VerifyConnectorBrowser(t.Context(), browser.ID, "nonce", "upstream", 42, now); err != nil {
		t.Fatal(err)
	}
	ref := "github:example/scan"
	grant := store.ConnectorGrant{ID: "scan-grant", ClientID: client.ID, Resource: browser.Resource, Scope: browser.Scope, OwnerID: 42, OwnerGeneration: owner.Generation, CreatedAt: now, ExpiresAt: now.Add(time.Hour), Coverage: &store.ConnectorCoverage{Repositories: []store.ConnectorRepository{{RepoID: repo.ID, RepositoryRef: ref}}}}
	code := store.ConnectorCode{Digest: "scan-code", GrantID: grant.ID, ClientID: client.ID, RedirectURI: browser.RedirectURI, Resource: browser.Resource, Scope: browser.Scope, PKCEChallenge: browser.PKCEChallenge, ExpiresAt: now.Add(time.Minute)}
	if err = db.ConsentConnectorBrowser(t.Context(), browser.ID, "nonce", 42, now, grant, code, true); err != nil {
		t.Fatal(err)
	}
	o := store.BrainstormOwner{OwnerID: 42, OwnerGeneration: owner.Generation}
	b, _, err := db.OpenBrainstorm(t.Context(), store.BrainstormRecord{BrainstormOwner: o, ID: "b1", RepositoryRef: ref, GrantID: grant.ID}, now)
	if err != nil {
		t.Fatal(err)
	}
	team := brainstormTestTeam()
	files, err := json.Marshal(team)
	if err != nil {
		t.Fatal(err)
	}
	scan, _, err := db.AdmitBrainstormScan(t.Context(), store.BrainstormScan{BrainstormOwner: o, ID: "scan-1", BrainstormID: b.ID, RequestKey: "ask-1", Digest: "d", Question: "How do retries work?", TeamVersion: team.Version, TeamFiles: string(files), GrantID: grant.ID, ConsentRevision: "{}", JobID: "scan-job", ExecutionID: "scan-execution"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = db.ClaimWorkflowAttempt(t.Context(), now, time.Minute); err != nil {
		t.Fatal(err)
	}
	provider := &controlledBrainstormProvider{answer: "It retries twice, then stops."}
	runner := &BrainstormScanRunner{DB: db, Paths: config.NewPaths(root), Executable: "/usr/bin/false", Providers: map[agent.Kind]agent.JudgeRunner{agent.Kind("codex"): provider}, Git: binary, Provider: agent.Kind("codex"), Model: "scan-model", Thinking: "high", Deadline: time.Minute}
	request := brainstorm.RunRequest{ScanID: scan.ID, JobID: scan.JobID, ExecutionID: scan.ExecutionID, BrainstormID: b.ID, RepositoryRef: ref, Question: scan.Question, Team: team, Digest: json.RawMessage(`{"newest_entries":[]}`), ContextFiles: map[string][]byte{"index.json": []byte(`{"counts":{}}`)}}
	wrong := request
	wrong.Team.Role = "substituted role"
	if _, err = runner.Run(runCtx, wrong); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("substituted team: %v", err)
	}
	result, err := runner.Run(runCtx, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Answer != provider.answer || result.Commit != commit || len(result.Research) == 0 {
		t.Fatalf("result = %+v want commit %s", result, commit)
	}
	if len(provider.calls) != 1 || provider.readme != string(committed) || provider.contextErr != nil {
		t.Fatalf("calls=%d readme=%q context=%v", len(provider.calls), provider.readme, provider.contextErr)
	}
	call := provider.calls[0]
	if !strings.HasPrefix(call.Invocation.Prompt, "ROLE BODY FIXTURE") || !strings.Contains(call.Invocation.Prompt, "BRAINSTORM BODY FIXTURE") || !strings.Contains(call.Invocation.Prompt, commit) || !strings.Contains(call.Invocation.Prompt, "How do retries work?") {
		t.Fatalf("prompt = %q", call.Invocation.Prompt)
	}
	meter, err := db.GetMeterAttempt(t.Context(), call.Invocation.UsageAttemptID)
	if err != nil || meter.Outcome != "failure" {
		t.Fatalf("meter = %+v %v", meter, err)
	}
	if _, err = os.Stat(call.Invocation.WorkingDirectory); !os.IsNotExist(err) {
		t.Fatal("temporary checkout was retained")
	}
	if _, err = os.Stat(filepath.Dir(call.Invocation.ReadableDirectories[0])); !os.IsNotExist(err) {
		t.Fatal("temporary workspace was retained")
	}
	recovered, found, err := runner.Recover(t.Context(), request)
	if err != nil || !found || recovered.Answer != result.Answer || recovered.Commit != commit {
		t.Fatalf("recover = %+v %v %v", recovered, found, err)
	}
	again, err := runner.Run(runCtx, request)
	if err != nil || again.Answer != result.Answer || len(provider.calls) != 1 {
		t.Fatalf("rerun after saved result: calls=%d %v", len(provider.calls), err)
	}
}
