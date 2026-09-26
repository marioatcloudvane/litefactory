package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func bugResultBoundaryFixture(t *testing.T) (*store.DB, store.Session, AgentResultServiceOptions, *ar.Context) {
	t.Helper()
	root := t.TempDir()
	db, err := store.Open(t.Context(), filepath.Join(root, "bug.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(t.Context(), store.Repo{Path: root, Forge: store.ForgeGitHub, Owner: "example", Name: "bugs", RemoteURL: "git@github.com:example/bugs.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: filepath.Join(root, "worktrees")})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 391)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitWorkflow(t.Context(), store.TaskWorkflowAdmission{SessionID: session.ID, Version: 1, Family: "standalone", Workflow: "bug"}); err != nil {
		t.Fatal(err)
	}
	if err = db.EnsureBugRecord(t.Context(), store.BugIdentity{SessionID: session.ID, Repository: "example/bugs", IssueID: 10391, IssueNumber: 391, OwnerID: 42}); err != nil {
		t.Fatal(err)
	}
	seg, err := db.GetOrCreateSegment(t.Context(), session.ID, 1, "bug-results", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: seg.ID, Workflow: "bug", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(n int) string { return fmt.Sprintf("bug-%d.log", n) }})
	if err != nil {
		t.Fatal(err)
	}
	trusted := &ar.Context{Workflow: agent.WorkflowBug, SchemaVersion: 1, WorkflowVersion: 2, QuestionsAllowed: true, Bug: &ar.BugBinding{Role: "bug-analyst", Invocation: "analysis-1", BaseRevision: 0}}
	options := AgentResultServiceOptions{DB: db, Attempt: store.ResultAttempt{Task: session.TaskRef, Workflow: "bug", WorkContext: "bug-analysis-1", Attempt: "analyst-attempt-1", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2}, ReadTargets: ar.EditTargets("bug"), EditTargets: ar.EditTargets("bug"), Resolve: func(context.Context) (ar.Context, error) { return *trusted, nil }}
	return db, session, options, trusted
}

func bugResultBoundaryBlocked() []byte {
	return []byte(`{"result":{"outcome":"blocked","reason":"Reproduction requires owner evidence","comment":"Investigation stopped with retained evidence","payload":{"bug":{"response":"blocked","invocation":"analysis-1","base_revision":0,"evidence":["owner report unavailable"]}}}}`)
}

func TestBugResultBoundaryServiceReceiptRecoveryAndCorrection(t *testing.T) {
	db, _, options, _ := bugResultBoundaryFixture(t)
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	data := bugResultBoundaryBlocked()
	diagnostics, err := service.Preflight(t.Context(), data)
	if err != nil || !diagnostics.Valid {
		t.Fatalf("preflight: %+v %v", diagnostics, err)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || len(revisions) != 0 {
		t.Fatalf("preflight retained result: %v %v", revisions, err)
	}
	submitted, err := service.Submit(t.Context(), "request-1", data)
	if err != nil || submitted.Receipt == nil || !submitted.Validation.Valid || submitted.Snapshot == nil {
		t.Fatalf("submit: %+v %v", submitted, err)
	}
	if submitted.Snapshot.Meta.Workflow != agent.WorkflowBug || submitted.Snapshot.Meta.Mode != "" {
		t.Fatalf("Bug lost workflow identity: %+v", submitted.Snapshot.Meta)
	}
	reader, err := OpenAgentResultReader(t.Context(), db, service.Binding(), ar.EditTargets("bug"))
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := reader.Receipt(t.Context(), options.Attempt.RunID, "request-1")
	if err != nil || recovered.Receipt == nil || recovered.Receipt.ID != submitted.Receipt.ID {
		t.Fatalf("lost response recovery: %+v %v", recovered, err)
	}
	replay, err := service.Submit(t.Context(), "request-1", data)
	if err != nil || replay.Receipt.ID != submitted.Receipt.ID {
		t.Fatalf("exact replay: %+v %v", replay, err)
	}
	if _, err := service.Submit(t.Context(), "request-1", append(bytes.Clone(data), ' ')); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatalf("changed bytes reused request: %v", err)
	}
	update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Retained obstacle clarified"}]}`)
	if d, err := service.Preflight(t.Context(), update); err != nil || !d.Valid {
		t.Fatalf("corrected preflight: %+v %v", d, err)
	}
	corrected, err := service.Submit(t.Context(), "request-2", update)
	if err != nil || corrected.Snapshot == nil || corrected.Snapshot.Meta.ResultRevision != 2 {
		t.Fatalf("corrected submission: %+v %v", corrected, err)
	}
	for _, response := range []AgentResultSubmission{submitted, recovered, corrected} {
		if response.Receipt.Latest.Input != "accepted" || response.Receipt.Latest.Engineering != "pending" || response.Receipt.Latest.Acceptance != "pending" || response.Receipt.Latest.Publication != "pending" {
			t.Fatalf("receipt created workflow authority: %+v", response.Receipt.Latest)
		}
	}
	old, err := reader.Receipt(t.Context(), options.Attempt.RunID, "request-1")
	if err != nil || !reflect.DeepEqual(old.Snapshot, submitted.Snapshot) {
		t.Fatalf("correction rewrote history: %+v %v", old, err)
	}
}

