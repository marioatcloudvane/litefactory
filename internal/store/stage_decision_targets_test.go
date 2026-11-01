package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/planbrief"
)

func captureDecisionProfile(t *testing.T, db *DB, instanceID int64, profile string) {
	t.Helper()
	instance, err := db.GetGraphInstance(t.Context(), instanceID)
	if err != nil {
		t.Fatal(err)
	}
	definition, err := db.GetGraphDefinition(t.Context(), instance.DefinitionID)
	if err != nil {
		t.Fatal(err)
	}
	files, _ := json.Marshal(map[string][]byte{definition.Path: []byte(profile)})
	_, err = db.sql.ExecContext(t.Context(), `INSERT INTO research_launch_definitions(session_id,trigger_id,instance_id,content_digest,topology_digest,files) VALUES(?,200,?,'test-content','test-topology',?)`, instance.SessionID, instanceID, files)
	if err != nil {
		t.Fatal(err)
	}
}

const decisionResearchProfile = `digraph research {
 graph [team="test", mode="research", profile_version="1", start="shape"];
 shape [lf_action="lf_agent",lf_agent_markdown="shaping",lf_outcomes="success"];
 settled [lf_action="lf_if",lf_source="shape",lf_condition="approved",lf_outcomes="true,false"];
 agreed [lf_action="lf_comment",lf_source="shape",lf_kind="final",lf_outcomes="published"];
 settled -> agreed [lf_on="true"];
}`

func TestStageDecisionResearchOperationMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, kind, role, operation string
		allowed                     bool
	}{
		{"investigation revision", "question", "research", "revise_task", true},
		{"investigation approval refused", "question", "research", "approve_stage", false},
		{"shaping question revision", "question", "shaping", "revise_task", true},
		{"shaping question approval refused", "question", "shaping", "approve_stage", false},
		{"shaping review revision", "review", "shaping", "revise_task", true},
		{"shaping review approval", "review", "shaping", "approve_stage", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, in := chatResearchFixture(t)
			profile := strings.ReplaceAll(decisionResearchProfile, `lf_agent_markdown="shaping"`, `lf_agent_markdown="`+tc.role+`"`)
			captureDecisionProfile(t, db, in.InstanceID, profile)
			if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_comment_intents SET kind=? WHERE delivery_key=?`, tc.kind, in.DeliveryKey); err != nil {
				t.Fatal(err)
			}
			target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "", tc.operation)
			if tc.allowed {
				if err != nil || target.Stage == "" || target.Effect == "" {
					t.Fatalf("eligible: %+v %v", target, err)
				}
			} else if !errors.Is(err, ErrStageDecisionTarget) {
				t.Fatalf("unsupported approval: %+v %v", target, err)
			}
		})
	}
}

func TestStageDecisionPlanCurrentReviewMatrix(t *testing.T) {
	for _, stage := range []string{"story", "technical", "acceptance"} {
		for _, operation := range []string{"revise_task", "approve_stage"} {
			t.Run(stage+"/"+operation, func(t *testing.T) {
				db, in := ownerReplyFixture(t, true, "plan", "shape", "review", "review")
				child, err := db.GetSessionByTaskRef(t.Context(), in.TaskRef)
				if err != nil {
					t.Fatal(err)
				}
				repo, e := db.GetRepo(t.Context(), child.RepoID)
				if e != nil {
					t.Fatal(e)
				}
				parent, err := db.GetOrCreateSession(t.Context(), repo, 379)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = db.AdmitPlanWorkflowPath(t.Context(), parent.ID, "graph"); err != nil {
					t.Fatal(err)
				}
				if _, err = db.ReservePlanStory(t.Context(), parent.TaskRef, "story-001"); err != nil {
					t.Fatal(err)
				}
				if err = db.BindPlanStoryIssue(t.Context(), parent.TaskRef, "story-001", 3780, child.IssueNumber); err != nil {
					t.Fatal(err)
				}
				state := planbrief.PlanState{SharedRevision: "initial", Stories: []planbrief.PlanStoryState{{Key: "story-001", Stage: stage, Candidate: "candidate", Draft: planbrief.PlanDraft{Key: "story-001", Name: "Human story name", Problem: "Keep exact owner approval"}, Approvals: map[string]string{}, Effect: "applied"}}}
				for _, earlier := range []string{"story", "technical", "acceptance"} {
					if earlier == stage {
						break
					}
					revision, e := state.ReviewRevision("story-001", earlier)
					if e != nil {
						t.Fatal(e)
					}
					state.Stories[0].Approvals[earlier] = revision
				}
				if err = state.OpenPlanWait("story-001", "semantic-wait", "review", 3780); err != nil {
					t.Fatal(err)
				}
				if _, err = db.SavePlanWorkflowState(t.Context(), parent.TaskRef, 0, state); err != nil {
					t.Fatal(err)
				}
				captureDecisionProfile(t, db, in.InstanceID, decisionResearchProfile)
				target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, stage, operation)
				if err != nil || target.StoryKey != "story-001" || target.PlanParent != parent.TaskRef || target.SessionID != child.ID || strings.Contains(target.Summary, "candidate") {
					t.Fatalf("target: %+v %v", target, err)
				}
				if _, err = db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "set", operation); !errors.Is(err, ErrStageDecisionTarget) {
					t.Fatalf("wrong stage: %v", err)
				}
				proposal, ctx, at := admitStageDecisionFixture(t, db, target, in.At)
				receipt, e := db.ApplyStageDecision(ctx, target, "Revise this stage", proposal.ExecutionID, at)
				if e != nil || receipt.State != "applied" {
					t.Fatalf("apply current Plan stage: %+v %v", receipt, e)
				}
				after, _, e := db.GetPlanWorkflowState(t.Context(), parent.TaskRef)
				if e != nil {
					t.Fatal(e)
				}
				revision, e := state.ReviewRevision("story-001", stage)
				if e != nil {
					t.Fatal(e)
				}
				if operation == "approve_stage" {
					if after.Stories[0].Approvals[stage] != revision || after.Stories[0].Wait != nil {
						t.Fatalf("approval differs from owner transition: %+v", after)
					}
				} else if after.Stories[0].Candidate != state.Stories[0].Candidate || after.Stories[0].Approvals[stage] == revision {
					t.Fatalf("revision approved unseen candidate: %+v", after)
				}
			})
		}
	}
}

func TestStageDecisionPlanUnpromotedSetAndAssembledSelection(t *testing.T) {
	db, in := ownerReplyFixture(t, true, "plan", "shape", "review", "review")
	captureDecisionProfile(t, db, in.InstanceID, decisionResearchProfile)
	state := planbrief.PlanState{SharedRevision: "initial"}
	if _, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Human story", Intent: "Retain the exact target"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePlanWorkflowState(t.Context(), in.TaskRef, 0, state); err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"revise_task", "approve_stage"} {
		target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "set", op)
		if err != nil || target.Stage != "set" {
			t.Fatalf("set: %+v %v", target, err)
		}
	}
	if _, err := state.PromoteStorySet(state.StorySet.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SavePlanWorkflowState(t.Context(), in.TaskRef, 1, state); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "set", "revise_task"); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("promoted set replaced: %v", err)
	}
	state.AssembledWait = state.PlanSetRevision()
	if _, err := db.SavePlanWorkflowState(t.Context(), in.TaskRef, 2, state); err != nil {
		t.Fatal(err)
	}
	target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "assembled", "approve_stage")
	if err != nil || target.Stage != "assembled" {
		t.Fatalf("assembled: %+v %v", target, err)
	}
	if _, err = db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "assembled", "revise_task"); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("ambiguous assembled revision: %v", err)
	}
}

func TestStageDecisionBugPreviewAndCurrentAnalysisOnly(t *testing.T) {
	db, _, instance, session, candidate, conversation := graphDiagnosisFixture(t)
	parkGraphDiagnosisReview(t, db, instance, session, candidate, conversation, true)
	captureDecisionProfile(t, db, instance.ID, decisionResearchProfile)
	for _, op := range []string{"revise_task", "approve_stage"} {
		target, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", op)
		if err != nil || target.DiagnosisRevision != candidate.Revision {
			t.Fatalf("Bug: %+v %v", target, err)
		}
		if op == "approve_stage" && !strings.Contains(target.Effect, "repair") {
			t.Fatalf("missing downstream effect: %s", target.Effect)
		}
	}
	if _, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "repair", "approve_stage"); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("repair control exposed: %v", err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE report_deliveries SET completed=0 WHERE delivery_key='review-1'`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", "approve_stage"); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("unpublished review approved: %v", err)
	}
}

func TestStageDecisionBugTransitionsShareCurrentOwnerBoundary(t *testing.T) {
	for _, op := range []string{"revise_task", "approve_stage"} {
		t.Run(op, func(t *testing.T) {
			db, _, instance, session, candidate, conversation := graphDiagnosisFixture(t)
			parkGraphDiagnosisReview(t, db, instance, session, candidate, conversation, true)
			captureDecisionProfile(t, db, instance.ID, decisionResearchProfile)
			target, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", op)
			if err != nil {
				t.Fatal(err)
			}
			p, ctx, at := admitStageDecisionFixture(t, db, target, graphTestTime())
			receipt, err := db.ApplyStageDecision(ctx, target, "Use the reviewed remedy", p.ExecutionID, at)
			if err != nil || receipt.State != "applied" {
				t.Fatalf("transition: %+v %v", receipt, err)
			}
			current, approval, err := db.CurrentGraphDiagnosis(t.Context(), instance.ID)
			if err != nil || current.Revision != candidate.Revision || current.Digest != candidate.Digest {
				t.Fatalf("candidate rewritten: %+v %v", current, err)
			}
			if (op == "approve_stage") != (approval != nil) {
				t.Fatalf("approval differs from operation: %+v", approval)
			}
			if approval != nil && (approval.Revision != candidate.Revision || approval.Digest != candidate.Digest || approval.OwnerID != 7) {
				t.Fatalf("wrong binding: %+v", approval)
			}
			wait, err := db.GetGraphOwnerWait(t.Context(), instance.ID)
			if err != nil || wait.State != "claimed" || wait.ReplySource != "chat" {
				t.Fatalf("continuation: %+v %v", wait, err)
			}
		})
	}
}

