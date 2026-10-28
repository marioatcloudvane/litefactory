package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
	"github.com/cloudvane/litefactory/internal/store"
)

// frozenStandards is supplied by daemon composition over the existing services.
type frozenStandards interface {
	ResolveFrozen(context.Context, owneraction.Principal, StandardsRequest) (owneraction.Facts, error)
	RecheckFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) error
	ExecuteFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) (StandardsResult, error)
	RecoverFrozen(context.Context, owneraction.Principal, StandardsRequest, owneraction.Facts) (StandardsResult, bool, error)
}

type ownerActionPayload struct {
	Natural       naturalPayload    `json:"natural"`
	Domain        owneraction.Facts `json:"domain"`
	DomainPayload string            `json:"domain_payload,omitempty"`
}

// OwnerActionDomain exposes the existing resolvers and domain handlers through
// the shared confirmation service. It does not admit or claim execution.
type OwnerActionDomain struct{ Service *Service }

func actionChannel(p owneraction.Principal) string {
	if p.Transport == "mcp" {
		return "mcp:" + p.GrantID
	}
	return p.ChannelID
}

func (b OwnerActionDomain) request(principal owneraction.Principal, f owneraction.Facts, key string, at time.Time) (StandardsRequest, error) {
	op, err := standardsOperation(Invocation{Command: f.Invocation.Command, Arguments: f.Invocation.Arguments})
	owner := principal.ActorID()
	return StandardsRequest{Operation: op, Arguments: maps.Clone(f.Invocation.Arguments), Transport: principal.Transport, OwnerID: owner, ChannelID: actionChannel(principal), MessageID: key, OperationKey: key, At: at}, err
}

// Resolve freezes factory selections before the proposal becomes available.
func (b OwnerActionDomain) Resolve(ctx context.Context, principal owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	s := b.Service
	inv, effect, err := s.ValidateInvocation(Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil || effect != ChangeEffect {
		return owneraction.Facts{}, ErrInvalidInvocation
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
	p := naturalPayload{Invocation: inv, Channel: actionChannel(principal), Transport: principal.Transport, freezeTargets: true}
	refusal, err := s.resolveNatural(ctx, &p, at)
	if err != nil {
		return owneraction.Facts{}, err
	}
	if refusal != "" {
		return owneraction.Facts{}, errors.New(refusal)
	}
	f := owneraction.Facts{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: maps.Clone(p.Invocation.Arguments)}, RepositoryID: p.RepoID, RepositoryRef: "", TargetKind: p.Invocation.Command, TargetID: p.Project, Estimate: "unknown"}
	if p.RepoID > 0 {
		repo, e := s.db.GetRepo(ctx, p.RepoID)
		if e != nil {
			return f, e
		}
		f.RepositoryRef = repo.Forge + ":" + repo.Slug()
	}
	domain := owneraction.Facts{}
	if standardsCommand(inv.Command) {
		adapter, ok := s.standards.(frozenStandards)
		if !ok {
			return f, errors.New("exact standards actions unavailable")
		}
		req, e := b.request(principal, f, "", at)
		if e != nil {
			return f, e
		}
		domain, err = adapter.ResolveFrozen(ctx, principal, req)
		if err != nil {
			return f, err
		}
		f = domain
		if missing := standardsMissingTerms(req.Operation, f.Invocation.Arguments); missing != "" {
			return f, errors.New(missing)
		}
		p.Invocation = Invocation{Command: f.Invocation.Command, Arguments: maps.Clone(f.Invocation.Arguments)}
	} else {
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
		case "diagnose":
			repo, e := s.db.GetRepo(ctx, p.RepoID)
			if e != nil {
				return f, e
			}
			session, e := s.knownNumericTask(ctx, repo, mustNumber(inv.Arguments["issue"]))
			if e != nil {
				return f, e
			}
			f.TargetID = session.TaskRef
			f.TargetRevision = store.OwnerActionSessionRevision(session)
		default:
			return f, ErrInvalidInvocation
		}
	}
	if f.Summary == "" {
		f.Summary = f.Invocation.Command + " " + f.Invocation.Arguments["action"]
	}
	if f.Estimate == "" {
		f.Estimate = "unknown"
	}
	raw, err := json.Marshal(ownerActionPayload{Natural: p, Domain: domain, DomainPayload: domain.Payload})
	if err != nil {
		return f, err
	}
	f.Payload = string(raw)
	return f, nil
}

func mustNumber(raw string) int { n, _ := strconv.Atoi(raw); return n }

func (b OwnerActionDomain) unpack(f owneraction.Facts) (ownerActionPayload, error) {
	var p ownerActionPayload
	err := json.Unmarshal([]byte(f.Payload), &p)
	p.Domain.Payload = p.DomainPayload
	if err == nil && (p.Natural.Invocation.Command != f.Invocation.Command || !maps.Equal(p.Natural.Invocation.Arguments, f.Invocation.Arguments)) {
		err = errors.New("protected action changed")
	}
	return p, err
}

