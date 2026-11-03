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

// DiagnosisEffects supplies the existing diagnosis execution and receipt path.
// It cannot select or change the reviewed task.
type DiagnosisEffects interface {
	DiagnosisAvailable() bool
	Diagnose(context.Context, string, string, string) (owneraction.Result, error)
	RecoverDiagnosis(context.Context, string, string) (owneraction.Result, bool, error)
}

// Diagnosis owns frozen diagnosis targets and their effect-time checks.
type Diagnosis struct {
	Targets      *Targets
	Availability Availability
	Protect      func(context.Context, string) (string, error)
	Effects      DiagnosisEffects
}

// legacyInvocation and legacyNatural retain the saved pending-action encoding.
// The capitalized field names differ from owneraction.Invocation's JSON tags.
type legacyInvocation struct {
	Command   string
	Arguments map[string]string
}
type legacyTurn struct{ SourceID, Text string }
type legacyNatural struct {
	Turns                                      []legacyTurn     `json:"turns,omitempty"`
	Invocation                                 legacyInvocation `json:"invocation"`
	Prompt                                     string           `json:"prompt,omitempty"`
	PreviewID, PreviewHash, Question, Revision string
	RepoID                                     int64
	Project, Channel, Transport                string
	ProjectPending                             bool `json:"project_pending,omitempty"`
}
type diagnosisPayload struct {
	Natural       legacyNatural     `json:"natural"`
	Domain        owneraction.Facts `json:"domain"`
	DomainPayload string            `json:"domain_payload,omitempty"`
}

func channel(p owneraction.Principal) string {
	if p.Transport == "mcp" {
		return "mcp:" + p.GrantID
	}
	return p.ChannelID
}

// Resolve freezes the exact reviewed action and its target.
func (d Diagnosis) Resolve(ctx context.Context, p owneraction.Principal, in owneraction.Invocation, at time.Time) (owneraction.Facts, error) {
	inv, err := d.Availability.ValidateChange(owneraction.Invocation{Command: in.Command, Arguments: maps.Clone(in.Arguments)})
	if err != nil {
		return owneraction.Facts{}, err
	}
	if d.Protect == nil {
		return owneraction.Facts{}, errors.New("action protection unavailable")
	}
	for _, value := range inv.Arguments {
		protected, e := d.Protect(ctx, value)
		if e != nil {
			return owneraction.Facts{}, e
		}
		if protected != value {
			return owneraction.Facts{}, errors.New("protection changed the exact request; revise it before proposing")
		}
	}
	selected, err := d.Targets.ResolveProject(ctx, inv.Arguments, "")
	if err != nil {
		return owneraction.Facts{}, err
	}
	if selected.Refusal != "" {
		return owneraction.Facts{}, errors.New(selected.Refusal)
	}
	repo := selected.Repository
	inv.Arguments["project"] = repo.Slug()
	if err = d.Targets.CheckResolution(ctx, repo); err != nil {
		return owneraction.Facts{}, err
	}
	for _, field := range []string{"note", "text", "question"} {
		if inv.Arguments[field] != "" {
			inv.Arguments[field], err = d.Protect(ctx, inv.Arguments[field])
			if err != nil {
				return owneraction.Facts{}, err
			}
		}
	}
	number, _ := strconv.Atoi(inv.Arguments["issue"])
	task, err := d.Targets.NumericTask(ctx, repo.ID, number)
	if err != nil {
		return owneraction.Facts{}, err
	}
	facts := owneraction.Facts{Invocation: inv, RepositoryID: repo.ID, RepositoryRef: repo.Forge + ":" + repo.Slug(), TargetKind: inv.Command, TargetID: task.Ref, TargetRevision: task.Revision, Summary: inv.Command + " " + inv.Arguments["action"], Estimate: "unknown"}
	natural := legacyNatural{Invocation: legacyInvocation{Command: inv.Command, Arguments: maps.Clone(inv.Arguments)}, RepoID: repo.ID, Project: repo.Slug(), Channel: channel(p), Transport: p.Transport}
	raw, err := json.Marshal(diagnosisPayload{Natural: natural})
	facts.Payload = string(raw)
	return facts, err
}