func TestBugResultBoundaryServiceRejectsMismatchedAuthority(t *testing.T) {
	for _, fault := range []string{"task", "role", "invocation", "revision", "mode", "unknown field", "self approval", "version", "duplicate member"} {
		t.Run(fault, func(t *testing.T) {
			_, _, options, trusted := bugResultBoundaryFixture(t)
			if fault == "task" {
				options.Attempt.Task = "github:example/bugs#392"
			}
			if fault == "role" {
				trusted.Bug.Role = "bug-fixer"
			}
			if fault == "mode" {
				trusted.Mode = "research"
			}
			service, err := BindAgentResultService(t.Context(), options)
			if fault == "task" || fault == "role" || fault == "mode" {
				if err == nil {
					t.Fatal("invalid service authority bound")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			raw := string(bugResultBoundaryBlocked())
			switch fault {
			case "invocation":
				trusted.Bug.Invocation = "analysis-other"
			case "revision":
				trusted.Bug.BaseRevision = 1
			case "unknown field":
				raw = strings.Replace(raw, `"base_revision":0`, `"base_revision":0,"analysis_used":0`, 1)
			case "self approval":
				raw = `{"result":{"outcome":"completed","comment":"approved","payload":{"bug":{"response":"approved","invocation":"analysis-1","base_revision":0,"approval":{"revision":1,"digest":"` + strings.Repeat("a", 64) + `","review":"review-1"}}}}}`
			case "version":
				raw = strings.Replace(raw, `{"result":`, `{"schema_version":2,"result":`, 1)
			case "duplicate member":
				raw = strings.Replace(raw, `"base_revision":0`, `"base_revision":0,"base_revision":0`, 1)
			}
			d, err := service.Preflight(t.Context(), []byte(raw))
			if err == nil && d.Valid {
				t.Fatalf("invalid %s preflight passed", fault)
			}
			response, err := service.Submit(t.Context(), "invalid", []byte(raw))
			if err == nil && (response.Validation.Valid || response.Snapshot != nil) {
				t.Fatalf("invalid %s retained successful snapshot: %+v", fault, response)
			}
		})
	}
}

func TestBugResultBoundaryReceiptAndTerminalCannotApplyWithoutSuccessfulInvocation(t *testing.T) {
	db, session, options, _ := bugResultBoundaryFixture(t)
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	definition, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "analyst", StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: options.Attempt.RunID, StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	pass := store.BugAnalysisPass{SessionID: session.ID, Invocation: "analysis-1", InputDigest: strings.Repeat("b", 64), GraphAttemptID: attempt.ID}
	if _, err = db.ReserveBugAnalysisPass(t.Context(), pass, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimBugAnalysisDispatch(t.Context(), session.ID, pass.Invocation, pass.InputDigest, options.Attempt.RunID); err != nil {
		t.Fatal(err)
	}
	data := bugResultBoundaryBlocked()
	terminal := []byte(`{"version":1,"response":"blocked","result":` + strings.TrimSuffix(strings.TrimPrefix(string(data), `{"result":`), `}`) + `}`)
	// Exercise the real retained-result boundary with the production Bug decoder;
	// this does not claim the complete Bug workflow registry is ready.
	activity := AgentActivity{Policy: WorkflowPolicy{Workflow: agent.WorkflowBug, Decode: ar.DecodeBugNode}}
	if _, _, err := activity.acceptRetained(t.Context(), service, string(terminal)); !errors.Is(err, errNoRetainedResult) {
		t.Fatalf("terminal-only retained boundary: %v", err)
	}
	application := store.BugAnalysisApplication{SessionID: session.ID, ExpectedRevision: 2, Invocation: pass.Invocation, Operation: "record-1", InputDigest: strings.Repeat("c", 64), ResultID: service.Binding().ResultID, ResultRevision: 1}
	if _, err := db.ApplyBugAnalysis(t.Context(), application); err == nil {
		t.Fatal("terminal-only output applied analysis")
	}
	submitted, err := service.Submit(t.Context(), "request-1", data)
	if err != nil || !submitted.Validation.Valid {
		t.Fatalf("submit: %+v %v", submitted, err)
	}
	if _, err := service.Receipt(t.Context(), options.Attempt.RunID, "request-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ApplyBugAnalysis(t.Context(), application); err == nil {
		t.Fatal("receipt without process success applied analysis")
	}
	if _, _, err := activity.acceptRetained(t.Context(), service, ""); err == nil {
		t.Fatal("receipt-only result lacked terminal consistency check")
	}
	if _, revision, err := activity.acceptRetained(t.Context(), service, string(terminal)); err != nil || revision != 1 {
		t.Fatalf("matching retained confirmation: %d %v", revision, err)
	}

	exit := int64(0)
	if _, err := db.FinishRun(t.Context(), options.Attempt.RunID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exit}); err != nil {
		t.Fatal(err)
	}
	if err := db.FinishBugAnalysisPass(t.Context(), session.ID, pass.Invocation, "succeeded", ""); err != nil {
		t.Fatal(err)
	}
	application.ExpectedRevision = 3
	if _, err := db.ApplyBugAnalysis(t.Context(), application); err == nil {
		t.Fatal("process success without graph result consistency applied analysis")
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: time.Now(), Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "analyst", ToNode: "record", OnOutcome: "success", At: time.Now()}, Outputs: []store.GraphOutput{{Kind: "json", Payload: []byte(fmt.Sprintf(`{"result_id":%d,"result_revision":1}`, service.Binding().ResultID))}}}); err != nil {
		t.Fatal(err)
	}
	revision, err := db.ApplyBugAnalysis(t.Context(), application)
	if err != nil {
		t.Fatal(err)
	}
	if revision.ReviewReady || revision.Response != "blocked" || revision.Reason != "Reproduction requires owner evidence" || !reflect.DeepEqual(revision.Evidence, []string{"owner report unavailable"}) {
		t.Fatalf("blocked-without-analysis lost evidence: %+v", revision)
	}
	state, _, approval, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil || approval != nil || state.AnalysisUsed != 1 {
		t.Fatalf("blocked receipt authorized repair: %+v %+v %v", state, approval, err)
	}
	// Exercise the concrete record/control handlers against the same durable
	// blocked result. A successful process/graph is not authority to approve.
	profile := &graphexec.Profile{Workflow: agent.WorkflowBug, Digest: definition.Digest, Nodes: []graphexec.Node{
		{Name: "record", Action: BugRecordAction},
		{Name: "gate", Action: graphexec.ResearchIfAction, Attrs: map[string]string{"lf_field": "response", "lf_condition": "approved", "lf_source": "record"}},
	}}
	recordAttempt, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "record", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	record := BugRecordActivity{Profile: profile, Resolve: func(context.Context, ActivityRequest) (BugRecordInput, error) {
		return BugRecordInput{Identity: store.BugIdentity{SessionID: session.ID, Repository: "example/bugs", IssueID: 10391, IssueNumber: 391, OwnerID: 42}, Application: &application}, nil
	}}
	recorded, err := record.Execute(t.Context(), Options{DB: db}, ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, NodeAttemptID: recordAttempt.ID, Node: "record"})
	if err != nil || recorded.Outcome != "blocked" {
		t.Fatalf("record outcome: %+v %v", recorded, err)
	}
	if err := db.CompleteGraphNodeAttempt(t.Context(), recordAttempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "blocked", FinishedAt: time.Now(), Outputs: []store.GraphOutput{{Kind: recorded.Kind, Payload: recorded.Output}}, Transition: &store.GraphTransition{InstanceID: instance.ID, FromNode: "record", ToNode: "gate", OnOutcome: "blocked", At: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	gateAttempt, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "gate", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	control := bugAnalysisControl{profile: profile, policy: graphexec.ControlPolicy{Workflow: agent.WorkflowBug, Responses: []string{"ask", "done", "approved", "blocked", "exhausted"}, Ask: "ask", Approved: "approved"}}
	gateRequest := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, NodeAttemptID: gateAttempt.ID, Node: "gate"}
	decision, err := control.Execute(t.Context(), Options{DB: db}, gateRequest)
	if err != nil || decision.Outcome != "false" {
		t.Fatalf("blocked record authorized repair: %+v %v", decision, err)
	}
	profile.Nodes[1].Attrs["lf_source"] = "analyst"
	if _, err := control.Execute(t.Context(), Options{DB: db}, gateRequest); err == nil {
		t.Fatal("control accepted non-record source")
	}

}