// Recheck keeps target and current domain authority independent of transport.
func (b OwnerActionDomain) Recheck(ctx context.Context, principal owneraction.Principal, f owneraction.Facts, at time.Time) error {
	p, err := b.unpack(f)
	if err != nil {
		return err
	}
	refusal, err := b.Service.recheckNatural(ctx, p.Natural, at)
	if err != nil {
		return err
	}
	if refusal != "" {
		return errors.New(refusal)
	}
	if f.Invocation.Command == "diagnose" {
		repo, err := b.Service.db.GetRepo(ctx, f.RepositoryID)
		if err != nil {
			return err
		}
		session, err := b.Service.knownNumericTask(ctx, repo, mustNumber(f.Invocation.Arguments["issue"]))
		if err != nil {
			return err
		}
		if session.TaskRef != f.TargetID || store.OwnerActionSessionRevision(session) != f.TargetRevision {
			return errors.New("reviewed diagnosis target changed; request a fresh proposal")
		}
	}
	if standardsCommand(f.Invocation.Command) {
		req, err := b.request(principal, f, "", at)
		if err != nil {
			return err
		}
		adapter, ok := b.Service.standards.(frozenStandards)
		if !ok {
			return errors.New("exact standards actions unavailable")
		}
		return adapter.RecheckFrozen(ctx, principal, req, p.Domain)
	}
	return nil
}

// Execute uses existing domain operations after the shared journal claim.
func (b OwnerActionDomain) Execute(ctx context.Context, principal owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	s := b.Service
	p, err := b.unpack(f)
	if err != nil {
		return owneraction.Result{}, err
	}
	if err = b.Recheck(ctx, principal, f, at); err != nil {
		return owneraction.Result{State: "failed", Summary: err.Error()}, nil //nolint:nilerr // A pre-effect refusal is a factual failed result, never a retry.
	}
	result := owneraction.Result{State: "succeeded"}
	work, assessment := presentation.WithAssessment(ctx)
	if standardsCommand(f.Invocation.Command) {
		req, e := b.request(principal, f, key, at)
		if e != nil {
			return result, e
		}
		adapter, ok := s.standards.(frozenStandards)
		if !ok {
			return result, errors.New("exact standards actions unavailable")
		}
		out, e := adapter.ExecuteFrozen(work, principal, req, p.Domain)
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
	switch f.Invocation.Command {
	case "draft":
		if s.drafter == nil || s.drafter.Model == nil || s.drafter.Redactor == nil || s.drafter.Store == nil || s.drafter.Repositories == nil {
			return owneraction.Result{State: "failed", Summary: "Drafting is unavailable. No issue was created; inspect factory drafting setup before a fresh proposal."}, nil
		}
		drafter := *s.drafter
		drafter.Bind = nil
		drafter.History = nil
		result.Summary, _, err = drafter.Reply(work, principal.Transport, actionChannel(principal), key, p.Natural.Project, f.Invocation.Arguments["note"], at)
	case "confirm":
		if s.confirmer == nil {
			return owneraction.Result{State: "failed", Summary: "Feature creation is unavailable. No issue was created; inspect factory confirmation setup before a fresh proposal."}, nil
		}
		result.Summary, err = s.confirmer.Confirm(work, principal.Transport, actionChannel(principal), key, f.Invocation.Arguments["code"], at)
	case "answer":
		result.Summary, err = s.researchAnswer(work, Message{Transport: principal.Transport, ID: key, Channel: actionChannel(principal), Sender: principal.OwnerID, Command: "answer", Arguments: maps.Clone(f.Invocation.Arguments)}, at)
	case "diagnose":
		diagnosis, ok := s.diagnosis.(interface {
			OwnerActionDiagnosis(context.Context, string, string, string) (string, error)
		})
		if !ok {
			return owneraction.Result{State: "failed", Summary: "Diagnosis is unavailable. No diagnosis was started; inspect factory diagnosis setup before a fresh proposal."}, nil
		}
		result.Summary, err = diagnosis.OwnerActionDiagnosis(work, f.TargetID, f.Invocation.Arguments["note"], key)
	default:
		err = ErrInvalidInvocation
	}
	switch assessment.Status {
	case presentation.Uncertain:
		result.State = "uncertain"
	case presentation.Refusal, presentation.ReportableError:
		result.State = "failed"
	}
	if err == nil {
		result.Summary, err = s.Protect(ctx, result.Summary)
	}
	if errors.Is(err, store.ErrConnectorInvalid) || errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorScope) {
		result.State = "failed"
		return result, nil
	}
	return result, err
}

