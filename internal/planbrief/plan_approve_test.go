package planbrief_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func graphApprove(t *testing.T, s *planbrief.PlanState, index int, stage string) planbrief.GraphApproval {
	t.Helper()
	st := s.Stories[index]
	got, err := s.ApproveByGraph(st.Key, stage, st.Candidate, st.EffectRequest)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestPlanApproveByGraph_Approves(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	want, err := s.ReviewRevision("a", "story")
	if err != nil {
		t.Fatal(err)
	}
	if got := graphApprove(t, &s, 0, "story"); got.Outcome != "approved" {
		t.Fatalf("got %+v", got)
	}
	st := s.Stories[0]
	if st.Approvals["story"] != want || st.ApprovedBy["story"] != planbrief.ApprovalByGraph || st.Stage != "technical" {
		t.Fatalf("approval not recorded: %+v", st)
	}
	if got := s.GraphApprovedStages(); !reflect.DeepEqual(got, []planbrief.GraphApprovedStage{{Key: "a", Stage: "story"}}) {
		t.Fatalf("graph approvals %v", got)
	}
}

func TestPlanApproveByGraph_ReplayWritesNothing(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	st := s.Stories[0]
	if got := graphApprove(t, &s, 0, "story"); got.Outcome != "approved" {
		t.Fatalf("got %+v", got)
	}
	before := cloneWorkflow(t, s)
	got, err := s.ApproveByGraph("a", "story", st.Candidate, st.EffectRequest)
	if err != nil || got.Outcome != "approved" {
		t.Fatalf("replay: %+v %v", got, err)
	}
	if !reflect.DeepEqual(before, s) {
		t.Fatal("replay changed the state")
	}
	if s.Stories[0].Stage != "technical" || s.Stories[0].Approvals["technical"] != "" {
		t.Fatalf("next stage approved by replay: %+v", s.Stories[0])
	}
}

func TestPlanApproveByGraph_InterveningOwnerApprovalKeepsOwner(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	st := s.Stories[0]
	r := workflowReply(t, &s, 0, 10)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if got, err := s.ApproveByGraph("a", "story", st.Candidate, st.EffectRequest); err != nil || got.Outcome != "approved" {
		t.Fatalf("got %+v %v", got, err)
	}
	if s.Stories[0].ApprovedBy["story"] != planbrief.ApprovalByOwner {
		t.Fatalf("source changed: %v", s.Stories[0].ApprovedBy)
	}
	if got := s.GraphApprovedStages(); len(got) != 0 {
		t.Fatalf("owner approval listed as graph: %v", got)
	}
}

func TestPlanApproveByGraph_Outcomes(t *testing.T) {
	tests := []struct {
		name   string
		keys   []string
		stage  string // stage argument; empty means the story's own
		setup  func(t *testing.T, s *planbrief.PlanState)
		args   func(s *planbrief.PlanState) (candidate, request string)
		want   string
		nowrit bool
	}{
		{name: "rewound stage keeps candidate", keys: []string{"a"}, stage: "technical", setup: func(t *testing.T, s *planbrief.PlanState) {
			approveWorkflowStory(t, s, 0, 10)
			s.Invalidate("a", "story", "")
		}, want: "superseded"},
		{name: "newer candidate", keys: []string{"a"}, args: func(*planbrief.PlanState) (string, string) { return "old-candidate", "" }, want: "superseded"},
		{name: "newer effect request", keys: []string{"a"}, args: func(s *planbrief.PlanState) (string, string) { return s.Stories[0].Candidate, "other-request" }, want: "superseded"},
		{name: "hold", keys: []string{"a"}, setup: func(t *testing.T, s *planbrief.PlanState) { s.Stories[0].Hold = "unknown impact" }, want: "blocked"},
		{name: "effect not applied", keys: []string{"a"}, setup: func(t *testing.T, s *planbrief.PlanState) { s.Stories[0].Effect = "pending" }, want: "blocked"},
		{name: "upstream held", keys: []string{"u", "a"}, setup: func(t *testing.T, s *planbrief.PlanState) {
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"u"}
			s.Stories[0].Hold = "held"
		}, want: "blocked", args: nil},
		{name: "stale earlier approval", keys: []string{"a"}, stage: "technical", setup: func(t *testing.T, s *planbrief.PlanState) {
			r := workflowReply(t, s, 0, 10)
			if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
				t.Fatal(ok, err)
			}
			s.Stories[0].Approvals["story"] = "stale"
		}, want: "superseded"},
		{name: "unapproved upstream", keys: []string{"u", "a"}, setup: func(t *testing.T, s *planbrief.PlanState) {
			s.Stories[1].Draft.Dependencies.StoryKeys = []string{"u"}
		}, want: "review"},
		{name: "open questions", keys: []string{"a"}, setup: func(t *testing.T, s *planbrief.PlanState) {
			s.Stories[0].Draft.Questions = []string{"Which owner?"}
		}, want: "review"},
		{name: "open wait", keys: []string{"a"}, setup: func(t *testing.T, s *planbrief.PlanState) {
			if err := s.OpenPlanWait("a", "w", "question", 100); err != nil {
				t.Fatal(err)
			}
		}, want: "review"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := workflowFixture(t, tt.keys...)
			if tt.setup != nil {
				tt.setup(t, &s)
			}
			index := len(tt.keys) - 1
			item := s.Stories[index]
			stage, candidate, request := tt.stage, item.Candidate, item.EffectRequest
			if stage == "" {
				stage = item.Stage
			}
			if tt.args != nil {
				candidate, request = tt.args(&s)
			}
			before := cloneWorkflow(t, s)
			got, err := s.ApproveByGraph(item.Key, stage, candidate, request)
			if err != nil || got.Outcome != tt.want {
				t.Fatalf("got %+v %v, want %s", got, err, tt.want)
			}
			if tt.want != "approved" && got.Reason == "" {
				t.Fatal("no reason")
			}
			if !reflect.DeepEqual(before, cloneWorkflow(t, s)) {
				t.Fatal("a refusal wrote state")
			}
			if len(s.GraphApprovedStages()) != 0 {
				t.Fatal("graph approval recorded")
			}
		})
	}
}

