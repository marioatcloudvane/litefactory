package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
)

type graphDeliveryForge struct {
	binding                                             GraphDeliveryBinding
	pr                                                  github.DeliveryPR
	issue                                               github.Issue
	main, remote                                        string
	creates, merges, closes                             int
	loseMerge, raceMain, losePR, loseClose, refuseMerge bool
	beforeMerge                                         func() error
	listRepo, createRepo, mergeRepo, closeRepo          github.Repo
}

func (f *graphDeliveryForge) BaseURL() string { return "https://github.com" }
func (f *graphDeliveryForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	return f.issue, nil
}
func (f *graphDeliveryForge) GetDeliveryHead(_ context.Context, _ github.Repo, branch string) (string, error) {
	if branch == "main" {
		return f.main, nil
	}
	return f.remote, nil
}
func (f *graphDeliveryForge) ListDeliveryPRs(_ context.Context, repo github.Repo, _ string) ([]github.DeliveryPR, error) {
	f.listRepo = repo
	if f.pr.ID == 0 {
		return nil, nil
	}
	return []github.DeliveryPR{f.pr}, nil
}
func (f *graphDeliveryForge) GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error) {
	return f.pr, nil
}
func (f *graphDeliveryForge) CreateDeliveryPR(_ context.Context, repo github.Repo, branch, target string, title, body redact.Body) (github.DeliveryPR, error) {
	f.createRepo = repo
	f.creates++
	f.pr = github.DeliveryPR{ID: 80, Number: 4, CreatorID: f.binding.BotID, Head: f.remote, Branch: branch, Target: target, HeadRepo: "acme/acme-api", BaseRepo: "acme/acme-api", State: "open", Body: body.String()}
	if f.losePR {
		return f.pr, errors.New("lost create acknowledgment")
	}
	return f.pr, nil
}
func (f *graphDeliveryForge) MergeDeliveryPR(_ context.Context, repo github.Repo, _ int, head string) error {
	f.mergeRepo = repo
	f.merges++
	if f.beforeMerge != nil {
		if err := f.beforeMerge(); err != nil {
			return err
		}
	}
	if f.refuseMerge {
		f.refuseMerge = false
		return &github.APIError{StatusCode: 405, Message: "Required status check is expected"}
	}
	if head != f.pr.Head {
		return errors.New("expected head guard failed")
	}
	f.pr.Merged = true
	f.pr.State = "closed"
	f.pr.MergeSHA = strings.Repeat("f", 40)
	f.main = f.pr.MergeSHA
	if f.loseMerge {
		return errors.New("lost merge acknowledgment")
	}
	return nil
}
func (f *graphDeliveryForge) GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error) {
	base := f.main
	if f.raceMain {
		base = strings.Repeat("e", 40)
	}
	return github.DeliveryCommit{SHA: f.pr.MergeSHA, Parents: []string{base, f.pr.Head}}, nil
}
func (f *graphDeliveryForge) CloseDeliveredParent(_ context.Context, repo github.Repo, _ int, _ int64) error {
	f.closeRepo = repo
	f.closes++
	if f.loseClose {
		f.loseClose = false
		return errors.New("lost closure acknowledgment")
	}
	f.issue.State = "closed"
	return nil
}

func graphFinalizationFixture(t *testing.T) (*fixture, GraphDelivery, ActivityRequest, *graphDeliveryForge, string) {
	t.Helper()
	f := newFixture(t, nil)
	head := strings.Repeat("a", 40)
	req := ActivityRequest{Mode: agent.ModeBug, Workflow: agent.WorkflowBug, InstanceID: 1, NodeAttemptID: 1, Node: "deliver"}
	binding := GraphDeliveryBinding{Record: "delivery_binding", InstanceID: 1, SessionID: f.session.ID, IssueID: 123, BotID: 99, IssueNumber: f.session.IssueNumber, Repository: github.Repo{Owner: "acme", Name: "acme-api"}, Branch: "lf/repair", Target: "main", Summary: "Fix #123 from the reported bug", Marker: "<!-- lf:delivery exact -->"}
	forge := &graphDeliveryForge{binding: binding, main: strings.Repeat("c", 40), remote: strings.Repeat("0", 40), issue: github.Issue{ID: 123, Number: f.session.IssueNumber, State: "open"}}
	handler := GraphDelivery{SyncBase: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
		return syncBaseOutput{Result: "current", Commit: head}, nil
	}, Forge: forge, Redactor: redact.New(redact.Options{Environ: []string{}}), Bind: func(context.Context, Options, ActivityRequest) (GraphDeliveryBinding, error) {
		return binding, nil
	}, Save: func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error) {
		return head, nil
	}, PublishSaved: func(context.Context, Options, ActivityRequest, string) error {
		forge.remote = head
		return nil
	}}
	return f, handler, req, forge, head
}

