package owneractions

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// FrozenStandards supplies existing standards domain operations over exact facts.
type FrozenStandards interface {
	ResolveFrozen(context.Context, owneraction.Principal, StandardsRequest) (owneraction.Facts, error)
	RecheckFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) error
	ExecuteFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) (StandardsResult, error)
	RecoverFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) (StandardsResult, bool, error)
}

// Standards owns standards action normalization, frozen requests and recovery.
type Standards struct {
	Targets   *Targets
	Available bool
	Frozen    FrozenStandards
	Validate  func(owneraction.Invocation) (owneraction.Invocation, error)
	Protect   func(context.Context, string) (string, error)
}

// StandardsSelection carries only the normalized target needed by consumers.
type StandardsSelection struct {
	Invocation      owneraction.Invocation
	RepoID          int64
	Project, Window string
}

const standardsUnavailable = "Standards operations are unavailable; no standards action was performed. Ask the factory operator to inspect standards service availability using `litefactory logs --all` on the factory host."

// Normalize resolves the existing action terms before they are frozen.
func (s Standards) Normalize(ctx context.Context, p *StandardsSelection, freeze bool, at time.Time) (string, error) {
	op, err := StandardsOperation(p.Invocation)
	if err != nil {
		return err.Error(), nil //nolint:nilerr // Invalid saved selections are owner-visible refusals, not retryable failures.
	}
	if !s.Available {
		return standardsUnavailable, nil
	}
	args := p.Invocation.Arguments
	if args["project"] != "" && args["repository_id"] == "" {
		resolution, e := s.Targets.ResolveProject(ctx, args, "")
		if e != nil {
			return "", e
		}
		if resolution.Refusal != "" {
			return resolution.Refusal, nil
		}
		p.RepoID = resolution.Repository.ID
		p.Project = resolution.Repository.Slug()
		args["project"] = p.Project
		args["repository_id"] = strconv.FormatInt(p.RepoID, 10)
	}
	if raw := args["repository_id"]; raw != "" {
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			return "", ErrInvalidSelection
		}
		repo, e := s.Targets.db.GetRepo(ctx, id)
		if e != nil {
			return "", e
		}
		if e = s.Targets.CheckResolution(ctx, repository(repo)); e != nil {
			return "", e
		}
		p.RepoID = repo.ID
		p.Project = repo.Slug()
	}
	if op == "analysis.start" && args["window"] != "" {
		after, before, ok := AnalysisWindow(args["window"], at)
		if !ok {
			return AnalysisWindowRefusal, nil
		}
		args["after"], args["before"] = after, before
		p.Window = args["window"]
		delete(args, "window")
	}
	if p.Invocation.Command != "standards" && !freeze {
		return StandardsMissingTerms(op, p.Invocation.Arguments), nil
	}
	return "", nil
}

// ErrInvalidSelection is translated to the consumer's existing input error.
var ErrInvalidSelection = errors.New("invalid factory action selection")