func TestBugResultBoundaryLocalBrokerCommunicationOnly(t *testing.T) {
	db, session, options, _ := bugResultBoundaryFixture(t)
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	runtime := &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}
	prepared := &preparedMCP{runtime: runtime, service: service}
	spec, err := prepareBugResultBroker(t.Context(), prepared, options.Attempt.Attempt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := prepared.close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	if spec == nil {
		t.Fatal("Bug broker did not supply launch binding")
	}
	info, err := os.Stat(filepath.Join(filepath.Dir(spec.DescriptorPath()), "s"))
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("not an actual local socket: %v %v", info, err)
	}
	client := submission.Client{DescriptorPath: spec.DescriptorPath()}
	list, err := client.List(t.Context())
	if err != nil || list.Error != "" {
		t.Fatalf("socket discovery: %+v %v", list, err)
	}
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	if err := json.Unmarshal(list.Result, &catalog); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range catalog.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	wanted := []string{"communication_preflight", "communication_receipt", "communication_submit"}
	if !reflect.DeepEqual(names, wanted) {
		t.Fatalf("Bug socket exposed extra capabilities: %v", names)
	}
	permitted := spec.PermittedTools()
	slices.Sort(permitted)
	if !reflect.DeepEqual(permitted, wanted) {
		t.Fatalf("launch profile differs from socket: %v", permitted)
	}
	for _, name := range []string{"issues_list", "issues_get", "issues_comment", "evidence_submit", "receipts_get", "activity_update", "create_issue", "merge", "close", "shell"} {
		args := []byte(`{}`)
		if name == "receipts_get" {
			args = []byte(`{"request_id":"unavailable-family-receipt"}`)
		}
		denied, err := client.Call(t.Context(), name, args)
		if err != nil {
			t.Fatal(err)
		}
		if denied.Error == "" {
			t.Fatalf("socket permitted forbidden %s: %+v", name, denied)
		}
	}
	data := bugResultBoundaryBlocked()
	preflight := mcpToolCall(t, client, "communication_preflight", data)
	var diagnostics ar.UpdateDiagnostics
	if err := json.Unmarshal(preflight.Result, &diagnostics); err != nil || !diagnostics.Valid || !diagnostics.Complete {
		t.Fatalf("socket preflight: %+v %v", diagnostics, err)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || len(revisions) != 0 {
		t.Fatalf("socket preflight persisted input: %v %v", revisions, err)
	}
	first := mcpToolCall(t, client, "communication_submit", data)
	submitted := mcpDecodeSubmission(t, first)
	if first.RequestID == "" || submitted.Receipt == nil || submitted.Snapshot == nil || !submitted.Validation.Valid {
		t.Fatalf("socket submit: %+v", submitted)
	}
	// A fresh client recovers the same durable receipt after the caller discards
	// its submission response. Replaying identical bytes reuses the journal ID.
	recoveredClient := submission.Client{DescriptorPath: spec.DescriptorPath()}
	recovered := mcpDecodeSubmission(t, mcpToolCall(t, recoveredClient, "communication_receipt", mcpReceiptArgs(t, first.RequestID)))
	replay := mcpToolCall(t, recoveredClient, "communication_submit", data)
	if recovered.Receipt == nil || recovered.Receipt.ID != submitted.Receipt.ID || replay.RequestID != first.RequestID {
		t.Fatalf("socket replay changed identity: %+v %+v", recovered, replay)
	}
	if recovered.Receipt.Latest.Input != "accepted" || recovered.Receipt.Latest.Engineering != "pending" || recovered.Receipt.Latest.Acceptance != "pending" || recovered.Receipt.Latest.Publication != "pending" {
		t.Fatalf("socket acknowledgment granted completion: %+v", recovered.Receipt.Latest)
	}
	state, _, approval, err := db.CurrentBugAnalysis(t.Context(), session.ID)
	if err != nil || state.AnalysisUsed != 0 || state.CurrentAnalysis != 0 || approval != nil {
		t.Fatalf("socket tool performed domain effects: %+v %+v %v", state, approval, err)
	}
	if err := prepared.guard.Revoke(t.Context(), "cancelled"); err != nil {
		t.Fatal(err)
	}
	denied, err := client.Call(t.Context(), "communication_preflight", data)
	if err != nil {
		t.Fatal(err)
	}
	if denied.Error == "" {
		t.Fatal("revoked socket retained result authority")
	}
}

