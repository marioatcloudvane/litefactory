package taskdiscussion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

const captureGraph = `digraph advisory {
 graph [team="test",mode="research",profile_version="1",start="investigate"];
 investigate [lf_action="lf_agent",lf_agent_markdown="research",lf_skills="evidence",lf_outcomes="success"];
 shape [lf_action="lf_agent",lf_agent_markdown="shaping",lf_skills="evidence",lf_outcomes="success"];
 report [lf_action="lf_comment",lf_source="investigate",lf_outcomes="published"];
 review [lf_action="lf_ask",lf_source="shape",lf_outcomes="published"];
 waiting [lf_terminal="true"];
 investigate -> report [lf_on="success"];
 shape -> review [lf_on="success"];
 report -> waiting [lf_on="published"];
 review -> waiting [lf_on="published"];
}`

func TestCaptureCurrentResearchAndBugRoleBindings(t *testing.T) {
	profile, err := graphexec.ParseProfile([]byte(captureGraph))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ node, role, stage string }{{"report", "research", "investigation"}, {"review", "shaping", "shaping"}} {
		s := store.TaskAdvisorySnapshot{Definition: store.GraphDefinition{Mode: "research"}, Instance: store.GraphInstance{ID: 1, CurrentNode: test.node}, Outputs: []store.AdvisorySourceOutput{{AttemptID: 3, Node: map[string]string{"report": "investigate", "review": "shape"}[test.node], Status: store.GraphSucceeded, OutputID: 5, Payload: json.RawMessage(`{"result":{"payload":{"research":{"findings":[{"text":"saved"}]}}}}`)}}}
		node, stage, _, _, err := selectAdvisoryTarget(s, profile)
		if err != nil || node.Attrs["lf_agent_markdown"] != test.role || stage != test.stage {
			t.Fatalf("%s: %+v %s %v", test.node, node, stage, err)
		}
	}
	// Without an exact owner wait, the shared terminal has two possible roles.
	if _, err = currentAnalysisNode(profile, "waiting", map[string]bool{}); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("ambiguous target: %v", err)
	}
	bug, err := graphexec.ParseProfile([]byte(`digraph bug { graph [team="test",workflow="bug",profile_version="2",start="conversation"]; conversation [lf_action="lf_loop",lf_outcomes="analyse,approved"]; analyst [lf_action="lf_agent",lf_agent_markdown="bug-analyst",lf_outcomes="done"]; fixer [lf_action="lf_agent",lf_agent_markdown="bug-fixer",lf_outcomes="reshape"]; analyst -> conversation [lf_on="done"]; fixer -> conversation [lf_on="reshape"]; conversation -> analyst [lf_on="analyse"]; conversation -> fixer [lf_on="approved"]; }`))
	if err != nil {
		t.Fatal(err)
	}
	source := store.AdvisorySourceOutput{AttemptID: 2, Node: "analyst", Status: store.GraphSucceeded, OutputID: 2, Payload: json.RawMessage(`{"result_id":3,"result_revision":1,"node":{"result":{"payload":{"bug":{"analysis":{"cause":"saved cause"}}}}}}`)}
	snapshot := store.TaskAdvisorySnapshot{Definition: store.GraphDefinition{Workflow: "bug"}, Instance: store.GraphInstance{CurrentNode: "conversation"}, Outputs: []store.AdvisorySourceOutput{source}}
	node, stage, _, _, err := selectAdvisoryTarget(snapshot, bug)
	if err != nil || node.Attrs["lf_agent_markdown"] != "bug-analyst" || stage != "analysis" {
		t.Fatalf("bug: %+v %s %v", node, stage, err)
	}
	snapshot.Outputs = append(snapshot.Outputs, store.AdvisorySourceOutput{AttemptID: 3, Node: "fixer", Status: store.GraphSucceeded, OutputID: 3, Payload: json.RawMessage(`{"node":{"result":{"payload":{"bug":{"repair":{}}}}}}`)})
	if _, _, _, _, err = selectAdvisoryTarget(snapshot, bug); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("repair target revived old analyst: %v", err)
	}
}

