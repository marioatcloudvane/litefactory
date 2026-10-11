package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	ar "github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// countedMCPService instruments the real service rather than replacing storage
// or validation. Mutex protects independent private-socket workers.
type countedMCPService struct {
	*AgentResultService
	mu                         sync.Mutex
	preflight, submit, receipt int
	payloads                   [][]byte
	// submitErr and receiptErr force a handler failure without touching storage.
	submitErr, receiptErr error
}

func (s *countedMCPService) Preflight(ctx context.Context, data []byte) (ar.UpdateDiagnostics, error) {
	s.mu.Lock()
	s.preflight++
	s.payloads = append(s.payloads, bytes.Clone(data))
	s.mu.Unlock()
	return s.AgentResultService.Preflight(ctx, data)
}
func (s *countedMCPService) Submit(ctx context.Context, id string, data []byte) (AgentResultSubmission, error) {
	s.mu.Lock()
	s.submit++
	s.payloads = append(s.payloads, bytes.Clone(data))
	forced := s.submitErr
	s.mu.Unlock()
	if forced != nil {
		return AgentResultSubmission{}, forced
	}
	return s.AgentResultService.Submit(ctx, id, data)
}
func (s *countedMCPService) Receipt(ctx context.Context, run int64, id string) (AgentResultSubmission, error) {
	s.mu.Lock()
	s.receipt++
	forced := s.receiptErr
	s.mu.Unlock()
	if forced != nil {
		return AgentResultSubmission{}, forced
	}
	return s.AgentResultService.Receipt(ctx, run, id)
}
func (s *countedMCPService) counts() (int, int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preflight, s.submit, s.receipt
}

