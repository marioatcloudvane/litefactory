package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugrepair"
	"github.com/cloudvane/litefactory/internal/git"
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

// GraphDeliveryBinding identifies the approved tested generation and delivery target.
type GraphDeliveryBinding struct {
	Record                                string `json:"record"`
	InstanceID, SessionID, IssueID, BotID int64
	IssueNumber                           int
	Repository                            github.Repo
	Branch, Target, Marker, Predecessor   string
	Eligibility                           GraphBugRepairEligibility
}

// GraphBugFinalization coordinates evidence-bound factory effects without running tests.
type GraphBugFinalization struct {
	Forge    DeliveryOperations
	Redactor *redact.Redactor
	Resolve  func(context.Context, Options, ActivityRequest, GraphBugRepairEligibility) (GraphDeliveryBinding, error)
	Observe  func(context.Context, Options, ActivityRequest) (GraphBugRepairInputs, error)
	LogRoot  func(ActivityRequest) string
	// Save is local only and must reconcile the retained original content/HEAD.
	Save         func(context.Context, Options, ActivityRequest, GraphDeliveryBinding) (string, error)
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

// Execute reconciles retained effects before advancing guarded delivery or closure.
func (h GraphBugFinalization) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	out := graphDeliveryProgress{Record: "delivery_progress"}
	finish := func(outcome, reason string) (ActivityResult, error) {
		out.Outcome, out.Reason = outcome, reason
		raw, e := json.Marshal(out)
		diagnostic := reason
		if out.Commit != "" {
			diagnostic += "\nSaved evidenced commit: " + out.Commit
		}
		if out.PR.ID > 0 {
			diagnostic += fmt.Sprintf("\nRetained PR: #%d (numeric ID %d), merged=%t", out.PR.Number, out.PR.ID, out.PR.Merged)
		}
		return ActivityResult{Outcome: outcome, Diagnostic: diagnostic, Kind: store.GraphOutputJSON, Output: raw}, e
	}
	if opts.DB == nil || opts.MCP == nil || opts.MCP.Now == nil || h.Forge == nil || h.Redactor == nil || h.Resolve == nil || h.Observe == nil || h.LogRoot == nil || h.Save == nil || h.PublishSaved == nil || req.Workflow != agent.WorkflowBug || req.Mode != "" {
		return ActivityResult{}, errors.New("incomplete graph finalization factory bindings")
	}
	eligibility, e := CurrentGraphBugRepairEligibility(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return ActivityResult{}, e
	}
	binding, e := h.Resolve(ctx, opts, req, eligibility)
	if e != nil {
		return ActivityResult{}, e
	}
	if binding.InstanceID != req.InstanceID || binding.SessionID < 1 || binding.IssueID < 1 || binding.BotID < 1 || binding.IssueNumber < 1 || binding.Branch == "" || binding.Target != "main" || binding.Marker == "" || !reflect.DeepEqual(binding.Eligibility, eligibility) {
		return ActivityResult{}, errors.New("delivery authority binding mismatch")
	}
	out.Binding = binding
	raw, e := json.Marshal(binding)
	if e != nil {
		return ActivityResult{}, e
	}
	// Persist each generation independently; immutable predecessors retain effects.
	_, e = opts.DB.RecordGraphOutput(ctx, store.GraphOutput{NodeAttemptID: req.NodeAttemptID, Kind: store.GraphOutputJSON, Payload: raw, CreatedAt: opts.MCP.Now()})
	if e != nil {
		return ActivityResult{}, e
	}
	effect := func(kind string, payload any) (store.GraphEffect, error) {
		b, e := json.Marshal(payload)
		if e != nil {
			return store.GraphEffect{}, e
		}
		key := fmt.Sprintf("delivery:%d:%s:%s", req.InstanceID, eligibility.Repair.Validation.Generation, kind)
		if kind == "pr" || kind == "close" {
			key = fmt.Sprintf("delivery:%d:%s", req.InstanceID, kind)
		}
		source := req.NodeAttemptID
		old, e := opts.DB.GetGraphEffect(ctx, key)
		if e == nil {
			if old.InstanceID != req.InstanceID || old.Kind != kind || !bytes.Equal(old.Binding, b) {
				return old, errors.New("retained immutable delivery effect binding differs")
			}
			return old, nil
		}
		if !errors.Is(e, store.ErrNotFound) {
			return old, e
		}
		return opts.DB.RetainGraphEffect(ctx, store.GraphEffect{Operation: key, InstanceID: req.InstanceID, SourceAttemptID: source, Kind: kind, Binding: b, CreatedAt: opts.MCP.Now()})
	}
	observe := func(f store.GraphEffect, state string, v any) error {
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return opts.DB.ObserveGraphEffect(ctx, f.Operation, req.NodeAttemptID, state, b, opts.MCP.Now())
	}
	claim := func(f store.GraphEffect) error {
		if f.State == "refused" {
			if e := observe(f, "intent", struct{ Reason string }{"Prior provider refusal established nonexecution; current binding independently rechecked"}); e != nil {
				return e
			}
		}
		_, e := opts.DB.ClaimGraphEffect(ctx, f.Operation, req.NodeAttemptID, opts.MCP.Now())
		return e
	}
	// Remote PR observation precedes freshness checks: confirmed integration may
	// continue only closure even though integration has necessarily moved main.
	prs, e := h.Forge.ListDeliveryPRs(ctx, binding.Repository, binding.Branch)
	if e != nil {
		return finish("blocked", e.Error())
	}
	var pr github.DeliveryPR
	for _, p := range prs {
		if strings.Contains(p.Body, binding.Marker) {
			if pr.ID != 0 {
				return finish("blocked", "Ambiguous durable PR identity")
			}
			if p.CreatorID != binding.BotID || p.HeadRepo != binding.Repository.Owner+"/"+binding.Repository.Name || p.BaseRepo != p.HeadRepo || p.Branch != binding.Branch || p.Target != binding.Target || p.ID < 1 || p.Number < 1 {
				return finish("blocked", "PR provenance differs from bound repository and bot")
			}
			pr = p
		}
	}
	if pr.ID != 0 {
		identified := pr
		pr, e = h.Forge.GetDeliveryPR(ctx, binding.Repository, pr.Number)
		if e != nil {
			return finish("blocked", e.Error())
		}
		if pr.ID != identified.ID || pr.Number != identified.Number || pr.CreatorID != identified.CreatorID || pr.Branch != binding.Branch || pr.Target != binding.Target || pr.HeadRepo != identified.HeadRepo || pr.BaseRepo != identified.BaseRepo || !strings.Contains(pr.Body, binding.Marker) {
			return finish("blocked", "Independent PR provenance changed")
		}
		out.PR = pr
	}
	mergeEffect, e := effect("merge", binding)
	if e != nil {
		return ActivityResult{}, e
	}
	history, readErr := opts.DB.ListGraphEffects(ctx, req.InstanceID)
	if readErr != nil {
		return ActivityResult{}, readErr
	}
	for _, prior := range history {
		if prior.Kind == "merge" && prior.Operation != mergeEffect.Operation && (prior.State == "confirmed" || prior.State == "dispatched" || prior.State == "uncertain") {
			return finish("blocked", "Prior generation has a dispatched, uncertain or confirmed merge; no second merge or repair is authorized")
		}
	}
	if pr.Merged {
		var saved struct{ Commit string }
		saveEffect, se := effect("save", binding)
		if se != nil {
			return ActivityResult{}, se
		}
		if saveEffect.State != "confirmed" || json.Unmarshal(saveEffect.Observation, &saved) != nil || saved.Commit == "" {
			return finish("blocked", "Merged effect has no confirmed evidenced saved head")
		}
		out.Commit = saved.Commit
		if e = h.confirmIntegration(ctx, binding, pr, saved.Commit); e != nil {
			if oe := observe(mergeEffect, "uncertain", struct {
				PR     github.DeliveryPR
				Reason string
			}{pr, e.Error()}); oe != nil {
				return ActivityResult{}, oe
			}
			return finish("blocked", "Merged but unverified: "+e.Error())
		}
		if e = observe(mergeEffect, "confirmed", pr); e != nil {
			return ActivityResult{}, e
		}
		out.Integration = true
	} else {
		if mergeEffect.State == "dispatched" || mergeEffect.State == "uncertain" {
			return finish("blocked", "Merge outcome remains uncertain; an open PR does not prove nonexecution")
		}
		if e = h.fresh(ctx, opts, req, eligibility); e != nil {
			// Only an exact claimed continuation at this origin permits revalidation.
			wait, we := opts.DB.GetGraphOwnerWait(ctx, req.InstanceID)
			if we == nil && wait.State == "allocated" && wait.ContinuationAttemptID == req.NodeAttemptID && wait.OriginNode == req.Node {
				return finish("revalidate", e.Error())
			}
			return finish("stale", e.Error())
		}
		saveEffect, e := effect("save", binding)
		if e != nil {
			return ActivityResult{}, e
		}
		var saved struct{ Commit string }
		if saveEffect.State == "confirmed" {
			if e = json.Unmarshal(saveEffect.Observation, &saved); e != nil {
				return ActivityResult{}, e
			}
		} else {
			if saveEffect.State == "intent" {
				if e = claim(saveEffect); e != nil {
					return ActivityResult{}, e
				}
			}
			// Save's git reconciliation checks original HEAD, parent, content and
			// operation identity before any mutation, including after lost ack.
			saved.Commit, e = h.Save(ctx, opts, req, binding)
			if e != nil {
				if oe := observe(saveEffect, "uncertain", struct{ Reason string }{e.Error()}); oe != nil {
					return ActivityResult{}, oe
				}
				return finish("blocked", e.Error())
			}
			if e = h.fresh(ctx, opts, req, eligibility); e != nil {
				return finish("stale", e.Error())
			}
			if e = observe(saveEffect, "confirmed", saved); e != nil {
				return ActivityResult{}, e
			}
		}
		if saved.Commit == "" {
			return finish("blocked", "Saved source identity is absent")
		}
		out.Commit = saved.Commit
		pushEffect, e := effect("push", struct {
			Binding GraphDeliveryBinding
			Commit  string
		}{binding, saved.Commit})
		if e != nil {
			return ActivityResult{}, e
		}
		remote, re := h.Forge.GetDeliveryHead(ctx, binding.Repository, binding.Branch)
		if re != nil && (pushEffect.State != "intent" || !graphDeliveryNotFound(re)) {
			return finish("blocked", "Remote push state cannot be observed: "+re.Error())
		}
		if remote != saved.Commit {
			if pushEffect.State == "dispatched" || pushEffect.State == "uncertain" {
				return finish("blocked", "Remote head differs after uncertain push; nonexecution is not established")
			}
			if e = h.fresh(ctx, opts, req, eligibility); e != nil {
				return finish("stale", e.Error())
			}
			if e = claim(pushEffect); e != nil {
				return ActivityResult{}, e
			}
			if e = h.PublishSaved(ctx, opts, req, saved.Commit); e != nil {
				if oe := observe(pushEffect, "uncertain", struct{ Reason string }{e.Error()}); oe != nil {
					return ActivityResult{}, oe
				}
				return finish("blocked", e.Error())
			}
			remote, e = h.Forge.GetDeliveryHead(ctx, binding.Repository, binding.Branch)
			if e != nil || remote != saved.Commit {
				reason := "Push confirmation differs"
				if e != nil {
					reason = e.Error()
				}
				if oe := observe(pushEffect, "uncertain", struct{ Reason string }{reason}); oe != nil {
					return ActivityResult{}, oe
				}
				return finish("blocked", reason)
			}
		}
		if e = observe(pushEffect, "confirmed", struct{ Head string }{remote}); e != nil {
			return ActivityResult{}, e
		}
		prEffect, e := effect("pr", struct {
			Repository             github.Repo
			Branch, Target, Marker string
			BotID                  int64
		}{binding.Repository, binding.Branch, binding.Target, binding.Marker, binding.BotID})
		if e != nil {
			return ActivityResult{}, e
		}
		if pr.ID == 0 {
			if prEffect.State != "intent" && prEffect.State != "refused" {
				return finish("blocked", "PR creation unresolved; absent listing does not establish nonexecution")
			}
			title, body, e := h.presentation(ctx, binding)
			if e != nil {
				return ActivityResult{}, e
			}
			if e = claim(prEffect); e != nil {
				return ActivityResult{}, e
			}
			created, createErr := h.Forge.CreateDeliveryPR(ctx, binding.Repository, binding.Branch, binding.Target, title, body)
			if createErr != nil {
				if oe := observe(prEffect, graphDeliveryFailureState(createErr), struct{ Reason string }{createErr.Error()}); oe != nil {
					return ActivityResult{}, oe
				}
				return finish("blocked", createErr.Error())
			}
			pr, e = h.Forge.GetDeliveryPR(ctx, binding.Repository, created.Number)
			if e != nil {
				return finish("blocked", "PR creation dispatched; independent observation unavailable")
			}
			if pr.ID != created.ID || pr.CreatorID != binding.BotID || pr.Branch != binding.Branch || pr.Target != binding.Target || pr.HeadRepo != binding.Repository.Owner+"/"+binding.Repository.Name || pr.BaseRepo != pr.HeadRepo || !strings.Contains(pr.Body, binding.Marker) {
				return finish("blocked", "Created PR independent identity differs")
			}
			out.PR = pr
		}
		if pr.State != "open" || pr.Head != saved.Commit {
			return finish("blocked", "PR is closed unmerged or its head differs from tested commit")
		}
		if e = observe(prEffect, "confirmed", pr); e != nil {
			return ActivityResult{}, e
		}
		if e = h.fresh(ctx, opts, req, eligibility); e != nil {
			return finish("stale", e.Error())
		}
		remote, e = h.Forge.GetDeliveryHead(ctx, binding.Repository, binding.Branch)
		if e != nil || remote != saved.Commit {
			return finish("blocked", "Remote head changed before merge")
		}
		main, e := h.Forge.GetDeliveryHead(ctx, binding.Repository, binding.Target)
		if e != nil {
			return finish("blocked", e.Error())
		}
		if main != eligibility.Inputs.TargetMain {
			return finish("stale", "Target main changed before merge")
		}
		if e = claim(mergeEffect); e != nil {
			return ActivityResult{}, e
		}
		if e = h.Forge.MergeDeliveryPR(ctx, binding.Repository, pr.Number, saved.Commit); e != nil {
			if oe := observe(mergeEffect, graphDeliveryFailureState(e), struct{ Reason string }{e.Error()}); oe != nil {
				return ActivityResult{}, oe
			}
			return finish("blocked", e.Error())
		}
		pr, e = h.Forge.GetDeliveryPR(ctx, binding.Repository, pr.Number)
		if e != nil {
			return finish("blocked", "Merge dispatched; independent observation unavailable")
		}
		out.PR = pr
		if e = h.confirmIntegration(ctx, binding, pr, saved.Commit); e != nil {
			if oe := observe(mergeEffect, "uncertain", struct {
				PR     github.DeliveryPR
				Reason string
			}{pr, e.Error()}); oe != nil {
				return ActivityResult{}, oe
			}
			return finish("blocked", e.Error())
		}
		if e = observe(mergeEffect, "confirmed", pr); e != nil {
			return ActivityResult{}, e
		}
		out.Integration = true
	}
	closeEffect, e := effect("close", struct {
		Repository github.Repo
		IssueID    int64
		Number     int
	}{binding.Repository, binding.IssueID, binding.IssueNumber})
	if e != nil {
		return ActivityResult{}, e
	}
	issue, e := h.Forge.GetIssue(ctx, binding.Repository, binding.IssueNumber)
	if e != nil {
		return finish("blocked", e.Error())
	}
	if issue.ID != binding.IssueID || issue.Number != binding.IssueNumber {
		return finish("blocked", "Closure numeric issue identity mismatch")
	}
	if issue.State != "closed" {
		if closeEffect.State == "dispatched" || closeEffect.State == "uncertain" {
			return finish("blocked", "Closure outcome remains uncertain")
		}
		if e = claim(closeEffect); e != nil {
			return ActivityResult{}, e
		}
		if e = h.Forge.CloseDeliveredParent(ctx, binding.Repository, binding.IssueNumber, binding.IssueID); e != nil {
			if oe := observe(closeEffect, graphDeliveryFailureState(e), struct{ Reason string }{e.Error()}); oe != nil {
				return ActivityResult{}, oe
			}
			return finish("blocked", e.Error())
		}
		issue, e = h.Forge.GetIssue(ctx, binding.Repository, binding.IssueNumber)
		if e != nil || issue.ID != binding.IssueID || issue.State != "closed" {
			return finish("blocked", "Closure dispatched but not independently confirmed")
		}
	}
	if e = observe(closeEffect, "confirmed", issue); e != nil {
		return ActivityResult{}, e
	}
	out.Closed = true
	return finish("completed", "Confirmed integration of the authorized tested head and confirmed issue closure")
}