func TestCaptureExactPlanReviewContexts(t *testing.T) {
	profile, err := graphexec.ParseProfile([]byte(`digraph plan { graph [team="test",mode="plan",profile_version="1",start="set_agent"]; set_agent [lf_action="lf_agent",lf_agent_markdown="plan-set",lf_outcomes="success"]; story_agent [lf_action="lf_agent",lf_agent_markdown="plan-story",lf_outcomes="success"]; technical_agent [lf_action="lf_agent",lf_agent_markdown="plan-technical",lf_outcomes="success"]; acceptance_agent [lf_action="lf_agent",lf_agent_markdown="plan-acceptance",lf_outcomes="success"]; }`))
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"set", "assembled", "story", "technical", "acceptance"} {
		t.Run(stage, func(t *testing.T) {
			state := planbrief.PlanState{SharedRevision: "initial"}
			s := store.TaskAdvisorySnapshot{Definition: store.GraphDefinition{Mode: "plan"}, PlanRevision: 7, PlanParent: "github:acme/api#1"}
			expectedRole := "plan-set"
			switch stage {
			case "set":
				state.StorySet = &planbrief.PlanStorySetState{Revision: "set-one", Entries: []planbrief.PlanStorySetEntry{{ID: "s1", Name: "Feature", Intent: "Useful"}}}
			case "assembled":
				state.AssembledWait = "assembly-one"
			default:
				draft := planbrief.PlanDraft{Key: "story-001", Name: "A story", UserStory: planbrief.UserStoryContent{Want: "A useful change"}}
				if err = state.PutCandidate(draft, stage, "candidate"); err != nil {
					t.Fatal(err)
				}
				state.Stories[0].Stage = stage
				revision, e := state.ReviewRevision(draft.Key, stage)
				if e != nil {
					t.Fatal(e)
				}
				state.Stories[0].Wait = &planbrief.PlanWait{ID: "wait", Stage: stage, Candidate: "candidate", Revision: revision}
				s.StoryKey = draft.Key
				expectedRole = "plan-" + stage
			}
			s.PlanState, _ = json.Marshal(state)
			node, gotStage, _, _, e := selectAdvisoryTarget(s, profile)
			if e != nil || gotStage != stage || node.Attrs["lf_agent_markdown"] != expectedRole {
				t.Fatalf("stage: %+v %s %v", node, gotStage, e)
			}
			if s.StoryKey != "" {
				state.Stories[0].Wait.Revision = "stale"
				s.PlanState, _ = json.Marshal(state)
				if _, _, _, _, e = selectAdvisoryTarget(s, profile); !errors.Is(e, ErrTargetUnavailable) {
					t.Fatalf("stale target: %v", e)
				}
			}
		})
	}
}

