package turn

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/teamsource"
	"github.com/cloudvane/litefactory/internal/teamsource/teamsourcetest"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

type checkoutDeliveryForge struct {
	*graphDeliveryForge
	checkout *string
}

func (f checkoutDeliveryForge) CreateDeliveryPR(ctx context.Context, repo github.Repo, branch, target string, title, body redact.Body) (github.DeliveryPR, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", *f.checkout, "rev-parse", "HEAD")
	raw, err := cmd.Output()
	if err != nil {
		return github.DeliveryPR{}, err
	}
	f.remote = strings.TrimSpace(string(raw))
	return f.graphDeliveryForge.CreateDeliveryPR(ctx, repo, branch, target, title, body)
}

func (f checkoutDeliveryForge) ListDeliveryPRs(ctx context.Context, repo github.Repo, branch string) ([]github.DeliveryPR, error) {
	if f.pr.ID != 0 && !f.pr.Merged {
		raw, e := exec.CommandContext(ctx, "git", "-C", *f.checkout, "rev-parse", "HEAD").Output()
		if e != nil {
			return nil, e
		}
		f.pr.Head = strings.TrimSpace(string(raw))
	}
	return f.graphDeliveryForge.ListDeliveryPRs(ctx, repo, branch)
}

func TestDeliveryWaitsForExactOwnerReview(t *testing.T) {
	for _, reply := range []string{"ship", "ship please", "changed head", "changed base", "changed PR", "retry close", "interrupted ship"} {
		t.Run(reply, func(t *testing.T) {
			calls := 0
			checkout := ""
			runner := lifecycleMCPHost(func(_ context.Context, req agent.Request) (*agent.RunResult, error) {
				calls++
				checkout = req.WorktreePath
				if calls == 1 {
					mustProgress(t, os.WriteFile(filepath.Join(checkout, "change.txt"), []byte("checked change\n"), 0600))
				}
				if calls == 1 {
					submitLiveResult(t, req, spikeDoneJSON)
				} else {
					client := submission.Client{DescriptorPath: req.MCPLaunch.DescriptorPath()}
					update := fmt.Appendf(nil, `{"expected_revision":%d,"operations":[{"op":"set","target":{"field":"comment"},"value":"Checked the changed candidate."}]}`, calls-1)
					if got := mcpDecodeSubmission(t, mcpToolCall(t, client, "communication_submit", update)); !got.Validation.Valid {
						t.Fatalf("update rejected: %+v", got.Validation)
					}
				}
				return &agent.RunResult{Outcome: agent.OutcomeSucceeded, FinishedAt: time.Now()}, nil
			})
			f, _, req := assembledSpikeFixture(t, runner)
			f.opts.WorkflowSource = deliveryWorkflowSource
			remote := &graphDeliveryForge{issue: github.Issue{ID: 123, Number: 123, State: "open"}}
			code := checkoutDeliveryForge{graphDeliveryForge: remote, checkout: &checkout}
			f.opts.Bug = &BugRuntime{cfg: BugRuntimeConfig{Code: code, Tickets: code}}
			_, err := Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			instance := spikeInstance(t, f)
			wait, err := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
			mustProgress(t, err)
			if remote.merges != 0 || remote.closes != 0 || calls != 1 || wait.State != "waiting" {
				t.Fatalf("prepare escaped review: %+v merges=%d closes=%d calls=%d", wait, remote.merges, remote.closes, calls)
			}
			intent, err := f.db.GetGraphComment(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			if intent.Kind != "review" {
				t.Fatalf("not a delivery review: %+v", intent)
			}
			prepared, err := preparedDelivery(t.Context(), f.db, instance.ID)
			mustProgress(t, err)
			if !strings.Contains(string(intent.Candidate), prepared.Commit) {
				t.Fatal("review omitted prepared commit")
			}
			body := reply
			if reply == "interrupted ship" {
				body = "ship"
			}
			if reply == "changed head" {
				body = "ship"
				mustProgress(t, os.WriteFile(filepath.Join(checkout, "change.txt"), []byte("unchecked\n"), 0600))
			}
			if reply == "changed base" {
				body = "ship"
				f.repo.WriteFile("base-change.txt", "new base\n")
				f.repo.Commit("Advance base after review")
				f.repo.Git("push", "origin", "main")
			}
			if reply == "changed PR" {
				body = "ship"
				remote.pr.Head = strings.Repeat("e", 40)
			}
			if reply == "retry close" {
				body = "ship"
				remote.loseClose = true
			}
			delivery, err := f.db.GetReportDelivery(t.Context(), wait.DeliveryKey)
			mustProgress(t, err)
			ownerReply := store.GraphOwnerReply{InstanceID: instance.ID, OwnerID: 7, AuthorID: 7, CommentID: testComment + 1, QuestionCommentID: delivery.CommentID, TaskRef: f.session.TaskRef, SourceRef: f.session.TaskRef, Mode: "spike", DeliveryKey: wait.DeliveryKey, Body: body, At: time.Now()}
			foreign := ownerReply
			foreign.AuthorID = 8
			if _, e := f.db.ClaimGraphOwnerReply(t.Context(), foreign); e == nil {
				t.Fatal("foreign author claimed delivery review")
			}
			_, err = f.db.ClaimGraphOwnerReply(t.Context(), ownerReply)
			mustProgress(t, err)
			if _, e := f.db.ClaimGraphOwnerReply(t.Context(), ownerReply); e == nil {
				t.Fatal("reply replay claimed twice")
			}
			req.Trigger.CommentID = ownerReply.CommentID
			req.Trigger.Body = body
			req.Command = ParseCommand(body)
			if reply == "interrupted ship" {
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				remote.beforeMerge = func() error { cancel(); return nil }
				_, e := Execute(ctx, f.opts, req)
				if e == nil || remote.merges != 1 {
					t.Fatalf("interruption not injected: %v merges=%d", e, remote.merges)
				}
				remote.beforeMerge = nil
				_, e = Execute(t.Context(), f.opts, req)
				mustProgress(t, e)
				if remote.merges != 1 || spikeInstance(t, f).Status != store.GraphSucceeded {
					t.Fatal("interrupted delivery failed to reconcile")
				}
				return
			}
			_, err = Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			if reply == "changed base" {
				for attempt := 0; attempt < 3; attempt++ {
					latest, e := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
					mustProgress(t, e)
					if latest.State == "waiting" {
						fresh, e := preparedDelivery(t.Context(), f.db, instance.ID)
						mustProgress(t, e)
						if fresh.Commit == prepared.Commit || fresh.Binding.SourceAttemptID == prepared.Binding.SourceAttemptID || calls < 2 {
							t.Fatal("stale candidate reused without new checks")
						}
						if remote.merges != 0 {
							t.Fatal("base change reused old shipping approval")
						}
						return
					}
					due, e := f.db.ListDueGraphDispatches(t.Context(), time.Now().Add(time.Hour), 10)
					mustProgress(t, e)
					if len(due) != 1 {
						t.Fatalf("missing recheck continuation: %+v", due)
					}
					_, reserved, e := f.db.ReserveGraphDispatch(t.Context(), due[0].ID, time.Now())
					mustProgress(t, e)
					if !reserved {
						t.Fatal("recheck not reserved")
					}
					req.Trigger.CommentID = due[0].TriggerID()
					req.Trigger.Body = ""
					req.Command = ParseCommand("/spike recheck")
					_, e = Execute(t.Context(), f.opts, req)
					mustProgress(t, e)
				}
				t.Fatal("base revalidation did not reach a fresh owner review")
			}
			if reply == "retry close" {
				retryWait, e := f.db.GetGraphOwnerWait(t.Context(), instance.ID)
				mustProgress(t, e)
				if retryWait.State != "waiting" || remote.merges != 1 || remote.closes != 1 {
					t.Fatalf("close failure did not park: %+v", retryWait)
				}
				posted, e := f.db.GetReportDelivery(t.Context(), retryWait.DeliveryKey)
				mustProgress(t, e)
				ownerReply.DeliveryKey = retryWait.DeliveryKey
				ownerReply.QuestionCommentID = posted.CommentID
				ownerReply.CommentID++
				ownerReply.Body = "retry"
				ownerReply.At = time.Now()
				_, e = f.db.ClaimGraphOwnerReply(t.Context(), ownerReply)
				mustProgress(t, e)
				req.Trigger.CommentID = ownerReply.CommentID
				req.Trigger.Body = "retry"
				req.Command = ParseCommand("retry")
				_, e = Execute(t.Context(), f.opts, req)
				mustProgress(t, e)
				if remote.merges != 1 || remote.closes != 2 || spikeInstance(t, f).Status != store.GraphSucceeded {
					t.Fatalf("close retry repeated merge or failed: merges=%d closes=%d", remote.merges, remote.closes)
				}
				return
			}
			if reply == "ship" || reply == "retry close" {
				if remote.merges != 1 || remote.closes != 1 || calls != 1 {
					t.Fatalf("shipping failed merges=%d closes=%d calls=%d", remote.merges, remote.closes, calls)
				}
				if spikeInstance(t, f).Status != store.GraphSucceeded {
					t.Fatal("shipping did not complete")
				}
			} else if remote.merges != 0 || remote.closes != 0 {
				t.Fatal("nonapproval or changed source shipped")
			}
		})
	}
}

func TestDeliveryPreparationNeverMergesOrCloses(t *testing.T) {
	f, h, req, forge, head := graphFinalizationFixture(t)
	h.Operation = "push"
	h.Inspect = func(context.Context, GraphDeliveryBinding) (string, string, error) { return head, "base", nil }
	got, err := h.Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if got.Outcome != "ready" || forge.merges != 0 || forge.closes != 0 {
		t.Fatalf("prepare: %+v merges=%d closes=%d", got, forge.merges, forge.closes)
	}
	forge.pr.Merged = true
	got, err = h.Execute(t.Context(), f.opts, req)
	mustProgress(t, err)
	if got.Outcome != "ready" || forge.closes != 0 {
		t.Fatalf("prepare closed an already merged candidate: %+v", got)
	}
}

const deliveryOperationFixture = `// One conversational agent authors checked results. Generic factory delivery
// prepares changed candidates, waits for an exact authenticated ship reply,
// and merges/closes only the reviewed commit. Source changes return for checks.
// Questions and ordinary review feedback resume the same agent.
digraph spike {
 graph [team="{{team}}", mode="spike", profile_version="2", start="spike"];

 spike    [lf_action="lf_agent", lf_agent_markdown="spike", lf_skills="spike-result", lf_access="write", lf_response="result", lf_outcomes="ask,done,failed", lf_limit="8"];
 prepare [lf_action="lf_deliver", lf_operation="push", lf_source="spike", lf_outcomes="ready,unchanged,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 review [lf_action="lf_ask", lf_kind="review", lf_source="spike", lf_approve_reply="ship", lf_outcomes="published,approved", lf_limit="8"];
 deliver [lf_action="lf_deliver", lf_operation="merge", lf_source="spike", lf_outcomes="merged,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 close [lf_action="lf_deliver", lf_operation="close", lf_source="spike", lf_outcomes="completed,revalidate,blocked", lf_limit="8", lf_park="owner", lf_stop="blocked,failed,invalid,exhausted"];
 ask      [lf_action="lf_ask", lf_kind="question", lf_outcomes="published", lf_source="spike", lf_limit="8"];
 report   [lf_action="lf_comment", lf_kind="final", lf_outcomes="published", lf_source="spike", lf_limit="8"];
 failure  [lf_action="lf_comment", lf_kind="failure", lf_outcomes="published", lf_limit="8"];
 waiting  [lf_terminal="true"];
 complete [lf_terminal="true"];
 stopped  [lf_terminal="true"];

 spike   -> ask [lf_on="ask"];
 spike   -> prepare  [lf_on="done"];
 prepare -> report [lf_on="unchanged"];
 prepare -> spike [lf_on="revalidate"];
 prepare -> review [lf_on="ready"];
 review -> waiting [lf_on="published"];
 review -> deliver [lf_on="approved"];
 deliver -> spike [lf_on="revalidate"];
 deliver -> close [lf_on="merged"];
 close -> complete [lf_on="completed"];
 close -> spike [lf_on="revalidate"];
 spike   -> failure  [lf_on="failed"];
 ask     -> waiting  [lf_on="published"];
 report  -> complete [lf_on="published"];
 failure -> stopped  [lf_on="published"];
}
`

func deliveryWorkflowSource(ctx context.Context, repo store.Repo, workflow agent.Workflow, specs graphexec.Registry) (*teamsource.WorkflowDefinition, error) {
	if workflow != agent.WorkflowSpike {
		return fixtureWorkflowSource(ctx, repo, workflow, specs)
	}
	root, err := teamsourcetest.Workflow(ctx, teamsourcetest.Go, "spike")
	if err != nil {
		return nil, err
	}
	files := fstest.MapFS{}
	err = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(root, p)
		if p == "spike.dot" {
			raw = []byte(strings.ReplaceAll(deliveryOperationFixture, "{{team}}", "go-development"))
		}
		files[p] = &fstest.MapFile{Data: raw}
		return err
	})
	if err != nil {
		return nil, err
	}
	return fixtureInstalled(teamsource.CaptureWorkflow(ctx, files, "spike.dot", "go-development", agent.ModeSpike, specs))
}