func (h GraphBugFinalization) fresh(ctx context.Context, opts Options, req ActivityRequest, want GraphBugRepairEligibility) error {
	current, e := CurrentGraphBugRepairEligibility(ctx, opts.DB, req.InstanceID)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(current, want) {
		return store.ErrBugRepairUnauthorized
	}
	actual, e := h.Observe(ctx, opts, req)
	if e != nil {
		return e
	}
	digest, e := bugrepair.ValidationInputsDigest(actual.Commands, actual.InputManifest)
	if e != nil {
		return e
	}
	if digest != actual.InputsDigest || actual.Source.Digest != want.Inputs.Source.Digest || actual.TargetMain != want.Inputs.TargetMain || actual.InputsDigest != want.Inputs.InputsDigest {
		return errors.New("tested source, target main or relevant validation inputs changed")
	}
	v := want.Repair.Validation
	for _, c := range v.Commands {
		observed, e := git.ObserveEvidenceLog(ctx, h.LogRoot(req), c.Log.Path)
		if e != nil {
			return e
		}
		if observed != c.Log.SHA256 {
			return errors.New("validation log changed")
		}
	}
	for _, c := range v.Conditions {
		observed, e := git.ObserveEvidenceLog(ctx, h.LogRoot(req), c.Log.Path)
		if e != nil {
			return e
		}
		if observed != c.Log.SHA256 {
			return errors.New("condition log changed")
		}
	}
	return nil
}
func (h GraphBugFinalization) confirmIntegration(ctx context.Context, b GraphDeliveryBinding, p github.DeliveryPR, head string) error {
	if !p.Merged || p.Head != head || p.MergeSHA == "" || p.Branch != b.Branch || p.Target != b.Target || p.CreatorID != b.BotID || !strings.Contains(p.Body, b.Marker) {
		return errors.New("PR integration binding differs")
	}
	commit, e := h.Forge.GetDeliveryCommit(ctx, b.Repository, p.MergeSHA)
	if e != nil {
		return e
	}
	if commit.SHA != p.MergeSHA || len(commit.Parents) != 2 || commit.Parents[0] != b.Eligibility.Inputs.TargetMain || commit.Parents[1] != head {
		return errors.New("merge parents differ from validated main and tested head")
	}
	main, e := h.Forge.GetDeliveryHead(ctx, b.Repository, b.Target)
	if e != nil {
		return e
	}
	if main != commit.SHA {
		frontier := []string{main}
		seen := map[string]bool{}
		found := false
		for range 256 { // fixed ancestry visit budget, not a retry
			if len(frontier) == 0 {
				break
			}
			sha := frontier[0]
			frontier = frontier[1:]
			if sha == commit.SHA {
				found = true
				break
			}
			if seen[sha] {
				continue
			}
			seen[sha] = true
			ancestor, e := h.Forge.GetDeliveryCommit(ctx, b.Repository, sha)
			if e != nil {
				return e
			}
			if ancestor.SHA != sha || len(ancestor.Parents) > 64 {
				return errors.New("main ancestry identity differs")
			}
			frontier = append(frontier, ancestor.Parents...)
		}
		if !found {
			return errors.New("intended main does not independently confirm the bound merge")
		}
	}
	return nil
}
func (h GraphBugFinalization) presentation(ctx context.Context, b GraphDeliveryBinding) (redact.Body, redact.Body, error) {
	summary := b.Eligibility.Repair.Summary
	// Neutralize automatic closure phrases in untrusted summaries. Explicit issue
	// closure follows independent integration proof, never a PR keyword.
	summary = regexp.MustCompile(`(?i)\b(close[sd]?|fix(e[sd])?|resolve[sd]?)\b`).ReplaceAllString(summary, "address")
	// Shorten before redaction: an over-budget title would get the redactor's
	// multi-line truncation notice, which GitHub titles cannot carry.
	title, e := h.Redactor.Apply(ctx, graphDeliveryTitle("Bug repair: "+strings.Split(summary, "\n")[0], 160), 256)
	if e != nil {
		return redact.Body{}, redact.Body{}, e
	}
	body, e := h.Redactor.Apply(ctx, fmt.Sprintf("%s\n\nApproved diagnosis: %s\nTested source: %s\nTarget main: %s\nValidation generation: %s\n\n%s", summary, b.Eligibility.Handoff.Candidate.Digest, b.Eligibility.Inputs.Source.Digest, b.Eligibility.Inputs.TargetMain, b.Eligibility.Repair.Validation.Generation, b.Marker), 65536)
	return title, body, e
}