func TestStoreCapturePinsAcceptedCurrentEvidenceAndResourceClosure(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "api"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitResearchPath(ctx, session.ID, "graph"); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{"research.dot": []byte(captureGraph), "research.md": []byte("historical analysis role"), "shaping.md": []byte("historical shaping role"), "skills/evidence/SKILL.md": []byte("---\nname: evidence\ndescription: Captured evidence\nresources:\n  - facts.md\n---\nUse available facts.\n"), "skills/evidence/facts.md": []byte("retained skill resource")}
	rawFiles, _ := json.Marshal(files)
	definition, err := db.CreateGraphDefinition(ctx, store.GraphDefinition{Team: "test", Mode: "research", ProfileVersion: 1, Path: "research/research.dot", Digest: strings.Repeat("a", 64), TeamSnapshotDigest: strings.Repeat("b", 64)})
	if err != nil {
		t.Fatal(err)
	}
	admission, err := db.AdmitWorkflowLaunch(ctx, store.WorkflowLaunch{Mode: "research", SessionID: session.ID, TriggerID: 1, DefinitionID: definition.ID, Start: "investigate", ContentDigest: strings.Repeat("b", 64), TopologyDigest: strings.Repeat("c", 64), Files: rawFiles})
	if err != nil {
		t.Fatal(err)
	}
	segment, err := db.GetOrCreateSegment(ctx, session.ID, store.FirstSegmentOrdinal, "capture", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "research", Agent: store.AgentClaude, TriggerCommentID: 1, LogPath: func(int) string { return filepath.Join(t.TempDir(), "run.log") }})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	attempt, err := db.StartGraphNodeAttempt(ctx, store.GraphAttemptStart{InstanceID: admission.Instance.ID, LaunchID: admission.LaunchID, Node: "investigate", RunID: run.ID, StartedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindResultAttempt(ctx, store.ResultAttempt{Task: session.TaskRef, Mode: "research", WorkContext: "capture", Attempt: "capture", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
	if err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"outcome":"completed","comment":"Saved findings","payload":{"research":{"findings":[{"text":"Saved evidence"}]}}}`)
	accepted := []byte(fmt.Sprintf(`{"result":%s}`, result))
	if _, err = db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "accepted", ExpectedRevision: 0, Payload: accepted, Snapshot: accepted, Diagnostics: []byte(`{"valid":true,"complete":true}`), Retired: []byte(`[]`), Accepted: true}); err != nil {
		t.Fatal(err)
	}
	output := []byte(fmt.Sprintf(`{"version":1,"response":"done","result":%s}`, result))
	if err = db.CompleteGraphNodeAttempt(ctx, attempt.ID, store.GraphCompletion{Status: store.GraphSucceeded, Outcome: "success", FinishedAt: now.Add(time.Second), InstanceStatus: store.GraphRunning, Outputs: []store.GraphOutput{{Kind: store.GraphOutputJSON, Payload: output}}, Transition: &store.GraphTransition{InstanceID: admission.Instance.ID, FromNode: "investigate", ToNode: "report", OnOutcome: "success", At: now.Add(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	var capturedAnalysis map[string]string
	capture := StoreCapture{DB: db, Guidance: func(_ context.Context, analysis, _ string) (json.RawMessage, error) {
		if err = json.Unmarshal([]byte(analysis), &capturedAnalysis); err != nil {
			return nil, err
		}
		return json.RawMessage(`{"dedicated":"advisory"}`), nil
	}}
	basis, err := capture.Capture(ctx, session.TaskRef, "ask")
	if err != nil {
		t.Fatal(err)
	}
	if basis.Role != "research" || basis.Stage != "investigation" || basis.Revision == "" || len(basis.Sources) != 1 || len(basis.Gaps) != 1 {
		t.Fatalf("basis: %+v", basis)
	}
	if len(capturedAnalysis) != 3 || capturedAnalysis["skills/evidence/facts.md"] != "retained skill resource" {
		t.Fatalf("closure lost: %+v", capturedAnalysis)
	}
	same, err := capture.Capture(ctx, session.TaskRef, "judge")
	if err != nil || same.Revision != basis.Revision {
		t.Fatalf("capture unstable: %+v %v", same, err)
	}
	c, err := db.OpenDiscussion(ctx, store.DiscussionConversation{DiscussionScope: store.DiscussionScope{OwnerID: 42, OwnerGeneration: 1, RepositoryRef: "github:acme/api", TaskRef: session.TaskRef}, ID: "conversation", ClientID: "client", RequestKey: "open", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	pending := store.AdvisoryAdmission{DiscussionScope: c.DiscussionScope, ID: "admission", ConversationID: c.ID, Operation: "ask", RequestKey: "ask", Digest: "digest", Basis: `{}`, Context: `[]`, Question: "why", GrantID: "grant", JobID: "job", ExecutionID: "execution", CreatedAt: now, TargetGuard: basis.StoreGuard}
	// A second saved JSON on the same attempt is ambiguous, even if one matches.
	if _, err = db.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: attempt.ID, Kind: store.GraphOutputJSON, Payload: output, CreatedAt: now.Add(2 * time.Second)}); err != nil {
		t.Fatal(err)
	}
	if _, err = db.AdmitAdvisory(ctx, pending); !errors.Is(err, store.ErrWorkflowRevision) {
		t.Fatalf("stale atomic admission guard: %v", err)
	}
	if _, err = capture.Capture(ctx, session.TaskRef, "ask"); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("ambiguous source: %v", err)
	}
	if err = db.SetGraphInstanceStatus(ctx, admission.Instance.ID, store.GraphSucceeded, time.Time{}, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	service := &Service{db: db, capture: capture}
	if got := service.freshness(ctx, basis, "ask"); got != "outdated" {
		t.Fatalf("advanced ineligible stage freshness = %s", got)
	}
}
