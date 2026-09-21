package turn

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestResearchShapingWorkerCannotCreateOwnerApproval(t *testing.T) {
	for _, response := range []string{"done", "approved", "changed-approved", "unapproved-reply"} {
		t.Run(response, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				if calls > 1 {
					client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
					update := []byte(fmt.Sprintf(`{"expected_revision":%d,"operations":[{"op":"set","target":{"field":"comment"},"value":"Investigation finished."}]}`, calls-1))
					if response == "changed-approved" && calls == 3 {
						update = []byte(strings.ReplaceAll(string(update), "Investigation finished.", "Changed after approval."))
					}
					got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update))
					if !got.Validation.Valid {
						t.Fatalf("stage update: %+v", got)
					}
				}
				raw := researchDoneJSON
				if calls == 2 {
					if req.Delivery.MainRole != "shaping" {
						t.Fatalf("handoff role: %+v", req.Delivery)
					}
					if response == "approved" {
						raw = strings.Replace(raw, `"response":"done"`, `"response":"approved"`, 1)
					}
				}
				if calls == 3 {
					raw = strings.Replace(raw, `"response":"done"`, `"response":"approved"`, 1)
					if response == "changed-approved" {
						raw = strings.ReplaceAll(raw, "Investigation finished.", "Changed after approval.")
					}
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: raw, FinishedAt: time.Now()}, nil
			})
			f, forge, req := assembledResearchFixture(t, runner)
			first, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if !first.Succeeded() {
				t.Fatalf("investigation: %+v", first)
			}
			req.Trigger.CommentID++
			req.Trigger.Body = "Continue the admitted graph"
			req.Command = ParseCommand(req.Trigger.Body)
			_, err = Execute(t.Context(), f.opts, req)
			instance, loadErr := f.db.LatestResearchInstance(t.Context(), f.session.ID)
			mustProgress(t, loadErr)
			if response == "approved" {
				if instance.Status == store.GraphSucceeded || err == nil {
					t.Fatalf("worker invented approval: status=%s err=%v calls=%d posts=%d", instance.Status, err, calls, forge.attemptCount())
				}
				return
			}
			mustProgress(t, err)
			wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
			mustProgress(t, err)
			if wait.State != "waiting" || instance.Status == store.GraphSucceeded || calls != 2 {
				t.Fatalf("done bypassed owner review: %+v %+v calls=%d", instance, wait, calls)
			}
			comment, err := f.db.GetGraphComment(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			if comment.Kind != "review" {
				t.Fatalf("done did not display review: %+v", comment)
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: req.Trigger.CommentID + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "done", At: time.Now()}
			if response == "unapproved-reply" {
				reply.Body = "Revise the scope before approval"
			}
			for _, variant := range []string{"nonowner", "stale", "candidate", "mode"} {
				bad := reply
				switch variant {
				case "nonowner":
					bad.AuthorID = 8
				case "stale":
					bad.CommentID = delivery.CommentID
				case "candidate":
					bad.DeliveryKey = "another-candidate"
				case "mode":
					bad.Mode = "plan"
				}
				if _, err = f.db.ClaimGraphOwnerReply(t.Context(), bad); err == nil {
					t.Fatalf("%s reply authorized review", variant)
				}
			}
			_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
			mustProgress(t, err)
			if _, err = f.db.ClaimGraphOwnerReply(t.Context(), reply); err == nil {
				t.Fatal("duplicate approval claimed")
			}
			req.Trigger.CommentID = reply.CommentID
			req.Trigger.Body = reply.Body
			req.Command = ParseCommand(reply.Body)
			final, err := Execute(t.Context(), f.opts, req)
			if response == "changed-approved" || response == "unapproved-reply" {
				if err == nil || forge.attemptCount() != 2 {
					t.Fatalf("changed candidate reused approval: err=%v posts=%d", err, forge.attemptCount())
				}
				return
			}
			mustProgress(t, err)
			instance, err = f.db.LatestResearchInstance(t.Context(), f.session.ID)
			mustProgress(t, err)
			if !final.Succeeded() || instance.Status != store.GraphSucceeded || calls != 3 || forge.attemptCount() != 3 {
				t.Fatalf("authenticated approval failed: %+v %+v calls=%d posts=%d", final, instance, calls, forge.attemptCount())
			}
			replay, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if replay.Run.ID != final.Run.ID || calls != 3 || forge.attemptCount() != 3 {
				t.Fatal("duplicate input repeated continuation")
			}

		})
	}
}

func TestResearchContinuationRechecksGeneratedDocuments(t *testing.T) {
	for _, variant := range []string{"handoff", "owner-reply"} {
		t.Run(variant, func(t *testing.T) {
			calls := 0
			runner := lifecycleMCPHost(func(_ context.Context, _ agent.Request) (*agent.RunResult, error) {
				calls++
				raw := researchDoneJSON
				if variant == "owner-reply" {
					raw = researchAskJSON
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: raw, FinishedAt: time.Now()}, nil
			})
			f, _, req := assembledResearchFixture(t, runner)
			first, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			instance, err := f.db.LatestResearchInstance(t.Context(), f.session.ID)
			mustProgress(t, err)
			req.Trigger.CommentID++
			if variant == "owner-reply" {
				wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
				mustProgress(t, err)
				delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
				mustProgress(t, err)
				reply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: req.Trigger.CommentID, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "research", DeliveryKey: wait.DeliveryKey, Body: "Inspect HTTP", At: time.Now()}
				_, err = f.db.ClaimGraphOwnerReply(t.Context(), reply)
				mustProgress(t, err)
				req.Trigger.Body = reply.Body
				req.Command = ParseCommand(reply.Body)
			}
			session, err := naming.AITeamSessionDir(first.Session.Branch, first.Run.StartedAt, first.Session.FeatureSlug)
			mustProgress(t, err)
			mustProgress(t, os.WriteFile(filepath.Join(first.WorktreePath, session, ResearchBriefName), []byte("old candidate bytes"), 0600))
			_, err = Execute(t.Context(), f.opts, req)
			if err == nil || !strings.Contains(err.Error(), "differs from generated candidate") || calls != 1 {
				t.Fatalf("changed documents reached next provider: calls=%d err=%v", calls, err)
			}
		})
	}
}
