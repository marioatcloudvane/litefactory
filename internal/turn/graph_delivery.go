package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// DeliveryOperations are workflow-neutral remote operations. Admission remains
// in the workflow adapter; these gateways confer no approval of their own.
type DeliveryOperations interface {
	BugPRForge
	BugMergeForge
	BugCloseForge
}

// GraphDeliveryBinding names the issue, branch and target a repair is delivered to.
type GraphDeliveryBinding struct {
	Record                                string `json:"record"`
	InstanceID, SessionID, IssueID, BotID int64
	IssueNumber                           int
	IssueReference                        string
	Repository                            github.Repo
	Branch, Target, Marker, Predecessor   string
	Eligibility                           GraphBugRepairEligibility
}

// GraphBugFinalization delivers an accepted repair by reconciling against git
// and GitHub: every step observes the current state and performs only the next
// missing action, so re-running it after any failure is always safe. Nothing is
// recorded as uncertain; a failure parks with GitHub's reason and an owner
// reply re-runs this step from the top.
type GraphBugFinalization struct {
	Forge    DeliveryOperations
	Code     BugCodeForge
	Tickets  BugCloseForge
	Redactor *redact.Redactor
	Resolve  func(context.Context, Options, ActivityRequest, GraphBugRepairEligibility) (GraphDeliveryBinding, error)
	// Save commits the worktree (a no-op when clean) and returns HEAD.
	Save func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error)
	// SyncBase synchronizes the saved head; any changed source requires revalidation.
	SyncBase func(context.Context, Options, ActivityRequest, GraphDeliveryBinding, string) (syncBaseOutput, error)
	// PublishSaved pushes the task branch; pushing an unchanged branch is a no-op.
	PublishSaved func(context.Context, Options, ActivityRequest, string) error
}

type graphDeliveryProgress struct {
	Record              string `json:"record"`
	Outcome, Reason     string
	Binding             GraphDeliveryBinding
	Commit              string
	PR                  github.DeliveryPR
	Integration, Closed bool
}

// Execute saves, pushes, opens or finds the PR, merges it and closes the issue.
func (h GraphBugFinalization) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if h.Code == nil {
		h.Code = h.Forge
	}
	if h.Tickets == nil {
		h.Tickets = h.Forge
	}
	out := graphDeliveryProgress{Record: "delivery_progress"}
	finish := func(outcome, reason string) (ActivityResult, error) {
		out.Outcome, out.Reason = outcome, reason
		raw, e := json.Marshal(out)
		diagnostic := reason
		if out.Commit != "" {
			diagnostic += "\nSaved commit: " + out.Commit
		}
		if out.PR.Number > 0 {
			diagnostic += fmt.Sprintf("\nPR: #%d, merged=%t", out.PR.Number, out.PR.Merged)
		}
		return ActivityResult{Outcome: outcome, Diagnostic: diagnostic, Kind: store.GraphOutputJSON, Output: raw}, e
	}
	if opts.DB == nil || (h.Code == nil || h.Tickets == nil) || h.Redactor == nil || h.Resolve == nil || h.Save == nil || h.PublishSaved == nil || h.SyncBase == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("incomplete graph finalization factory bindings")
	}
	eligibility, e := CurrentGraphBugRepairEligibility(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return finish("blocked", "No accepted successful repair to deliver: "+e.Error())
	}
	binding, e := h.Resolve(ctx, opts, req, eligibility)
	if e != nil {
		return finish("blocked", "Resolving the delivery target failed: "+e.Error())
	}
	out.Binding = binding
	repo := binding.Repository

	head, e := h.Save(ctx, opts, req, binding)
	if e != nil {
		return finish("blocked", "Saving the repair failed: "+e.Error())
	}
	out.Commit = head

	pr, found, e := h.findPR(ctx, binding, head)
	if e != nil {
		return finish("blocked", "Listing PRs for "+binding.Branch+" failed: "+e.Error())
	}
	if !found || !pr.Merged {
		if e = h.PublishSaved(ctx, opts, req, head); e != nil {
			return finish("blocked", "Pushing "+binding.Branch+" failed: "+e.Error())
		}
		sync := func() (ActivityResult, bool, error) {
			state, err := h.SyncBase(ctx, opts, req, binding, head)
			if err != nil {
				res, e := finish("blocked", "Synchronizing the repair failed: "+err.Error())
				return res, true, e
			}
			switch state.Result {
			case "synced", "retry":
				res, e := finish("revalidate", "The branch was synchronized or moved; rerun the approved repair checks on the current head. "+state.Reason)
				return res, true, e
			case "current":
				if state.Commit == head {
					return ActivityResult{}, false, nil
				}
				res, e := finish("revalidate", "The current branch differs from the checked repair; rerun its checks.")
				return res, true, e
			default:
				res, e := finish("blocked", fmt.Sprintf("Bringing %s into %s could not complete. Conflicting files: %s. %s. Reply with how to resolve these conflicts.", binding.Target, binding.Branch, strings.Join(state.Conflicts, ", "), state.Reason))
				return res, true, e
			}
		}
		if res, stop, err := sync(); stop {
			return res, err
		}
		if !found {
			title, body, pe := h.presentation(ctx, binding)
			if pe != nil {
				return finish("blocked", "Rendering the PR failed: "+pe.Error())
			}
			created, ce := h.Code.CreateDeliveryPR(ctx, repo, binding.Branch, binding.Target, title, body)
			if ce != nil {
				// A lost reply may still have created it; look before reporting.
				if pr, found, e = h.findPR(ctx, binding, head); e != nil || !found {
					return finish("blocked", "Creating the PR failed: "+ce.Error())
				}
			} else {
				pr = created
			}
		}
		out.PR = pr
		if me := h.Code.MergeDeliveryPR(ctx, repo, pr.Number, head); me != nil {
			// The merge may have happened despite the error; look before reporting.
			if again, ge := h.Code.GetDeliveryPR(ctx, repo, pr.Number); ge != nil || !again.Merged {
				var api *github.APIError
				if ge == nil && errors.As(me, &api) && api.StatusCode == http.StatusMethodNotAllowed && strings.Contains(strings.ToLower(api.Message), "merge conflict") {
					if res, stop, err := sync(); stop {
						return res, err
					}
				}
				return finish("blocked", fmt.Sprintf("Merging PR #%d failed: %s. Reply to retry once GitHub allows the merge.", pr.Number, me.Error()))
			}
		}
		if pr, e = h.Code.GetDeliveryPR(ctx, repo, pr.Number); e != nil {
			return finish("blocked", fmt.Sprintf("Reading PR #%d after merging failed: %s", out.PR.Number, e.Error()))
		}
		if !pr.Merged {
			return finish("blocked", fmt.Sprintf("PR #%d is not merged yet. Reply to retry.", pr.Number))
		}
	}
	out.PR = pr
	out.Integration = true

	if e = h.Tickets.CloseDeliveredParent(ctx, repo, binding.IssueNumber, binding.IssueID); e != nil {
		return finish("blocked", fmt.Sprintf("Merged PR #%d, but closing issue %s failed: %s", pr.Number, binding.displayReference(), e.Error()))
	}
	out.Closed = true
	return finish("completed", fmt.Sprintf("Merged PR #%d and closed issue %s", pr.Number, binding.displayReference()))
}