func graphDeliveryNotFound(err error) bool {
	var response interface{ HTTPStatusCode() int }
	if errors.As(err, &response) {
		return response.HTTPStatusCode() == http.StatusNotFound
	}
	// The existing typed GitHub response exposes the authoritative HTTP status.
	var api *github.APIError
	return errors.As(err, &api) && api.StatusCode == http.StatusNotFound
}

// Only an authenticated gateway response that rejects the mutation request
// establishes nonexecution. Transport errors, 5xx, and observation mismatches
// remain uncertain even when the remote object currently looks unchanged.
// graphDeliveryTitle cuts a one-line title at a word boundary within max bytes.
func graphDeliveryTitle(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r", " "))
	if len(s) <= max {
		return s
	}
	cut := max - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	if i := strings.LastIndexByte(s[:cut], ' '); i > max/2 {
		cut = i
	}
	return strings.TrimRight(s[:cut], " ,.;:") + "…"
}

func graphDeliveryFailureState(err error) string {
	// A request refused locally was never sent, so nonexecution is certain.
	if errors.Is(err, github.ErrInvalidArgument) {
		return "refused"
	}
	var api *github.APIError
	if errors.As(err, &api) && (api.StatusCode == http.StatusForbidden || api.StatusCode == http.StatusUnprocessableEntity) {
		return "refused"
	}
	return "uncertain"
}