func TestStageDecisionBugQuestionAdmitsRevisionOnly(t *testing.T) {
	db, _, instance, session, candidate, conversation := graphDiagnosisFixture(t)
	parkGraphDiagnosisReview(t, db, instance, session, candidate, conversation, true)
	candidate.ReviewReady = false
	raw, _ := json.Marshal(candidate)
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_outputs SET payload=? WHERE json_valid(CAST(payload AS TEXT)) AND json_extract(CAST(payload AS TEXT),'$.record')='diagnosis'`, raw); err != nil {
		t.Fatal(err)
	}
	question, _ := json.Marshal(struct {
		Diagnosis GraphDiagnosis `json:"diagnosis"`
	}{Diagnosis: candidate})
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE graph_comment_intents SET stop_cause='diagnosis_question',candidate=? WHERE delivery_key='review-1'`, question); err != nil {
		t.Fatal(err)
	}
	captureDecisionProfile(t, db, instance.ID, decisionResearchProfile)
	if _, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", "approve_stage"); !errors.Is(err, ErrStageDecisionTarget) {
		t.Fatalf("question approved: %v", err)
	}
	target, err := db.ResolveStageDecisionTarget(t.Context(), session.TaskRef, "analysis", "revise_task")
	if err != nil {
		t.Fatal(err)
	}
	p, ctx, at := admitStageDecisionFixture(t, db, target, graphTestTime())
	receipt, err := db.ApplyStageDecision(ctx, target, "Use storage evidence to complete the diagnosis", p.ExecutionID, at)
	if err != nil || receipt.State != "applied" {
		t.Fatalf("question guidance: %+v %v", receipt, err)
	}
	_, approval, err := db.CurrentGraphDiagnosis(t.Context(), instance.ID)
	if err != nil || approval != nil {
		t.Fatalf("question gained approval: %+v %v", approval, err)
	}
}

func TestStageDecisionPlanSetAdmissionAndGitHubRace(t *testing.T) {
	for _, operation := range []string{"revise_task", "approve_stage"} {
		t.Run(operation, func(t *testing.T) {
			db, in := ownerReplyFixture(t, true, "plan", "shape", "review", "review")
			captureDecisionProfile(t, db, in.InstanceID, decisionResearchProfile)
			state := planbrief.PlanState{SharedRevision: "initial"}
			if _, _, err := state.ProposeStorySet([]planbrief.PlanStorySetEntry{{ID: "s1", Name: "Storage", Intent: "Retain exact state"}, {ID: "s2", Name: "Reader", Intent: "Read the retained state", DependsOn: []string{"s1"}}}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.SavePlanWorkflowState(t.Context(), in.TaskRef, 0, state); err != nil {
				t.Fatal(err)
			}
			target, err := db.ResolveStageDecisionTarget(t.Context(), in.TaskRef, "set", operation)
			if err != nil {
				t.Fatal(err)
			}
			p, ctx, at := admitStageDecisionFixture(t, db, target, in.At)
			receipt, err := db.ApplyStageDecision(ctx, target, "Clarify the storage responsibilities", p.ExecutionID, at)
			if err != nil || receipt.State != "applied" {
				t.Fatalf("set transition: %+v %v", receipt, err)
			}
			after, revision, err := db.GetPlanWorkflowState(t.Context(), in.TaskRef)
			if err != nil {
				t.Fatal(err)
			}
			if operation == "approve_stage" {
				if !after.StorySet.Promoted || after.StorySet.PromotedRevision != state.StorySet.Revision {
					t.Fatalf("wrong set promoted: %+v", after.StorySet)
				}
				drafts, err := db.ListPlanStoryDrafts(t.Context(), in.TaskRef)
				if err != nil || len(drafts) != 2 || drafts[0].Ordinal != 1 || drafts[1].Ordinal != 2 {
					t.Fatalf("changed ordinals: %+v %v", drafts, err)
				}
			} else {
				if after.StorySet.Promoted {
					t.Fatal("revision promoted old set")
				}
				if _, err = after.PromoteStorySet(after.StorySet.Revision); err != nil {
					t.Fatal(err)
				}
				_, err = db.SavePlanWorkflowState(WithPlanOwnerAdmission(t.Context(), in.TaskRef, ""), in.TaskRef, revision, after)
				if !errors.Is(err, ErrStageDecisionTarget) {
					t.Fatalf("GitHub raced ahead of pending set revision: %v", err)
				}
			}
		})
	}
}