// findPR returns this branch's PR into the target: a merged one carrying head,
// else an open one. A closed or older merged PR does not count, so a new
// repair generation gets a new PR.
func (h GraphBugFinalization) findPR(ctx context.Context, b GraphDeliveryBinding, head string) (github.DeliveryPR, bool, error) {
	if h.Code == nil {
		h.Code = h.Forge
	}
	prs, e := h.Code.ListDeliveryPRs(ctx, b.Repository, b.Branch)
	if e != nil {
		return github.DeliveryPR{}, false, e
	}
	var open github.DeliveryPR
	for _, p := range prs {
		if p.Branch != b.Branch || p.Target != b.Target || p.Number < 1 {
			continue
		}
		if p.Merged && p.Head == head {
			return p, true, nil
		}
		if p.State == "open" && open.Number == 0 {
			open = p
		}
	}
	return open, open.Number > 0, nil
}

func (h GraphBugFinalization) presentation(ctx context.Context, b GraphDeliveryBinding) (redact.Body, redact.Body, error) {
	summary := b.Eligibility.Repair.Summary
	// Neutralize automatic closure phrases in untrusted summaries. The factory
	// closes the issue itself after the merge, never through a PR keyword.
	summary = regexp.MustCompile(`(?i)\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\b`).ReplaceAllString(summary, "address")
	// Shorten before redaction: an over-budget title would get the redactor's
	// multi-line truncation notice, which GitHub titles cannot carry.
	title, e := h.Redactor.Apply(ctx, graphDeliveryTitle("Bug repair: "+strings.Split(summary, "\n")[0]), 256)
	if e != nil {
		return redact.Body{}, redact.Body{}, e
	}
	body, e := h.Redactor.Apply(ctx, fmt.Sprintf("%s\n\nIssue %s. Approved diagnosis: %s\n\n%s", summary, b.displayReference(), b.Eligibility.Handoff.Candidate.Digest, b.Marker), 65536)
	return title, body, e
}

// graphDeliveryTitle cuts a one-line title at a word boundary within limit bytes.
func graphDeliveryTitle(s string) string {
	const limit = 160
	s = strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(s))
	if len(s) <= limit {
		return s
	}
	end := 0
	for i := range s {
		if i > limit-len("…") {
			break
		}
		end = i
	}
	cut := s[:end]
	if i := strings.LastIndexByte(cut, ' '); i > len(cut)/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:") + "…"
}

func (b GraphDeliveryBinding) displayReference() string {
	if b.IssueReference != "" {
		return b.IssueReference
	}
	return "this issue"
}
