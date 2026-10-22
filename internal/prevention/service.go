// Package prevention coordinates standalone bounded advisory bug analysis.
package prevention

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Source freezes bounded host-controlled evidence.
type Source interface {
	Collect(context.Context, Scope) (Manifest, error)
}

// Runner provides the isolated standalone invocation boundary.
type Runner interface {
	Isolation(context.Context) (string, error)
	Run(context.Context, Invocation, *Session) (agent.DoctorRunResult, error)
}

// Invocation carries immutable host identity and effective work limits.
type Invocation struct {
	Usage                                  *agent.UsageCapture
	ID, AttemptID, Provider, Model, Prompt string
	Limits                                 Limits
}

// Service owns analysis coordination without workflow or repair authority.
type Service struct {
	DB              *store.DB
	Source          Source
	Runner          Runner
	Redactor        *redact.Redactor
	Instructions    instructionpackage.CaptureSource
	Provider, Model string
}

// Inspection exposes retained evidence, independent outcomes and exact proposals.
type Inspection struct {
	Analysis  store.PreventionAnalysis         `json:"analysis"`
	Manifest  Manifest                         `json:"manifest"`
	Result    *Result                          `json:"result,omitempty"`
	Standards standards.RunInspection          `json:"standards"`
	Proposals []store.StandardsProposalVersion `json:"proposals"`
}

// Result is the strict versioned analyst submission.
type Result struct {
	SchemaVersion int       `json:"schema_version"`
	Findings      []Finding `json:"findings"`
}

// Finding separates cause support, uncertainty and prevention proposals per bug.
type Finding struct {
	Issue               int                   `json:"issue"`
	Expected            string                `json:"expected"`
	Actual              string                `json:"actual"`
	Cause               string                `json:"cause"`
	CauseBasis          string                `json:"cause_basis"`
	Uncertainty         string                `json:"uncertainty"`
	PreventionRationale string                `json:"prevention_rationale"`
	BugEvidence         []string              `json:"bug_evidence"`
	CauseEvidence       []string              `json:"cause_evidence"`
	MissingEvidence     []string              `json:"missing_evidence"`
	Outcome             string                `json:"outcome"`
	Proposals           []standards.Operation `json:"proposals"`
}
type retainedResult struct {
	Result         Result     `json:"result"`
	RequestID      string     `json:"request_id"`
	Coverage       []Coverage `json:"coverage"`
	Limits         Limits     `json:"limits"`
	ManifestDigest string     `json:"manifest_digest"`
}

// ProposalReference identifies the exact unapproved proposal without repeating its content.
type ProposalReference struct {
	ProposalID   string `json:"proposal_id"`
	Version      int64  `json:"version"`
	BaseRevision int64  `json:"base_revision"`
	ChangeDigest string `json:"change_digest"`
	ResultDigest string `json:"result_digest"`
}

// Receipt identifies one durable accepted result and its unapproved proposals.
type Receipt struct {
	RequestID    string              `json:"request_id"`
	AnalysisID   string              `json:"analysis_id"`
	ResultDigest string              `json:"result_digest"`
	Proposals    []ProposalReference `json:"proposals"`
}

