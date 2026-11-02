package owneractions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// FeatureEffects exposes fixed unbound draft and reviewed feature effects.
type FeatureEffects interface {
	DraftAvailable() bool
	ConfirmAvailable() bool
	Draft(context.Context, string, string, string, string, string, time.Time) (owneraction.Result, error)
	Confirm(context.Context, string, string, string, string, time.Time) (owneraction.Result, error)
	RecoverFeature(context.Context, string, string, string, time.Time) (string, bool, error)
}

// Actions owns feature preview and research answer decisions over exact targets.
type Actions struct {
	FallbackTransport string
	Targets           *Targets
	Validate          func(owneraction.Invocation) (owneraction.Invocation, error)
	Protect           func(context.Context, string) (string, error)
	Research          ResearchAnswers
	Answer            AnswerAdmission
	Features          FeatureEffects
}

// Selection retains only saved action target facts and the transient project focus.
type Selection struct {
	Invocation                                 owneraction.Invocation
	PreviewID, PreviewHash, Question, Revision string
	RepoID                                     int64
	Project, Channel, Transport                string
	ProjectPending                             bool
	Focus                                      string
}

// Normalize resolves the existing action terms before they are frozen.
func (a Actions) Normalize(ctx context.Context, p *Selection, at time.Time) (string, error) {
	args := p.Invocation.Arguments
	if p.Invocation.Command == "draft" || p.Invocation.Command == "answer" || args["project"] != "" {
		selected, err := a.Targets.ResolveProject(ctx, args, p.Focus)
		if err != nil {
			return "", err
		}
		if selected.Refusal != "" {
			p.ProjectPending = true
			return selected.Refusal, nil
		}
		args["project"] = selected.Repository.Slug()
		p.Project = args["project"]
		p.RepoID = selected.Repository.ID
		if err = a.Targets.CheckResolution(ctx, selected.Repository); err != nil {
			return "", err
		}
	}
	for _, field := range []string{"note", "text", "question"} {
		if args[field] != "" && a.Protect != nil {
			v, err := a.Protect(ctx, args[field])
			if err != nil {
				return "", err
			}
			args[field] = v
		}
	}
	switch p.Invocation.Command {
	case "answer":
		ref, _, refusal := a.Targets.ResearchTarget(ctx, args, true)
		if refusal != "" {
			return refusal, nil
		}
		if a.Research == nil {
			return "No research answer is available here.", nil
		}
		q, err := a.Research.Current(ctx, ref)
		if err != nil {
			return "", err
		}
		if q.Text == "" || q.Token != args["revision"] {
			return "Please provide the current research question revision from /question.", nil
		}
		p.Question, p.Revision = q.Text, q.Token
	case "confirm":
		preview, err := a.Targets.db.ChatIssuePreviewByCode(ctx, p.Transport, p.Channel, args["code"])
		if errors.Is(err, store.ErrNotFound) {
			return "Please provide the current preview code from /draft.", nil
		}
		if err != nil {
			return "", err
		}
		if preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The saved preview is no longer available; create a new draft.", nil
		}
		repos, err := a.Targets.db.ListRepos(ctx)
		if err != nil {
			return "", err
		}
		for _, repo := range repos {
			if repo.ID == preview.RepoID {
				if err = a.Targets.CheckResolution(ctx, repository(repo)); err != nil {
					return "", err
				}
				p.Project = repo.Slug()
			}
		}
		if p.Project == "" {
			return "The preview project is no longer registered.", nil
		}
		p.RepoID, p.PreviewID, p.PreviewHash, p.Question = preview.RepoID, preview.ID, preview.ContentHash, preview.Title+"\n"+preview.Body
	}
	return "", nil
}