func mcpToolFixture(t *testing.T, db *store.DB, service *AgentResultService) (submission.Client, *AttemptGuard, *AttemptAuthority, *countedMCPService) {
	t.Helper()
	authority, err := NewAttemptAuthority(t.Context(), db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := authority.Close(context.WithoutCancel(t.Context())); err != nil {
			t.Error(err)
		}
	})
	guard, err := authority.Bind(t.Context(), service.Binding(), service.MCPPermissionDigest(), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedMCPService{AgentResultService: service}
	dispatch, err := NewToolDispatcher(guard, counted)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("/tmp", "lf-tools-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	broker, err := submission.Start(t.Context(), submission.Options{Directory: dir, AllowTools: true, MaxWorkers: 4, RequestTimeout: 5 * time.Second, Handler: dispatch.Handler})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := broker.Close(); err != nil {
			t.Error(err)
		}
	})
	return submission.Client{DescriptorPath: broker.DescriptorPath()}, guard, authority, counted
}
func mcpToolCall(t *testing.T, c submission.Client, name string, data []byte) submission.Response {
	t.Helper()
	r, err := c.Call(t.Context(), name, data)
	if err != nil || r.Error != "" {
		t.Fatalf("%s: response=%+v error=%v", name, r, err)
	}
	return r
}
func mcpDecodeSubmission(t *testing.T, r submission.Response) AgentResultSubmission {
	t.Helper()
	var result AgentResultSubmission
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func mcpReceiptArgs(t *testing.T, id string) []byte {
	t.Helper()
	b, err := json.Marshal(struct {
		RequestID string `json:"request_id"`
	}{id})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestMCPTools_ModeServiceSocket(t *testing.T) {
	for _, mode := range []string{"research", "spike", "plan", "implement", "validate"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			db, _, o := agentResultFixture(t, mode)
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			client, _, _, counted := mcpToolFixture(t, db, service)
			list, err := client.List(t.Context())
			if err != nil || list.Error != "" {
				t.Fatalf("list: %+v %v", list, err)
			}
			var catalog struct {
				Tools []submission.ToolSpec `json:"tools"`
			}
			if err := json.Unmarshal(list.Result, &catalog); err != nil || len(catalog.Tools) != 3 {
				t.Fatalf("catalog=%+v %v", catalog, err)
			}
			for _, name := range []string{"shell", "capture_execution", "create_issue", "waive", "acceptance_override", "dispatch", "change_mode", "communication_submit_create"} {
				denied, err := client.Call(t.Context(), name, json.RawMessage(`{}`))
				if err != nil {
					t.Fatal(err)
				}
				if denied.Error == "" {
					t.Fatalf("direct %s allowed", name)
				}
			}
			if p, s, r := counted.counts(); p+s+r != 0 {
				t.Fatalf("denied calls reached backend %d/%d/%d", p, s, r)
			}
			data := resultCreate(mode)
			if mode == "plan" {
				data = []byte(`{"result":{"outcome":"incomplete","reason":"Needs owner input","comment":"Discussion","payload":{"plan":{}}}}`)
			}
			var pretty bytes.Buffer
			if err := json.Indent(&pretty, data, "", "  "); err != nil {
				t.Fatal(err)
			}
			data = pretty.Bytes()
			before, err := service.Preflight(t.Context(), data)
			if err != nil || !before.Valid {
				t.Fatalf("baseline: %+v %v", before, err)
			}
			preflight := mcpToolCall(t, client, "communication_preflight", data)
			var diagnostics ar.UpdateDiagnostics
			if err := json.Unmarshal(preflight.Result, &diagnostics); err != nil || !reflect.DeepEqual(diagnostics, before) {
				t.Fatalf("shared preflight mismatch: %+v %v", diagnostics, err)
			}
			revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
			if err != nil || len(revisions) != 0 {
				t.Fatal("preflight persisted a revision")
			}
			first := mcpToolCall(t, client, "communication_submit", data)
			recorded := mcpDecodeSubmission(t, first)
			if first.RequestID == "" || recorded.Receipt == nil || !recorded.Validation.Valid || recorded.Snapshot == nil {
				t.Fatalf("submission=%+v", recorded)
			}
			states := recorded.Receipt.Latest
			if states.Input != "accepted" || states.Engineering != "pending" || states.Acceptance != "pending" || states.Publication != "pending" {
				t.Fatalf("false workflow success: %+v", states)
			}
			duplicate := mcpToolCall(t, client, "communication_submit", data)
			again := mcpDecodeSubmission(t, duplicate)
			if duplicate.RequestID != first.RequestID || again.Receipt.ID != recorded.Receipt.ID {
				t.Fatal("duplicate allocated another application operation")
			}
			receipt := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_receipt", mcpReceiptArgs(t, first.RequestID)))
			if receipt.Receipt.ID != recorded.Receipt.ID || !bytes.Equal(receipt.Receipt.Payload, data) {
				t.Fatal("receipt lost exact input or correlation")
			}
			if p, s, r := counted.counts(); p != 1 || s != 2 || r != 1 {
				t.Fatalf("backend delegation=%d/%d/%d", p, s, r)
			}
			counted.mu.Lock()
			for _, got := range counted.payloads {
				if !bytes.Equal(got, data) {
					t.Error("transport changed contribution bytes")
				}
			}
			counted.mu.Unlock()
			revisions, err = db.ResultRevisionNumbers(t.Context(), service.Binding())
			if err != nil || !reflect.DeepEqual(revisions, []int64{1}) {
				t.Fatalf("revisions=%v %v", revisions, err)
			}
			// Caller-owned source run/task cannot widen receipt authority.
			_, err = client.Call(t.Context(), "communication_receipt", json.RawMessage(`{"request_id":"foreign","run_id":999}`))
			if err == nil {
				t.Fatal("foreign receipt selector accepted by client")
			}
			invalid := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(`{"mode":"research","task":"foreign","result":{}}`)))
			if invalid.Validation.Valid || invalid.Receipt == nil || invalid.Receipt.InputState != "rejected" {
				t.Fatalf("caller scope override accepted: %+v", invalid)
			}
			revisions, err = db.ResultRevisionNumbers(t.Context(), service.Binding())
			if err != nil || len(revisions) != 1 {
				t.Fatal("selector override changed snapshot")
			}
		})
	}
}