// RecheckSelection verifies saved action terms against current registered state.
func (s Standards) RecheckSelection(ctx context.Context, p StandardsSelection) (string, error) {
	if !s.Available {
		return standardsUnavailable, nil
	}
	if _, err := StandardsOperation(p.Invocation); err != nil {
		return err.Error(), nil //nolint:nilerr // Invalid saved selections are owner-visible refusals, not retryable failures.
	}
	if p.RepoID > 0 {
		if err := s.Targets.RecheckProject(ctx, p.Project, p.RepoID); err != nil {
			// Keep infrastructure errors distinct from a changed project refusal.
			if errors.Is(err, ErrProjectChanged) {
				return "The registered standards project changed. Request a fresh proposal.", nil
			}
			return "", err
		}
	}
	for _, value := range p.Invocation.Arguments {
		protected, err := s.Protect(ctx, value)
		if err != nil {
			return "", err
		}
		if protected != value {
			return "Protection changed the exact standards request. Request a fresh proposal.", nil
		}
	}
	return "", nil
}
func standardsRequest(p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (StandardsRequest, error) {
	op, err := StandardsOperation(f.Invocation)
	return StandardsRequest{Operation: op, Arguments: maps.Clone(f.Invocation.Arguments), Transport: p.Transport, OwnerID: p.ActorID(), ChannelID: channel(p), MessageID: key, OperationKey: key, At: at}, err
}

// Resolve freezes the exact reviewed action and its target.
func (s Standards) Resolve(ctx context.Context, principal owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	inv, err := s.Validate(owneraction.Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil {
		return owneraction.Facts{}, err
	}
	if s.Protect == nil {
		return owneraction.Facts{}, errors.New("action protection unavailable")
	}
	for _, value := range inv.Arguments {
		protected, e := s.Protect(ctx, value)
		if e != nil {
			return owneraction.Facts{}, e
		}
		if protected != value {
			return owneraction.Facts{}, errors.New("protection changed the exact request; revise it before proposing")
		}
	}
	selection := StandardsSelection{Invocation: inv}
	refusal, err := s.Normalize(ctx, &selection, true, at)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if refusal != "" {
		return owneraction.Facts{}, errors.New(refusal)
	}
	f := owneraction.Facts{Invocation: selection.Invocation, RepositoryID: selection.RepoID, TargetKind: selection.Invocation.Command, TargetID: selection.Project, Estimate: "unknown"}
	if selection.RepoID > 0 {
		repo, e := s.Targets.db.GetRepo(ctx, selection.RepoID)
		if e != nil {
			return f, e
		}
		f.RepositoryRef = repo.Forge + ":" + repo.Slug()
	}
	if s.Frozen == nil {
		return f, errors.New("exact standards actions unavailable")
	}
	req, err := standardsRequest(principal, f, "", at)
	if err != nil {
		return f, err
	}
	domain, err := s.Frozen.ResolveFrozen(ctx, principal, req)
	if err != nil {
		return f, err
	}
	f = domain
	if missing := StandardsMissingTerms(req.Operation, f.Invocation.Arguments); missing != "" {
		return f, errors.New(missing)
	}
	if f.Summary == "" {
		f.Summary = f.Invocation.Command + " " + f.Invocation.Arguments["action"]
	}
	if f.Estimate == "" {
		f.Estimate = "unknown"
	}
	natural := legacyNatural{Invocation: legacyInvocation{Command: f.Invocation.Command, Arguments: maps.Clone(f.Invocation.Arguments)}, RepoID: selection.RepoID, Project: selection.Project, Channel: channel(principal), Transport: principal.Transport}
	raw, err := json.Marshal(diagnosisPayload{Natural: natural, Domain: domain, DomainPayload: domain.Payload})
	f.Payload = string(raw)
	return f, err
}
func unpackStandards(f owneraction.Facts) (diagnosisPayload, error) {
	p, err := unpackDiagnosis(f)
	p.Domain.Payload = p.DomainPayload
	return p, err
}

// Recheck checks the saved target before an effect.
func (s Standards) Recheck(ctx context.Context, p owneraction.Principal, f owneraction.Facts, at time.Time) error {
	payload, err := unpackStandards(f)
	if err != nil {
		return err
	}
	refusal, err := s.RecheckSelection(ctx, StandardsSelection{Invocation: owneraction.Invocation{Command: payload.Natural.Invocation.Command, Arguments: payload.Natural.Invocation.Arguments}, RepoID: payload.Natural.RepoID, Project: payload.Natural.Project})
	if err != nil {
		return err
	}
	if refusal != "" {
		return errors.New(refusal)
	}
	req, err := standardsRequest(p, f, "", at)
	if err != nil {
		return err
	}
	if s.Frozen == nil {
		return errors.New("exact standards actions unavailable")
	}
	return s.Frozen.RecheckFrozen(ctx, p, req, payload.Domain)
}

// Execute performs the claimed action after its final target recheck.
func (s Standards) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	payload, err := unpackStandards(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	if err = s.Recheck(ctx, p, f, at); err != nil {
		return owneraction.Result{State: "failed", Summary: err.Error()}, nil //nolint:nilerr // A pre-effect refusal is a factual failed result, never a retry.
	}
	result := owneraction.Result{State: "succeeded"}
	req, err := standardsRequest(p, f, key, at)
	if err != nil {
		return result, err
	}
	if s.Frozen == nil {
		return result, errors.New("exact standards actions unavailable")
	}
	out, e := s.Frozen.ExecuteFrozen(ctx, p, req, payload.Domain)
	result.Summary, err = s.Protect(ctx, out.Text)
	if err != nil {
		return owneraction.Result{}, err
	}
	if out.Interrupted {
		result.State = "uncertain"
	}
	if out.Status != "" {
		result.State = out.Status
	}
	if out.References["pending"] == "true" {
		result.State = "executing"
	}
	if e != nil && !out.Interrupted && (errors.Is(e, store.ErrConnectorInvalid) || errors.Is(e, store.ErrConnectorOwner) || errors.Is(e, store.ErrConnectorScope) || errors.Is(e, store.ErrStandardsConflict)) {
		result.State = "failed"
		if result.Summary == "" {
			result.Summary = "Current authority or reviewed target changed before effects; request a fresh proposal."
		}
		return result, nil
	}
	return result, e
}

// Recover reads retained evidence without replaying an uncertain effect.
func (s Standards) Recover(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	payload, err := unpackStandards(f)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	req, err := standardsRequest(p, f, key, at)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	if s.Frozen == nil {
		return owneraction.Result{}, false, errors.New("exact standards recovery unavailable")
	}
	out, found, e := s.Frozen.RecoverFrozen(ctx, p, req, payload.Domain)
	state := "succeeded"
	if out.Interrupted {
		state = "uncertain"
	}
	if out.Status != "" {
		state = out.Status
	}
	if e == nil {
		out.Text, e = s.Protect(ctx, out.Text)
	}
	return owneraction.Result{State: state, Summary: out.Text}, found, e
}