// RecheckSelection verifies saved action terms against current registered state.
func (a Actions) RecheckSelection(ctx context.Context, p Selection, at time.Time) (string, error) {
	if _, err := a.Validate(p.Invocation); err != nil {
		return "This action is no longer enabled; request a fresh proposal.", nil //nolint:nilerr // A retired catalogue selection is a final owner-visible refusal.
	}
	if p.RepoID > 0 {
		if err := a.Targets.RecheckProject(ctx, p.Project, p.RepoID); err != nil {
			if errors.Is(err, ErrProjectChanged) {
				return err.Error(), nil //nolint:nilerr // Invalid saved selections are owner-visible refusals, not retryable failures.
			}
			return "", err
		}
	}
	if p.Invocation.Command == "answer" {
		ref, _, refusal := a.Targets.ResearchTarget(ctx, p.Invocation.Arguments, true)
		if refusal != "" {
			return refusal, nil
		}
		if a.Research == nil {
			return "Research answers are unavailable; no answer was recorded. Open " + researchIssueURL(ref) + " to inspect its current question before sending another answer.", nil
		}
		q, err := a.Research.Current(ctx, ref)
		if err != nil {
			return "", err
		}
		if q.Token != p.Revision || q.Text != p.Question {
			return "The research question changed; request a fresh proposal.", nil
		}
	}
	if p.Invocation.Command == "confirm" {
		preview, err := a.Targets.db.ChatIssuePreviewByCode(ctx, p.Transport, p.Channel, p.Invocation.Arguments["code"])
		if errors.Is(err, store.ErrNotFound) {
			return "The preview changed; request a fresh proposal.", nil
		}
		if err != nil {
			return "", err
		}
		if preview.ID != p.PreviewID || preview.ContentHash != p.PreviewHash || preview.Title+"\n"+preview.Body != p.Question || preview.RepoID != p.RepoID || preview.State != "previewed" || !at.Before(preview.ExpiresAt) {
			return "The preview changed; request a fresh proposal.", nil
		}
	}
	if a.Protect != nil {
		for _, field := range []string{"note", "text", "question"} {
			if value := p.Invocation.Arguments[field]; value != "" {
				protected, err := a.Protect(ctx, value)
				if err != nil {
					return "", err
				}
				if protected != value {
					return "Protection changed; request a fresh proposal.", nil
				}
			}
		}
	}
	return "", nil
}
func selection(p legacyNatural) Selection {
	return Selection{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, PreviewID: p.PreviewID, PreviewHash: p.PreviewHash, Question: p.Question, Revision: p.Revision, RepoID: p.RepoID, Project: p.Project, Channel: p.Channel, Transport: p.Transport, ProjectPending: p.ProjectPending}
}

// Resolve freezes the exact reviewed action and its target.
func (a Actions) Resolve(ctx context.Context, principal owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	inv, err := a.Validate(owneraction.Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil {
		return owneraction.Facts{}, err
	}
	if a.Protect == nil {
		return owneraction.Facts{}, errors.New("action protection unavailable")
	}
	for _, value := range inv.Arguments {
		protected, e := a.Protect(ctx, value)
		if e != nil {
			return owneraction.Facts{}, e
		}
		if protected != value {
			return owneraction.Facts{}, errors.New("protection changed the exact request; revise it before proposing")
		}
	}
	p := Selection{Invocation: inv, Channel: channel(principal), Transport: principal.Transport}
	refusal, err := a.Normalize(ctx, &p, at)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if refusal != "" {
		return owneraction.Facts{}, errors.New(refusal)
	}
	f := owneraction.Facts{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: maps.Clone(p.Invocation.Arguments)}, RepositoryID: p.RepoID, TargetKind: p.Invocation.Command, TargetID: p.Project, Estimate: "unknown"}
	if p.RepoID > 0 {
		repo, e := a.Targets.db.GetRepo(ctx, p.RepoID)
		if e != nil {
			return f, e
		}
		f.RepositoryRef = repo.Forge + ":" + repo.Slug()
	}
	switch inv.Command {
	case "confirm":
		f.TargetID = p.PreviewID
		f.TargetRevision = p.PreviewHash
	case "answer":
		f.TargetID = p.Project + "#" + inv.Arguments["issue"]
		f.TargetRevision = p.Revision
	case "draft":
		f.TargetRevision = fmt.Sprint(p.RepoID)
		f.Summary = "Create a feature preview from the fixed idea"
	default:
		return f, ErrInvalidSelection
	}
	if f.Summary == "" {
		f.Summary = f.Invocation.Command + " " + f.Invocation.Arguments["action"]
	}
	natural := legacyNatural{Invocation: legacyInvocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, PreviewID: p.PreviewID, PreviewHash: p.PreviewHash, Question: p.Question, Revision: p.Revision, RepoID: p.RepoID, Project: p.Project, Channel: p.Channel, Transport: p.Transport, ProjectPending: p.ProjectPending}
	raw, err := json.Marshal(diagnosisPayload{Natural: natural})
	f.Payload = string(raw)
	return f, err
}