func TestPlanApproveByGraph_Errors(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	if _, err := s.ApproveByGraph("a", "nope", "a-candidate", ""); err == nil {
		t.Fatal("invalid stage accepted")
	}
	if _, err := s.ApproveByGraph("missing", "story", "x", ""); err == nil {
		t.Fatal("unknown story accepted")
	}
}

func TestPlanApproval_SourceRecording(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a", "b")
	r := workflowReply(t, &s, 0, 10)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if s.Stories[0].ApprovedBy["story"] != planbrief.ApprovalByOwner {
		t.Fatalf("owner reply source %v", s.Stories[0].ApprovedBy)
	}
	graphApprove(t, &s, 1, "story")
	if s.Stories[1].ApprovedBy["story"] != planbrief.ApprovalByGraph {
		t.Fatal("graph source missing")
	}
	s.Invalidate("b", "story", "")
	if len(s.GraphApprovedStages()) != 0 {
		t.Fatalf("invalidated approval listed: %v", s.GraphApprovedStages())
	}
	graphApprove(t, &s, 1, "story")
	if err := s.SetSharedRevision("shared-2"); err != nil {
		t.Fatal(err)
	}
	if len(s.Stories[1].ApprovedBy) != 0 || len(s.GraphApprovedStages()) != 0 {
		t.Fatalf("shared revision kept the source: %v", s.Stories[1].ApprovedBy)
	}
}

func TestPlanApproval_OldStateReadsAsOwner(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a")
	approveWorkflowStory(t, &s, 0, 10)
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"approved_by"`) {
		t.Fatal("approved_by not saved")
	}
	var generic map[string]any
	if err = json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	for _, st := range generic["stories"].([]any) {
		delete(st.(map[string]any), "approved_by")
	}
	raw, _ = json.Marshal(generic)
	var old planbrief.PlanState
	if err = json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if got := old.GraphApprovedStages(); len(got) != 0 {
		t.Fatalf("old state reads graph approvals: %v", got)
	}
	if len(old.Stories) != 1 || len(old.Stories[0].Approvals) != 3 || old.Stories[0].ApprovedBy["story"] == planbrief.ApprovalByGraph {
		t.Fatalf("old state misread: %+v", old.Stories)
	}
}

func TestPlanApproval_GraphApprovedStagesOnlyCurrent(t *testing.T) {
	t.Parallel()
	s := workflowFixture(t, "a", "b")
	graphApprove(t, &s, 0, "story")
	graphApprove(t, &s, 1, "story")
	r := workflowReply(t, &s, 1, 10)
	if ok, err := s.ApplyOwnerReply(r); err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := []planbrief.GraphApprovedStage{{Key: "a", Stage: "story"}, {Key: "b", Stage: "story"}}
	if got := s.GraphApprovedStages(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	// A stale approval and a retired story are not current.
	s.Stories[0].Approvals["story"] = "stale"
	s.Stories[1].Retired = true
	if got := s.GraphApprovedStages(); len(got) != 0 {
		t.Fatalf("not current but listed: %v", got)
	}
}