func TestMCPTools_RevokedSocketDeniesBackend(t *testing.T) {
	for _, state := range []string{"cancelled", "closed", "replaced", "restart"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db, session, o := agentResultFixture(t, "research")
			service, err := BindAgentResultService(t.Context(), o)
			if err != nil {
				t.Fatal(err)
			}
			client, guard, authority, counted := mcpToolFixture(t, db, service)
			first := mcpToolCall(t, client, "communication_submit", resultCreate("research"))
			switch state {
			case "cancelled", "closed":
				if err := guard.Revoke(t.Context(), state); err != nil {
					t.Fatal(err)
				}
			case "restart":
				fresh, err := NewAttemptAuthority(t.Context(), db)
				if err != nil {
					t.Fatal(err)
				}
				defer fresh.Close(context.WithoutCancel(t.Context()))
			case "replaced":
				o.Attempt = agentResultRun(t, db, session, "research", "replacement", 2)
				next, err := BindAgentResultService(t.Context(), o)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := authority.Bind(t.Context(), next.Binding(), next.MCPPermissionDigest(), time.Now().Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
			}
			list, err := client.List(t.Context())
			if err != nil || list.Error == "" {
				t.Fatalf("stale list=%+v %v", list, err)
			}
			for _, tool := range []string{"communication_preflight", "communication_submit", "communication_receipt"} {
				data := resultCreate("research")
				if tool == "communication_receipt" {
					data = mcpReceiptArgs(t, first.RequestID)
				}
				r, err := client.Call(t.Context(), tool, data)
				if err != nil || r.Error == "" {
					t.Fatalf("stale %s=%+v %v", tool, r, err)
				}
			}
			if p, s, r := counted.counts(); p != 0 || s != 1 || r != 0 {
				t.Fatalf("stale call accessed backend: %d/%d/%d", p, s, r)
			}
			retained, err := service.Receipt(t.Context(), service.Binding().RunID, first.RequestID)
			if err != nil || retained.Receipt == nil || retained.Receipt.InputState != "accepted" {
				t.Fatalf("internal history revoked: %+v %v", retained, err)
			}
		})
	}
}

func TestMCPTools_PlanUnsafeRouteAndRecovery(t *testing.T) {
	db, session, o := agentResultFixture(t, "plan")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, authority, _ := mcpToolFixture(t, db, service)
	proposal := resultCreate("plan")
	pre := mcpToolCall(t, client, "communication_preflight", proposal)
	if !bytes.Contains(pre.Result, []byte("mcp_creation_refused")) {
		t.Fatalf("missing actionable refusal: %s", pre.Result)
	}
	denied := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", proposal))
	if denied.Validation.Valid || denied.Receipt == nil || denied.Receipt.InputState != "rejected" || denied.Snapshot != nil {
		t.Fatalf("unsafe proposal accepted: %+v", denied)
	}
	if _, err := db.GetResultRevision(t.Context(), service.Binding(), 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("MCP retained a publishable snapshot: %v", err)
	}
	// Original helper input policy still accepts the same proposal bytes.
	original, err := service.Submit(t.Context(), "ordinary-helper", proposal)
	if err != nil || !original.Validation.Valid || original.Snapshot == nil {
		t.Fatalf("helper policy changed: %+v %v", original, err)
	}
	update := []byte(`{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Retained proposal update"}]}`)
	// The shared update grammar will reject an invalid operation; require the
	// fixture itself to be valid before testing the stricter MCP route.
	baseline, err := service.Preflight(t.Context(), update)
	if err != nil || !baseline.Valid {
		t.Fatalf("update fixture: %+v %v", baseline, err)
	}
	deniedUpdate := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update))
	if deniedUpdate.Validation.Valid || !bytes.Contains(deniedUpdate.Receipt.Diagnostics, []byte("mcp_creation_refused")) {
		t.Fatalf("retained proposal update bypassed policy: %+v", deniedUpdate)
	}
	if err := authority.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Fresh authority reuses retained data without turning it into MCP permission.
	o.Attempt = agentResultRun(t, db, session, "plan", "recovery-attempt", 2)
	recoveryService, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	recovered, _, _, _ := mcpToolFixture(t, db, recoveryService)
	again := mcpDecodeSubmission(t, mcpToolCall(t, recovered, "communication_submit", update))
	if again.Validation.Valid {
		t.Fatal("restart lost unsafe-route restriction")
	}
	receipt, err := recoveryService.Receipt(t.Context(), service.Binding().RunID, "ordinary-helper")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Receipt.ID != original.Receipt.ID {
		t.Fatal("recovery lost legitimate historical receipt")
	}
	numbers, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || !reflect.DeepEqual(numbers, []int64{1}) {
		t.Fatalf("MCP altered retained proposal: %v %v", numbers, err)
	}
}