// Delivery reconciles against GitHub: a lost reply is resolved by looking, a
// refused step parks and the owner's reply re-runs it from the top, and main
// moving on is never a reason to stop.
func TestGraphDeliveryReconcilesAndRetries(t *testing.T) {
	for _, variant := range []string{"complete", "lost merge reply", "lost PR reply", "lost close reply", "main moved", "merge refused", "already merged"} {
		t.Run(variant, func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			forge.loseMerge = variant == "lost merge reply"
			forge.losePR = variant == "lost PR reply"
			forge.loseClose = variant == "lost close reply"
			forge.refuseMerge = variant == "merge refused"
			if variant == "main moved" {
				forge.main = strings.Repeat("e", 40)
			}
			if variant == "already merged" {
				forge.remote = head
				forge.pr = github.DeliveryPR{ID: 80, Number: 4, Head: head, Branch: "lf/repair", Target: "main", State: "closed", Merged: true, MergeSHA: strings.Repeat("f", 40)}
			}
			out, e := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, e)
			retried := variant == "merge refused" || variant == "lost close reply"
			if retried {
				if out.Outcome != "blocked" || !strings.Contains(out.Diagnostic, "PR #4") {
					t.Fatalf("first attempt=%+v", out)
				}
				out, e = h.Execute(t.Context(), f.opts, req)
				mustProgress(t, e)
			}
			if out.Outcome != "completed" || forge.issue.State != "closed" || !forge.pr.Merged {
				t.Fatalf("outcome=%+v pr=%+v", out, forge.pr)
			}
			wantCreates, wantMerges := 1, 1
			if variant == "merge refused" {
				wantMerges = 2
			}
			if variant == "already merged" {
				wantCreates, wantMerges = 0, 0
			}
			if forge.creates != wantCreates || forge.merges != wantMerges {
				t.Fatalf("creates=%d merges=%d", forge.creates, forge.merges)
			}
		})
	}
}

func TestGraphDeliverySyncOutcomesNeverMergeUncheckedSource(t *testing.T) {
	for _, state := range []string{"synced", "retry", "conflict", "drift", "error"} {
		t.Run(state, func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			h.SyncBase = func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
				if forge.remote != head {
					t.Fatal("sync ran before retained repair was saved and published")
				}
				if state == "error" {
					return syncBaseOutput{}, errors.New("fetch failed")
				}
				result := state
				if state == "drift" {
					result = "current"
				}
				return syncBaseOutput{Result: result, Commit: "changed", Conflicts: []string{"shared_test.go"}, Reason: "controlled refusal"}, nil
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			want := "revalidate"
			if state == "conflict" || state == "error" {
				want = "blocked"
			}
			if out.Outcome != want || forge.merges != 0 || forge.closes != 0 {
				t.Fatalf("unchecked delivery: %+v merges=%d closes=%d", out, forge.merges, forge.closes)
			}
			if state == "conflict" && (!strings.Contains(out.Diagnostic, "shared_test.go") || !strings.Contains(out.Diagnostic, "controlled refusal")) {
				t.Fatalf("not actionable: %s", out.Diagnostic)
			}
		})
	}
}

func TestGraphDeliveryRechecksBaseOnlyOnConflictRefusal(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprint(conflict), func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			calls := 0
			h.SyncBase = func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error) {
				calls++
				if calls == 1 {
					return syncBaseOutput{Result: "current", Commit: head}, nil
				}
				return syncBaseOutput{Result: "synced", Commit: "new base merge"}, nil
			}
			forge.beforeMerge = func() error {
				message := "Required status check is expected"
				if conflict {
					message = "Pull Request has merge conflicts"
				}
				return &github.APIError{StatusCode: 405, Message: message}
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			want, wantCalls := "blocked", 1
			if conflict {
				want, wantCalls = "revalidate", 2
			}
			if out.Outcome != want || calls != wantCalls || forge.closes != 0 {
				t.Fatalf("out=%+v syncs=%d closes=%d", out, calls, forge.closes)
			}
		})
	}
}

func TestGraphDeliveryTicketContainer(t *testing.T) {
	for _, linear := range []bool{false, true} {
		t.Run(fmt.Sprintf("linear=%t", linear), func(t *testing.T) {
			f, h, req, forge, head := graphFinalizationFixture(t)
			binding := forge.binding
			wantTicketRepo := binding.Repository
			if linear {
				binding.TicketRepository = github.Repo{Owner: "team", Name: "project"}
				binding.IssueReference = "MAR-52"
				wantTicketRepo = binding.TicketRepository
			}
			h.Bind = func(context.Context, Options, ActivityRequest) (GraphDeliveryBinding, error) {
				return binding, nil
			}
			var syncRepo github.Repo
			h.SyncBase = func(_ context.Context, _ Options, _ ActivityRequest, b GraphDeliveryBinding, _ string) (syncBaseOutput, error) {
				syncRepo = b.Repository
				return syncBaseOutput{Result: "current", Commit: head}, nil
			}
			out, err := h.Execute(t.Context(), f.opts, req)
			mustProgress(t, err)
			var progress graphDeliveryProgress
			mustProgress(t, json.Unmarshal(out.Output, &progress))
			if out.Outcome != "completed" || !progress.Closed || forge.closes != 1 || forge.closeRepo != wantTicketRepo {
				t.Fatalf("out=%+v closed=%t close repo=%+v want=%+v", out, progress.Closed, forge.closeRepo, wantTicketRepo)
			}
			if forge.listRepo != binding.Repository || forge.createRepo != binding.Repository || forge.mergeRepo != binding.Repository || syncRepo != binding.Repository {
				t.Fatalf("code repos: list=%+v create=%+v merge=%+v sync=%+v", forge.listRepo, forge.createRepo, forge.mergeRepo, syncRepo)
			}
		})
	}
}