func marshal(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func hash(v any) string             { b := marshal(v); s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func runtime(db *store.DB, id, attempt string) *standards.Runtime {
	return &standards.Runtime{Store: db, RootRunID: "analysis:" + id, ConsumerKey: "prevention-analyst", AttemptID: attempt, OutputLimit: 8 << 10}
}

// Start persists identity before collection and never reanalyzes a saved request.
func (s *Service) Start(ctx context.Context, scope Scope) (Inspection, error) {
	if s.DB == nil {
		return Inspection{}, errors.New("analysis storage unavailable")
	}
	scope.Repository = strings.ToLower(strings.TrimSpace(scope.Repository))
	scope.Issues = slices.Clone(scope.Issues)
	slices.Sort(scope.Issues)
	if scope.Limits == (Limits{}) {
		scope.Limits = DefaultLimits()
	}
	if err := scope.Validate(); err != nil {
		return Inspection{}, err
	}
	if strings.TrimSpace(scope.RequestKey) == "" || len(scope.RequestKey) > 256 {
		return Inspection{}, errors.New("analysis requires request key of 1..256 bytes")
	}
	if s.Redactor == nil {
		return Inspection{}, errors.New("analysis redaction unavailable")
	}
	safeKey, keyErr := s.Redactor.Apply(ctx, scope.RequestKey, 512)
	if keyErr != nil || safeKey.Truncated() || safeKey.String() != scope.RequestKey {
		return Inspection{}, errors.New("request key refused by redaction policy")
	}
	a, created, err := s.DB.CreatePreventionAnalysis(ctx, store.PreventionAnalysis{ID: rand.Text(), AttemptID: rand.Text(), RequestKey: scope.RequestKey, ScopeDigest: hash(scope), Repository: scope.Repository, Scope: marshal(scope), Limits: marshal(scope.Limits), DeadlineAt: time.Now().UTC().Add(time.Duration(scope.Limits.DurationSeconds) * time.Second)})
	if err != nil {
		return Inspection{}, err
	}
	if !created {
		if _, err = s.DB.RecoverPreventionAnalysis(ctx, a.ID); err != nil {
			return Inspection{Analysis: a}, err
		}
		return s.Inspect(ctx, a.ID)
	}
	ctx, cancel := context.WithDeadline(ctx, a.DeadlineAt)
	defer cancel()
	finish := func(status string, cause error) (Inspection, error) {
		cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer done()
		reason := "analysis failed; sanitized diagnostic unavailable"
		if s.Redactor != nil {
			if b, e := s.Redactor.Apply(cleanup, cause.Error(), 4096); e == nil {
				reason = b.String()
			}
		}
		update := s.DB.UpdatePreventionAnalysis(cleanup, a.ID, a.AttemptID, status, marshal(map[string]any{"reason": reason}))
		out, read := s.Inspect(cleanup, a.ID)
		return out, &analysisFailure{cause: errors.Join(cause, update, read), message: fmt.Sprintf("analysis %s: %s", a.ID, reason)}
	}
	r := runtime(s.DB, a.ID, a.AttemptID)
	if _, err = r.Bind(store.WithPreventionWriter(ctx, a.ID, a.AttemptID), true); err != nil {
		return finish("failed", err)
	}
	instructions := s.Instructions
	if instructions == nil {
		instructions, err = instructionpackage.LoadShipped(ctx, "")
		if err != nil {
			return finish("failed", err)
		}
	}
	capture, _, err := instructions.Capture(ctx, "prevention-analyst")
	if err != nil {
		return finish("failed", err)
	}
	if s.Redactor == nil {
		return finish("failed", errors.New("analysis redaction unavailable"))
	}
	instructionText := string(capture.Instructions())
	safeInstructions, redactionErr := s.Redactor.Apply(ctx, instructionText, len(instructionText)*2+4096)
	if redactionErr != nil || safeInstructions.Truncated() || safeInstructions.String() != instructionText {
		return finish("failed", errors.New("analysis instructions withheld by redaction policy"))
	}
	provenance := map[string]any{"instructions": capture.Snapshot(), "provider": s.Provider, "model": s.Model, "tool_profile": "not launched", "source_policy": collectionPolicy()}
	if err = s.retainProvenance(ctx, a.ID, a.AttemptID, provenance); err != nil {
		return finish("failed", err)
	}
	if s.Source == nil {
		return finish("failed", errors.New("evidence source unavailable"))
	}
	collectionStarted := time.Now().UTC()
	manifest, err := s.Source.Collect(ctx, scope)
	manifest.Frozen = true
	if manifest.StartedAt.IsZero() {
		manifest.StartedAt = collectionStarted
	}
	if manifest.FinishedAt.IsZero() {
		manifest.FinishedAt = time.Now().UTC()
	}
	// Own a deep copy; source adapters cannot change the frozen consumer view.
	frozenBytes := marshal(manifest)
	var ownedManifest Manifest
	if cloneErr := json.Unmarshal(frozenBytes, &ownedManifest); cloneErr != nil {
		return finish("failed", cloneErr)
	}
	manifest = ownedManifest
	// Preserve whatever collection completed even when the caller cancelled.
	frozen, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	freezeErr := s.DB.FreezePreventionAnalysis(frozen, a.ID, a.AttemptID, marshal(manifest))
	done()
	if freezeErr != nil {
		return finish("failed", freezeErr)
	}
	if err != nil || ctx.Err() != nil {
		status := "failed"
		if ctx.Err() != nil {
			status = "interrupted"
		}
		return finish(status, errors.Join(err, ctx.Err()))
	}
	if s.Runner == nil {
		return finish("provider_unavailable", errors.New("configured provider cannot isolate prevention analysis"))
	}
	isolation, err := s.Runner.Isolation(ctx)
	if err != nil || isolation == "" {
		return finish("provider_unavailable", errors.New("configured provider cannot isolate prevention analysis"))
	}
	modelScope := scope
	modelScope.RequestKey = ""
	prompt := instructionText + "\nFactory scope (data):\n" + string(marshal(modelScope)) + "\nRead evidence_read with id=manifest and offset=0; follow next_offset until complete for the frozen evidence index, host coverage and counters. Then read each cited evidence ID in full.\n"

	provenance["tool_profile"] = isolation
	if err = s.retainProvenance(ctx, a.ID, a.AttemptID, provenance); err != nil {
		return finish("failed", err)
	}
	if err = s.DB.UpdatePreventionAnalysis(ctx, a.ID, a.AttemptID, "analyzing", marshal(map[string]string{})); err != nil {
		return finish("failed", err)
	}
	session := newSession(s, a, scope, manifest, r)
	var usageMu sync.Mutex
	usageEvents := []agent.UsageEvent{}
	usage := &agent.UsageCapture{AttemptID: a.AttemptID, Sink: func(eventCtx context.Context, event agent.UsageEvent) error {
		usageMu.Lock()
		defer usageMu.Unlock()
		candidate := append(slices.Clone(usageEvents), event)
		if len(marshal(candidate)) > 16<<10 {
			provenance["usage_limited"] = true
		} else {
			usageEvents = candidate
		}
		provenance["usage_events"] = usageEvents
		return s.retainProvenance(eventCtx, a.ID, a.AttemptID, provenance)
	}}
	native, runErr := s.Runner.Run(ctx, Invocation{ID: a.ID, AttemptID: a.AttemptID, Provider: s.Provider, Model: s.Model, Prompt: prompt, Limits: scope.Limits, Usage: usage}, session)
	session.close()
	// A durable accepted result takes precedence over a lost provider reply.
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer done()
	saved, readErr := s.DB.GetPreventionAnalysis(cleanup, a.ID)
	if readErr != nil {
		return Inspection{Analysis: a}, readErr
	}
	usageMu.Lock()
	defer usageMu.Unlock()
	provenance["output_bytes"] = session.used
	provenance["output_limited"] = session.degraded
	provenance["observed_provider"] = native
	if e := s.retainProvenance(cleanup, a.ID, a.AttemptID, provenance); e != nil {
		return Inspection{Analysis: saved}, e
	}
	if len(saved.Result) > 0 {
		return s.Inspect(cleanup, a.ID)
	}
	provenance["observed_provider"] = native
	if runErr == nil {
		runErr = errors.New("provider exited without accepted typed result")
	}
	status := "failed"
	if ctx.Err() != nil {
		status = "interrupted"
	}
	return finish(status, runErr)
}
func evidenceIndex(m Manifest) []Evidence {
	out := slices.Clone(m.Evidence)
	for i := range out {
		out[i].Excerpt = ""
	}
	return out
}

// Inspect reads retained analysis without launching or refreshing evidence.
func (s *Service) Inspect(ctx context.Context, id string) (Inspection, error) {
	a, err := s.DB.RecoverPreventionAnalysis(ctx, id)
	out := Inspection{Analysis: a, Proposals: a.Proposals}
	if err != nil {
		return out, err
	}
	if len(a.Manifest) > 0 {
		if err = json.Unmarshal(a.Manifest, &out.Manifest); err != nil {
			return out, err
		}
	}
	if len(a.Result) > 0 {
		var retained retainedResult
		if err = json.Unmarshal(a.Result, &retained); err != nil {
			return out, err
		}
		out.Result = &retained.Result
	}
	out.Standards, err = runtime(s.DB, id, a.AttemptID).Inspect(ctx)
	return out, err
}

// List returns at most the requested number of concise retained summaries.
func (s *Service) List(ctx context.Context, repository string, limit int) ([]store.PreventionAnalysis, error) {
	all, err := s.DB.ListPreventionAnalysesForRepository(ctx, strings.ToLower(strings.TrimSpace(repository)), limit)
	if err != nil {
		return nil, err
	}
	out := []store.PreventionAnalysis{}
	for _, a := range all {
		a.Manifest = nil
		a.Result = nil
		a.Draft = nil
		a.Proposals = nil
		a.Diagnostics = nil
		a.Provenance = nil
		a.DeliveredEvidence = nil
		out = append(out, a)
	}
	return out, nil
}

func (s *Service) retainProvenance(ctx context.Context, id, attempt string, value any) error {
	raw := marshal(value)
	safe, err := s.Redactor.Apply(ctx, string(raw), len(raw)*2+4096)
	if err != nil || safe.Truncated() || safe.String() != string(raw) {
		return errors.New("provider or instruction provenance withheld by redaction policy")
	}
	return s.DB.SetPreventionProvenance(ctx, id, attempt, raw)
}

// analysisFailure preserves error classification without echoing unsafe cause text.
type analysisFailure struct {
	cause   error
	message string
}

func (e *analysisFailure) Error() string { return e.message }
func (e *analysisFailure) Unwrap() error { return e.cause }