// Recheck checks the saved target before an effect.
func (a Actions) Recheck(ctx context.Context, _ owneraction.Principal, f owneraction.Facts, at time.Time) error {
	p, err := unpackDiagnosis(f)
	if err != nil {
		return err
	}
	saved := selection(p.Natural)
	if saved.Transport == "" {
		saved.Transport = a.FallbackTransport
	}
	refusal, err := a.RecheckSelection(ctx, saved, at)
	if err != nil {
		return err
	}
	if refusal != "" {
		return errors.New(refusal)
	}
	return nil
}

// Execute performs the claimed action after its final target recheck.
func (a Actions) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	payload, err := unpackDiagnosis(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	if err = a.Recheck(ctx, p, f, at); err != nil {
		return owneraction.Result{State: "failed", Summary: err.Error()}, nil //nolint:nilerr // A pre-effect refusal is a factual failed result, never a retry.
	}
	result := owneraction.Result{State: "succeeded"}
	switch f.Invocation.Command {
	case "draft":
		if a.Features == nil || !a.Features.DraftAvailable() {
			return owneraction.Result{State: "failed", Summary: "Drafting is unavailable. No issue was created; inspect factory drafting setup before a fresh proposal."}, nil
		}
		result, err = a.Features.Draft(ctx, p.Transport, channel(p), key, payload.Natural.Project, f.Invocation.Arguments["note"], at)
	case "confirm":
		if a.Features == nil || !a.Features.ConfirmAvailable() {
			return owneraction.Result{State: "failed", Summary: "Feature creation is unavailable. No issue was created; inspect factory confirmation setup before a fresh proposal."}, nil
		}
		result, err = a.Features.Confirm(ctx, p.Transport, channel(p), key, f.Invocation.Arguments["code"], at)
	case "answer":
		ref, _, refusal := a.Targets.ResearchTarget(ctx, f.Invocation.Arguments, true)
		if refusal != "" {
			result = owneraction.Result{State: "failed", Summary: refusal}
		} else {
			result, err = a.Answer.Answer(ctx, ref, f.Invocation.Arguments, at)
		}
	default:
		return result, ErrInvalidSelection
	}
	if result.State == "" {
		result.State = "succeeded"
	}
	if err == nil {
		result.Summary, err = a.Protect(ctx, result.Summary)
	}
	if errors.Is(err, store.ErrConnectorInvalid) || errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorScope) {
		result.State = "failed"
		return result, nil
	}
	return result, err
}

// Recover reads retained evidence without replaying an uncertain effect.
func (a Actions) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	if _, err := unpackDiagnosis(f); err != nil {
		return owneraction.Result{}, false, err
	}
	if f.Invocation.Command == "confirm" {
		preview, e := a.Targets.db.ChatIssuePreviewByCode(ctx, p.Transport, channel(p), f.Invocation.Arguments["code"])
		if e != nil {
			if errors.Is(e, store.ErrNotFound) {
				return owneraction.Result{}, false, nil
			}
			return owneraction.Result{}, false, e
		}
		if preview.ID != f.TargetID || preview.ContentHash != f.TargetRevision || preview.RepoID != f.RepositoryID {
			return owneraction.Result{}, false, errors.New("reviewed feature preview changed")
		}
		repo, e := a.Targets.db.GetRepo(ctx, preview.RepoID)
		if e != nil {
			return owneraction.Result{}, false, e
		}
		if repo.Forge+":"+repo.Slug() != f.RepositoryRef {
			return owneraction.Result{}, false, errors.New("reviewed feature repository changed")
		}
		if preview.State == "created" {
			return owneraction.Result{State: "succeeded", Summary: "Feature issue: " + preview.IssueLink}, true, nil
		}
		if a.Features != nil {
			text, found, e := a.Features.RecoverFeature(ctx, p.Transport, channel(p), f.Invocation.Arguments["code"], at)
			if e != nil {
				return owneraction.Result{}, false, e
			}
			if found {
				safe, e := a.Protect(ctx, text)
				return owneraction.Result{State: "succeeded", Summary: safe}, true, e
			}
		}
	}
	if f.Invocation.Command == "draft" {
		preview, e := a.Targets.db.ChatIssuePreviewForMessage(ctx, p.Transport, key)
		if e == nil {
			return owneraction.Result{State: "succeeded", Summary: preview.Title + "\n" + preview.Body, Reference: preview.ID}, true, nil
		}
	}
	return owneraction.Result{}, false, nil
}