func TestMCPTools_ValidateRetainedAssessment(t *testing.T) {
	db, _, o := agentResultFixture(t, "validate")
	digest := strings.Repeat("a", 64)
	revision := strings.Repeat("b", 64)
	o.Resolve = func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "validate", Stories: map[string]agent.ContributionStory{"US-001": {Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, SnapshotRetained: true, Criteria: []string{"AC-001"}, ExecutionRequired: []string{"AC-001"}, Checks: map[string]agent.RetainedContributionCheck{"check-1": {Story: "US-001", Revision: revision, SnapshotID: "snapshot-1", ImplementationDigest: digest, Executed: true, Passed: true, Retained: true}}}}}, nil
	}
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, _ := mcpToolFixture(t, db, service)
	story := ar.AssessmentStory{Story: "US-001", SnapshotID: "snapshot-1", Criteria: []agent.ContributionFinding{{Name: "AC-001", Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{"check-1"}, Summary: "Retained offline fixture evidence"}}}}}
	for _, name := range []string{"code", "architecture", "plan"} {
		story.Reviews = append(story.Reviews, agent.ContributionFinding{Name: name, Status: "satisfied", Classification: "blocking", Evidence: []agent.EvidenceExplanation{{CheckIDs: []string{}, Summary: "Retained source assessment"}}})
	}
	data, err := json.Marshal(ar.CreateInput{Result: ar.Result{Outcome: "completed", Comment: "Retained assessment", Payload: ar.Payload{Validate: &ar.Validate{Stories: []ar.AssessmentStory{story}}}}})
	if err != nil {
		t.Fatal(err)
	}
	fresh := []byte(strings.Replace(string(data), `"snapshot_id":"snapshot-1"`, `"snapshot_id":"snapshot-1","capture":{"files":[],"checks":[]}`, 1))
	refused := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", fresh))
	if refused.Validation.Valid || refused.Receipt.InputState != "rejected" {
		t.Fatalf("fresh capture allowed: %+v", refused)
	}
	accepted := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", data))
	if !accepted.Validation.Valid || accepted.Receipt.InputState != "accepted" || accepted.Receipt.Latest.Acceptance != "pending" {
		t.Fatalf("retained assessment/self-acceptance: %+v", accepted)
	}
}

func TestMCPTools_CompletedPlanRefused(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, _ := mcpToolFixture(t, db, service)
	story := ar.PlanStory{Key: "US-001", Intent: "create", Title: "Create result", UserStory: ar.UserStory{Actor: "operator", Want: "consistent results", Benefit: "reliable communication"}, AcceptanceCriteria: []planbrief.CriterionContent{{ID: "AC-001", Text: "Reject invalid inputs"}}, Tasks: []planbrief.TaskContent{{ID: "T-001", Description: "Implement validation", OwnerRole: "engineer", CriterionIDs: []string{"AC-001"}, DependsOn: []string{}}}, Architecture: &planbrief.ArchitectureContent{Summary: "Pure boundary", Components: []planbrief.ComponentContent{{Name: "validator", Responsibility: "Validate input"}}, Interfaces: []planbrief.InterfaceContent{{Name: "Create", Input: "authored input", Output: "validated result"}}, Constraints: []string{"No I/O"}, Decisions: []planbrief.DecisionContent{{Decision: "Typed input", Reason: "Explicit shape"}}}, Dependencies: &planbrief.DependencyContent{StoryKeys: []string{}, IssueRefs: []string{}}}
	data, err := json.Marshal(ar.CreateInput{Result: ar.Result{Outcome: "completed", Comment: "Completed plan", Payload: ar.Payload{Plan: &ar.Plan{Summary: "Deliver a result boundary", Stories: []ar.PlanStory{story}}}}})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := service.Preflight(t.Context(), data)
	if err != nil || !baseline.Valid {
		t.Fatalf("completed fixture invalid: %+v %v", baseline, err)
	}
	refused := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", data))
	if refused.Validation.Valid || refused.Receipt == nil || refused.Receipt.InputState != "rejected" || refused.Snapshot != nil || !bytes.Contains(refused.Receipt.Diagnostics, []byte("mcp_creation_refused")) {
		t.Fatalf("completed creation proposal accepted: %+v", refused)
	}
	ordinary, err := service.Submit(t.Context(), "completed-helper", data)
	if err != nil || !ordinary.Validation.Valid || ordinary.Snapshot == nil {
		t.Fatalf("ordinary completed Plan changed: %+v %v", ordinary, err)
	}
}