// Recover reads domain receipts; it never invokes an uncertain effect again.
func (b OwnerActionDomain) Recover(ctx context.Context, principal owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, bool, error) {
	p, err := b.unpack(f)
	if err != nil {
		return owneraction.Result{}, false, err
	}
	if standardsCommand(f.Invocation.Command) {
		req, e := b.request(principal, f, key, at)
		if e != nil {
			return owneraction.Result{}, false, e
		}
		adapter, ok := b.Service.standards.(frozenStandards)
		if !ok {
			return owneraction.Result{}, false, errors.New("exact standards recovery unavailable")
		}
		out, found, e := adapter.RecoverFrozen(ctx, principal, req, p.Domain)
		state := "succeeded"
		if out.Interrupted {
			state = "uncertain"
		}
		if out.Status != "" {
			state = out.Status
		}
		if e == nil {
			out.Text, e = b.Service.Protect(ctx, out.Text)
		}
		return owneraction.Result{State: state, Summary: out.Text}, found, e
	}
	if f.Invocation.Command == "diagnose" {
		if recovery, ok := b.Service.diagnosis.(interface {
			RecoverOwnerActionDiagnosis(context.Context, string, string) (owneraction.Result, bool, error)
		}); ok {
			result, found, err := recovery.RecoverOwnerActionDiagnosis(ctx, f.TargetID, key)
			if err == nil && found {
				result.Summary, err = b.Service.Protect(ctx, result.Summary)
			}
			return result, found, err
		}
	}
	if f.Invocation.Command == "confirm" {
		preview, e := b.Service.db.ChatIssuePreviewByCode(ctx, principal.Transport, actionChannel(principal), f.Invocation.Arguments["code"])
		if e != nil {
			if errors.Is(e, store.ErrNotFound) {
				return owneraction.Result{}, false, nil
			}
			return owneraction.Result{}, false, e
		}
		if preview.ID != f.TargetID || preview.ContentHash != f.TargetRevision || preview.RepoID != f.RepositoryID {
			return owneraction.Result{}, false, errors.New("reviewed feature preview changed")
		}
		repo, e := b.Service.db.GetRepo(ctx, preview.RepoID)
		if e != nil {
			return owneraction.Result{}, false, e
		}
		if repo.Forge+":"+repo.Slug() != f.RepositoryRef {
			return owneraction.Result{}, false, errors.New("reviewed feature repository changed")
		}
		if preview.State == "created" {
			return owneraction.Result{State: "succeeded", Summary: "Feature issue: " + preview.IssueLink}, true, nil
		}
		if recovery, ok := b.Service.confirmer.(interface {
			RecoverOwnerActionFeature(context.Context, string, string, string, time.Time) (string, bool, error)
		}); ok {
			text, found, e := recovery.RecoverOwnerActionFeature(ctx, principal.Transport, actionChannel(principal), f.Invocation.Arguments["code"], at)
			if e != nil {
				return owneraction.Result{}, false, e
			}
			if found {
				safe, e := b.Service.Protect(ctx, text)
				return owneraction.Result{State: "succeeded", Summary: safe}, true, e
			}
		}
	}

	if f.Invocation.Command == "draft" {
		preview, e := b.Service.db.ChatIssuePreviewForMessage(ctx, principal.Transport, key)
		if e == nil {
			return owneraction.Result{State: "succeeded", Summary: preview.Title + "\n" + preview.Body, Reference: preview.ID}, true, nil
		}
	}
	return owneraction.Result{}, false, nil
}

// ActionCatalog derives discovery from enabled command definitions and fixed policy.
func (b OwnerActionDomain) ActionCatalog() []owneraction.ActionDefinition {
	var out []owneraction.ActionDefinition
	for _, def := range b.Service.NaturalCommands() {
		if def.Effect != ChangeEffect {
			continue
		}
		actions := []string{""}
		switch def.Name {
		case "standards-author":
			actions = []string{"start", "answer", "revise", "edit", "continue", "validate", "complete", "rebase", "publish"}
		case "standards-proposal":
			actions = []string{"edit", "approve", "publish"}
		case "standards-exception":
			actions = []string{"propose", "approve", "revoke"}
		case "standards-analysis":
			actions = []string{"start"}
		}
		for _, action := range actions {
			tier, scope, err := owneraction.Classify(owneraction.Invocation{Command: def.Name, Arguments: map[string]string{"action": action}})
			if err != nil {
				continue
			}
			entry := owneraction.ActionDefinition{Command: def.Name, Subaction: action, Tier: tier, Scope: scope}
			for _, arg := range def.Arguments {
				entry.Arguments = append(entry.Arguments, owneraction.ArgumentDefinition{Name: arg.Name, Type: string(arg.Type), Required: arg.Required, MaxBytes: argumentLimit(def.Name, arg.Name)})
			}
			out = append(out, entry)
		}
	}
	return out
}
