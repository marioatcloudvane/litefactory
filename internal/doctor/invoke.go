package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

func (s *Service) invoke(ctx context.Context, r DiagnoseRequest, c store.DoctorConversation, id, provider, model string, manifest PathManifest, instruction string, captured *instructionpackage.RoleCapture, failure func(error) (Conversation, error)) (_ Conversation, err error) {
	memory, e := s.DB.DoctorAttempts(ctx, c.ID)
	if e != nil {
		return failure(e)
	}
	// Exclude the current input from memory and omit stale evidence snapshots;
	// fresh evidence is always supplied separately, with omissions explicit.
	prior := make([]store.DoctorAttempt, 0, len(memory))
	for _, a := range memory {
		if a.ID == id {
			continue
		}
		a.Evidence = ""
		prior = append(prior, a)
	}
	current, e := s.currentDiagnosis(ctx, c.ID)
	if e != nil {
		return failure(e)
	}
	proposal, e := s.currentProposal(ctx, c.ID)
	if e != nil {
		return failure(e)
	}
	if e = os.MkdirAll(s.Paths.DoctorScratchDir(), 0700); e != nil {
		return failure(e)
	}
	dir, e := os.MkdirTemp(s.Paths.DoctorScratchDir(), "invocation-")
	if e != nil {
		return failure(e)
	}
	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()
	handoff, e := s.PrepareRetainedEvidence(ctx, r.Target, id, dir)
	if e != nil {
		return failure(e)
	}
	prompt, e := BuildPrompt(PromptContext{Instructions: instruction, Target: r.Target.String(), Manifest: manifest, Memory: prior, CurrentDiagnosis: current, CurrentProposal: proposal, RetainedEvidenceIndex: handoff.Index, MemoryOmissions: "Latest 30 attempts only; exact retained evidence is linked through the bounded evidence index; current findings and proposal retained separately"}, captured)
	if e != nil {
		return failure(e)
	}
	safe, e := s.Redactor.Apply(ctx, prompt, 524288)
	if e != nil {
		return failure(e)
	}
	if safe.Truncated() {
		return failure(errors.New("doctor prompt exceeds redaction budget"))
	}
	results, e := BindResults(ctx, s.DB, id, manifest.evidenceIdentity(), s.Redactor)
	if e != nil {
		return failure(e)
	}
	results.comments = s.Comments
	runCtx, runCancel := context.WithCancel(ctx)
	s.mu.Lock()
	if s.active == nil {
		s.active = make(map[string]activeInvocation)
	}
	s.active[r.Target.String()] = activeInvocation{cancel: runCancel, generation: c.Generation}
	s.mu.Unlock()
	defer func() { runCancel(); s.mu.Lock(); delete(s.active, r.Target.String()); s.mu.Unlock() }()
	// Stop may have fenced the attempt just before the cancellation handle was
	// installed. Recheck authority after installing it, before dispatch.
	if e = s.DB.CheckResultWriter(ctx, results.binding); e != nil {
		return failure(e)
	}
	usage, e := s.DB.StartDoctorMeterAttempt(ctx, id, provider)
	if e != nil {
		return failure(e)
	}
	var sinkFailed atomic.Bool
	capture := &agent.UsageCapture{AttemptID: usage.ID, Sink: func(c context.Context, event agent.UsageEvent) error {
		err := s.DB.RecordMeterEvent(c, usage.ID, provider, event)
		if err != nil {
			sinkFailed.Store(true)
		}
		return err
	}}
	outcome, runErr := s.Runner.Run(runCtx, Invocation{ID: id, Provider: provider, ConfiguredModel: model, Prompt: safe.String(), WorkingDirectory: dir, MeterAttempt: usage, Usage: capture}, results)
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	meterOutcome := "success"
	if runErr != nil {
		meterOutcome = "failure"
	}
	if ctx.Err() != nil {
		meterOutcome = "cancelled"
	}
	coverage := agent.UsageCoverage{Status: agent.CollectionUnknown, Version: outcome.ProviderVersion, Diagnostics: []string{"doctor_provider_usage_not_observed"}}
	if outcome.Usage != nil {
		coverage = outcome.Usage.Clone()
	}
	if sinkFailed.Load() {
		coverage.Status = agent.CollectionPartial
		coverage.Diagnostics = append(coverage.Diagnostics, "persistence_failure")
	}
	meterErr := s.DB.FinishMeterAttempt(cleanup, usage.ID, meterOutcome, string(coverage.Status), coverage.Version, coverage.SourceFormat, coverage.Diagnostics)
	if runErr != nil || meterErr != nil {
		return failure(errors.Join(runErr, meterErr))
	}
	if outcome.ObservedModel != "" {
		body, e := s.Redactor.Apply(ctx, outcome.ObservedModel, 8192)
		if e != nil {
			return failure(e)
		}
		if e = s.DB.RecordDoctorObservedModel(ctx, id, body.String()); e != nil {
			return failure(e)
		}
	}
	if outcome.RequestID == "" {
		return failure(errors.New("doctor provider ended without a durable result receipt"))
	}
	if e = results.Adopt(ctx, outcome.RequestID); e != nil {
		return failure(e)
	}
	return s.ReadConversation(ctx, r.Target)
}

func (s *Service) currentDiagnosis(ctx context.Context, cid int64) (*Diagnosis, error) {
	data, err := s.DB.DoctorCurrentResult(ctx, cid)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	// Retained content must pass redaction again before disclosure.
	d, err := DecodeDiagnosis(data)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	body, err := s.Redactor.Apply(ctx, string(encoded), 131072)
	if err != nil {
		return nil, err
	}
	if body.Truncated() {
		return nil, errors.New("saved doctor result exceeds display budget")
	}
	d, err = DecodeDiagnosis([]byte(body.String()))
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Service) currentProposal(ctx context.Context, cid int64) ([]RepairStep, error) {
	data, err := s.DB.DoctorCurrentProposal(ctx, cid)
	if err != nil || len(data) == 0 {
		return nil, err
	}
	body, err := s.Redactor.Apply(ctx, string(data), 131072)
	if err != nil {
		return nil, err
	}
	if body.Truncated() {
		return nil, errors.New("doctor proposal exceeds display budget")
	}
	var p []RepairStep
	if err := json.Unmarshal([]byte(body.String()), &p); err != nil {
		return nil, err
	}
	return p, nil
}