func TestMCPTools_EditSelectorAndPermissionBinding(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	o.EditTargets = []ar.Target{{Field: "comment"}}
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, guard, _, _ := mcpToolFixture(t, db, service)
	_ = mcpToolCall(t, client, "communication_submit", resultCreate("research"))
	wider := *service
	wider.editTargets = ar.EditTargets("research")
	if _, err := NewToolDispatcher(guard, &wider); err == nil {
		t.Fatal("wider service grants matched original permission digest")
	}
	updates := []struct {
		name, data string
		valid      bool
	}{
		{"permitted comment", `{"expected_revision":1,"operations":[{"op":"set","target":{"field":"comment"},"value":"Allowed annotation"}]}`, true},
		{"unassigned selector", `{"expected_revision":2,"operations":[{"op":"set","target":{"field":"payload.research.request"},"value":"Different task"}]}`, false},
	}
	for _, tt := range updates {
		t.Run(tt.name, func(t *testing.T) {
			result := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", []byte(tt.data)))
			if result.Validation.Valid != tt.valid {
				t.Fatalf("selector valid=%v want=%v %+v", result.Validation.Valid, tt.valid, result.Validation)
			}
		})
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || !reflect.DeepEqual(revisions, []int64{1, 2}) {
		t.Fatalf("unauthorized selector changed snapshots: %v %v", revisions, err)
	}
}

// lockedBuffer collects log output written from broker workers.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureSlog replaces the package-level logger; callers must not run in parallel.
func captureSlog(t *testing.T) *lockedBuffer {
	t.Helper()
	out := &lockedBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(out, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return out
}

func requireNotRecorded(t *testing.T, r submission.Response) {
	t.Helper()
	got := mcpDecodeSubmission(t, r)
	d := got.Validation.Diagnostics.Diagnostics
	if got.Validation.Valid || got.Receipt != nil || len(d) != 1 || d[0].Code != "not_recorded" || !strings.Contains(d[0].Message, "call submit again") {
		t.Fatalf("not a not_recorded answer: %s", r.Result)
	}
}

// A handler failure is logged with its correlation and the real error, and a
// failure after which a receipt may exist keeps the generic broker text.
func TestMCPTools_FailedSubmitAndReceiptAreLogged(t *testing.T) {
	logs := captureSlog(t)
	db, _, o := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, counted := mcpToolFixture(t, db, service)
	counted.mu.Lock()
	counted.submitErr = errors.New("forced submit failure 7d1")
	counted.receiptErr = errors.New("forced receipt failure 9c4")
	counted.mu.Unlock()
	submitted, err := client.Call(t.Context(), "communication_submit", resultCreate("research"))
	if err != nil || !strings.Contains(submitted.Error, "query its receipt") {
		t.Fatalf("submit=%+v %v", submitted, err)
	}
	receipt, err := client.Call(t.Context(), "communication_receipt", mcpReceiptArgs(t, submitted.RequestID))
	if err != nil || !strings.Contains(receipt.Error, "query its receipt") {
		t.Fatalf("receipt=%+v %v", receipt, err)
	}
	b := service.Binding()
	for _, want := range []struct{ method, text string }{{"communication_submit", "forced submit failure 7d1"}, {"communication_receipt", "forced receipt failure 9c4"}} {
		found := false
		for _, line := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
			var entry struct {
				Method    string `json:"method"`
				RequestID string `json:"request_id"`
				RunID     int64  `json:"run_id"`
				Attempt   string `json:"attempt"`
				Error     string `json:"error"`
			}
			if json.Unmarshal([]byte(line), &entry) == nil && entry.Method == want.method {
				found = entry.RequestID == submitted.RequestID && entry.RunID == b.RunID && entry.Attempt == b.Attempt && entry.Error == want.text
			}
		}
		if !found {
			t.Fatalf("%s failure not logged with correlation: %s", want.method, logs.String())
		}
	}
}

// A submit that recorded nothing says so plainly, a receipt for it says the
// same, and submitting again records the result.
func TestMCPTools_UnrecordedSubmitAsksForResubmit(t *testing.T) {
	db, _, o := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, counted := mcpToolFixture(t, db, service)
	data := resultCreate("research")
	counted.mu.Lock()
	counted.submitErr = errors.New("forced failure before recording")
	counted.mu.Unlock()
	failed := mcpToolCall(t, client, "communication_submit", data)
	requireNotRecorded(t, failed)
	requireNotRecorded(t, mcpToolCall(t, client, "communication_receipt", mcpReceiptArgs(t, failed.RequestID)))
	if revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding()); err != nil || len(revisions) != 0 {
		t.Fatalf("failed submit recorded %v %v", revisions, err)
	}
	counted.mu.Lock()
	counted.submitErr = nil
	counted.mu.Unlock()
	again := mcpToolCall(t, client, "communication_submit", data)
	accepted := mcpDecodeSubmission(t, again)
	if again.RequestID != failed.RequestID || accepted.Receipt == nil || accepted.Receipt.InputState != "accepted" || accepted.Receipt.AppliedRevision != 1 {
		t.Fatalf("resubmit=%+v", accepted)
	}
	receipt := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_receipt", mcpReceiptArgs(t, again.RequestID)))
	if receipt.Receipt == nil || receipt.Receipt.ID != accepted.Receipt.ID {
		t.Fatalf("receipt=%+v", receipt)
	}
}

