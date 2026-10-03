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

func TestBugResultBoundaryLocalBrokerCommunicationOnly(t *testing.T) {
	db, _, options, _ := bugResultBoundaryFixture(t)
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