func unpackDiagnosis(f owneraction.Facts) (diagnosisPayload, error) {
	var p diagnosisPayload
	err := json.Unmarshal([]byte(f.Payload), &p)
	if err == nil && (p.Natural.Invocation.Command != f.Invocation.Command || !maps.Equal(p.Natural.Invocation.Arguments, f.Invocation.Arguments)) {
		err = errors.New("protected action changed")
	}
	return p, err
}

// Recheck checks the saved target before an effect.
func (d Diagnosis) Recheck(ctx context.Context, _ owneraction.Principal, f owneraction.Facts, _ time.Time) error {
	p, err := unpackDiagnosis(f)
	if err != nil {
		return err
	}
	if _, err = d.Availability.ValidateChange(owneraction.Invocation{Command: p.Natural.Invocation.Command, Arguments: p.Natural.Invocation.Arguments}); err != nil {
		return errors.New("This action is no longer enabled; request a fresh proposal.") //nolint:staticcheck // Preserve the existing owner-visible refusal text.
	}
	if p.Natural.RepoID > 0 {
		if err = d.Targets.RecheckProject(ctx, p.Natural.Project, p.Natural.RepoID); err != nil {
			return err
		}
	}
	if d.Protect != nil {
		for _, field := range []string{"note", "text", "question"} {
			if value := p.Natural.Invocation.Arguments[field]; value != "" {
				protected, e := d.Protect(ctx, value)
				if e != nil {
					return e
				}
				if protected != value {
					return errors.New("Protection changed; request a fresh proposal.") //nolint:staticcheck // Preserve the existing owner-visible refusal text.
				}
			}
		}
	}
	number, _ := strconv.Atoi(f.Invocation.Arguments["issue"])
	task, err := d.Targets.NumericTask(ctx, f.RepositoryID, number)
	if err != nil {
		return err
	}
	if task.Ref != f.TargetID || task.Revision != f.TargetRevision {
		return errors.New("reviewed diagnosis target changed; request a fresh proposal")
	}
	return nil
}

// Execute performs the claimed action after its final target recheck.
func (d Diagnosis) Execute(ctx context.Context, p owneraction.Principal, f owneraction.Facts, key string, at time.Time) (owneraction.Result, error) {
	if _, err := unpackDiagnosis(f); err != nil {
		return owneraction.Result{}, err
	}
	if err := d.Recheck(ctx, p, f, at); err != nil {
		return owneraction.Result{State: "failed", Summary: err.Error()}, nil //nolint:nilerr // A pre-effect refusal is a factual failed result, never a retry.
	}
	if d.Effects == nil || !d.Effects.DiagnosisAvailable() {
		return owneraction.Result{State: "failed", Summary: "Diagnosis is unavailable. No diagnosis was started; inspect factory diagnosis setup before a fresh proposal."}, nil
	}
	result, err := d.Effects.Diagnose(ctx, f.TargetID, f.Invocation.Arguments["note"], key)
	if err == nil {
		result.Summary, err = d.Protect(ctx, result.Summary)
	}
	if errors.Is(err, store.ErrConnectorInvalid) || errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorScope) {
		result.State = "failed"
		return result, nil
	}
	return result, err
}

// Recover reads retained evidence without replaying an uncertain effect.
func (d Diagnosis) Recover(ctx context.Context, _ owneraction.Principal, f owneraction.Facts, key string, _ time.Time) (owneraction.Result, bool, error) {
	if _, err := unpackDiagnosis(f); err != nil {
		return owneraction.Result{}, false, err
	}
	if d.Effects == nil {
		return owneraction.Result{}, false, nil
	}
	result, found, err := d.Effects.RecoverDiagnosis(ctx, f.TargetID, key)
	if err == nil && found {
		result.Summary, err = d.Protect(ctx, result.Summary)
	}
	return result, found, err
}