// A promoted story's first draft with two dependency story keys is accepted
// through the MCP submit path (issue #633: runs 1238 and 1472).
func TestMCPTools_PromotedCreateWithTwoDependencies(t *testing.T) {
	db, _, o := agentResultFixture(t, "plan")
	binding := &ar.PlanGraphBinding{Story: "story-003", Stage: "story", Candidate: "plan-3-1", RequestID: "plan-request-3-1", Upstream: []string{}, SharedRevision: "initial", Promoted: true, PromotedDependencies: []string{"story-001", "story-002"}}
	o.Resolve = func(context.Context) (ar.Context, error) {
		return ar.Context{SchemaVersion: 1, WorkflowVersion: 2, Mode: "plan", QuestionsAllowed: true, PlanScope: planbrief.ContentScope{Roles: []string{"engineer"}}, PlanGraph: binding.Clone()}, nil
	}
	o.ReadTargets, o.EditTargets = planGraphResultTargets(), planGraphResultTargets()
	service, err := BindAgentResultService(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	client, _, _, _ := mcpToolFixture(t, db, service)
	request := integrationCreateRequest("story-003")
	request.Draft.Dependencies.StoryKeys = []string{"story-001", "story-002"}
	data := submitBytes(t, integrationDone(request, "First draft"))
	var pre ar.UpdateDiagnostics
	if err := json.Unmarshal(mcpToolCall(t, client, "communication_preflight", data).Result, &pre); err != nil || !pre.Valid {
		t.Fatalf("preflight=%+v %v", pre, err)
	}
	got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", data))
	if !got.Validation.Valid || got.Receipt == nil || got.Receipt.InputState != "accepted" || got.Receipt.AppliedRevision != 1 {
		t.Fatalf("two-dependency create refused: %+v", got)
	}
}