func TestBugResultBoundaryAnalystReservationAndRecovery(t *testing.T) {
	for _, variant := range []string{"success", "resume refusal", "failed", "cancelled", "uncertain", "missing cold context"} {
		t.Run(variant, func(t *testing.T) {
			db, session, options, _ := bugResultBoundaryFixture(t)
			now := time.Now()
			definition, err := db.CreateGraphDefinition(t.Context(), store.GraphDefinition{Team: "builtin", Workflow: "bug", ProfileVersion: 1, Path: "bug/bug.dot", Digest: strings.Repeat("a", 64), CreatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			instance, err := db.CreateGraphInstance(t.Context(), store.GraphInstance{SessionID: session.ID, DefinitionID: definition.ID, CurrentNode: "analyst", StartedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			attempt, err := db.StartGraphNodeAttempt(t.Context(), store.GraphAttemptStart{InstanceID: instance.ID, Node: "analyst", RunID: options.Attempt.RunID, StartedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			run, err := db.GetRun(t.Context(), options.Attempt.RunID)
			if err != nil {
				t.Fatal(err)
			}
			segment, err := db.GetSegment(t.Context(), run.SegmentID)
			if err != nil {
				t.Fatal(err)
			}
			authority, err := NewAttemptAuthority(t.Context(), db)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = authority.Close(context.WithoutCancel(t.Context())) })
			worktree := t.TempDir()
			if err := os.MkdirAll(filepath.Join(worktree, ".aiteam/session"), 0700); err != nil {
				t.Fatal(err)
			}
			ref, err := naming.ParseRef(session.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var service *AgentResultService
			var retainedContext string
			var calls []agent.Request
			runner := newStubRunner(func(ctx context.Context, request agent.Request) (*agent.RunResult, error) {
				calls = append(calls, request)
				pass, err := db.GetBugAnalysisPass(ctx, session.ID, "analysis-1")
				if err != nil || pass.Ordinal != 1 || pass.State != "dispatched" || pass.RunID != run.ID {
					t.Fatalf("runner launched before durable reservation/claim: %+v %v", pass, err)
				}
				if request.Workflow != agent.WorkflowBug || request.Mode != "" || request.Permissions.Profile != "bug-analyst" || !strings.Contains(request.Prompt, retainedContext) {
					t.Fatalf("incorrect Bug context: %+v", request)
				}
				switch variant {
				case "resume refusal":
					if len(calls) == 1 {
						return nil, agent.ErrSessionNotResumable
					}
				case "failed":
					return &agent.RunResult{Outcome: agent.OutcomeFailed, Reason: "controlled process failure"}, nil
				case "cancelled":
					cancel()
					return nil, context.Canceled
				case "uncertain":
					return nil, errors.New("controlled process state unknown")
				}
				data := bugResultBoundaryBlocked()
				if d, err := service.Preflight(ctx, data); err != nil || !d.Valid {
					t.Fatalf("runner preflight: %+v %v", d, err)
				}
				response, err := service.Submit(ctx, "runner-result", data)
				if err != nil || !response.Validation.Valid {
					t.Fatalf("runner submit: %+v %v", response, err)
				}
				if _, err := service.Receipt(ctx, run.ID, "runner-result"); err != nil {
					t.Fatal(err)
				}
				terminal := `{"version":1,"response":"blocked","result":` + strings.TrimSuffix(strings.TrimPrefix(string(data), `{"result":`), `}`) + `}`
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: terminal}, nil
			})
			prepareCalls := 0
			handler := BugAnalystActivity{Resolve: func(context.Context, ActivityRequest) (BugAnalystInput, error) {
				return BugAnalystInput{SessionID: session.ID, ExpectedRevision: 1, Invocation: "analysis-1", InputDigest: strings.Repeat("b", 64)}, nil
			}, Prepare: func(ctx context.Context, _ Options, _ ActivityRequest, scope ar.Context, retained string) (ResearchInvocation, error) {
				prepareCalls++
				retainedContext = retained
				state, _, _, err := db.CurrentBugAnalysis(ctx, session.ID)
				if err != nil || state.AnalysisUsed != 1 {
					t.Fatalf("prepare before reservation: %+v %v", state, err)
				}
				options.Resolve = func(context.Context) (ar.Context, error) { return scope, nil }
				service, err = BindAgentResultService(ctx, options)
				if err != nil {
					return ResearchInvocation{}, err
				}
				prepared := &preparedMCP{runtime: &MCPRuntime{Authority: authority, Paths: config.NewPaths(launchFixtureDirectory(t)), Executable: "/factory/litefactory", Now: time.Now}, service: service}
				spec, err := prepareBugResultBroker(ctx, prepared, options.Attempt.Attempt)
				if err != nil {
					return ResearchInvocation{}, err
				}
				t.Cleanup(func() { _ = prepared.close(context.WithoutCancel(t.Context())) })
				request := agent.Request{Workflow: agent.WorkflowBug, Invocation: agent.InvocationResearchNode, MCPLaunch: spec, Ref: ref, RunOrdinal: run.Ordinal, SegmentOrdinal: segment.Ordinal, Session: agent.SessionRef{Ref: segment.SessionRef, Resume: segment.ResumeToken}, WorktreePath: worktree, Prompt: "Investigate retained context\n" + retained, Permissions: agent.Permissions{Profile: "bug-analyst"}}
				cold := request
				cold.Prompt = "Cold retained context\n" + retained
				if variant == "resume refusal" {
					request.Path = agent.PathResume
				}
				if variant == "missing cold context" {
					cold.Prompt = "retained context omitted"
				}
				return ResearchInvocation{Request: request, ColdRequest: &cold, Kind: agent.Kind(store.AgentClaude), SessionPath: ".aiteam/session", Results: service}, nil
			}}
			activity := ActivityRequest{Workflow: agent.WorkflowBug, InstanceID: instance.ID, NodeAttemptID: attempt.ID, RunID: run.ID, Node: "analyst"}
			out, executeErr := handler.Execute(ctx, Options{DB: db, Runners: runnerSet(runner)}, activity)
			success := variant == "success" || variant == "resume refusal"
			if (executeErr == nil) != success {
				t.Fatalf("activity outcome: %+v %v; run=%+v definition=%+v attempt=%+v", out, executeErr, run, definition, attempt)
			}
			wantCalls := 1
			if variant == "resume refusal" {
				wantCalls = 2
			}
			if variant == "missing cold context" {
				wantCalls = 0
			}
			if len(calls) != wantCalls || prepareCalls != 1 {
				t.Fatalf("calls=%d preparations=%d", len(calls), prepareCalls)
			}
			pass, err := db.GetBugAnalysisPass(t.Context(), session.ID, "analysis-1")
			if err != nil {
				t.Fatal(err)
			}
			wantState := "failed"
			if success {
				wantState = "dispatched"
			}
			if variant == "cancelled" {
				wantState = "cancelled"
			}
			if pass.State != wantState || pass.Ordinal != 1 {
				t.Fatalf("pass state %+v want %s", pass, wantState)
			}
			if success {
				if err := ReconcileBugAnalystSuccess(t.Context(), db, session.ID, pass.Invocation); err == nil {
					t.Fatal("receipt alone reconciled unfinished run")
				}
				exit := int64(0)
				if _, err := db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &exit}); err != nil {
					t.Fatal(err)
				}
				if err := ReconcileBugAnalystSuccess(t.Context(), db, session.ID, pass.Invocation); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := handler.Execute(t.Context(), Options{DB: db, Runners: runnerSet(runner)}, activity); err == nil {
				t.Fatal("replay blindly relaunched terminal/dispatched work")
			}
			if len(calls) != wantCalls || prepareCalls != 1 {
				t.Fatal("replay invoked preparation or runner")
			}
			state, _, approval, err := db.CurrentBugAnalysis(t.Context(), session.ID)
			if err != nil || state.AnalysisUsed != 1 || approval != nil {
				t.Fatalf("outcome refunded pass or granted approval: %+v %+v %v", state, approval, err)
			}
		})
	}
}
