package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// diagnosisDomain supplies parsing, protection and the existing effect host;
// target selection and all frozen-target decisions belong to the factory.
func (b OwnerActionDomain) diagnosisDomain() owneractions.Diagnosis {
	s := b.Service
	return owneractions.Diagnosis{Targets: s.factory.Targets, Protect: s.Protect, Effects: diagnosisEffects{s}, Validate: func(in owneraction.Invocation) (owneraction.Invocation, error) {
		inv, effect, err := s.ValidateInvocation(Invocation{Command: in.Command, Arguments: in.Arguments})
		if err != nil || effect != ChangeEffect {
			return owneraction.Invocation{}, ErrInvalidInvocation
		}
		return owneraction.Invocation{Command: inv.Command, Arguments: inv.Arguments}, nil
	}}
}

type diagnosisEffects struct{ service *Service }

func (e diagnosisEffects) DiagnosisAvailable() bool {
	_, ok := e.service.diagnosis.(interface {
		OwnerActionDiagnosis(context.Context, string, string, string) (string, error)
	})
	return ok
}

func (e diagnosisEffects) Diagnose(ctx context.Context, task, note, key string) (owneraction.Result, error) {
	diagnosis, ok := e.service.diagnosis.(interface {
		OwnerActionDiagnosis(context.Context, string, string, string) (string, error)
	})
	if !ok {
		return owneraction.Result{State: "failed", Summary: "Diagnosis is unavailable. No diagnosis was started; inspect factory diagnosis setup before a fresh proposal."}, nil
	}
	work, assessment := presentation.WithAssessment(ctx)
	summary, err := diagnosis.OwnerActionDiagnosis(work, task, note, key)
	result := owneraction.Result{State: "succeeded", Summary: summary}
	switch assessment.Status {
	case presentation.Uncertain:
		result.State = "uncertain"
	case presentation.Refusal, presentation.ReportableError:
		result.State = "failed"
	}
	return result, err
}
func (e diagnosisEffects) RecoverDiagnosis(ctx context.Context, task, key string) (owneraction.Result, bool, error) {
	if recovery, ok := e.service.diagnosis.(interface {
		RecoverOwnerActionDiagnosis(context.Context, string, string) (owneraction.Result, bool, error)
	}); ok {
		return recovery.RecoverOwnerActionDiagnosis(ctx, task, key)
	}
	return owneraction.Result{}, false, nil
}

func (b OwnerActionDomain) standardsDomain() owneractions.Standards {
	s := b.Service
	frozen, _ := s.standards.(owneractions.FrozenStandards)
	return owneractions.Standards{Targets: s.factory.Targets, Available: s.standards != nil, Frozen: frozen, Protect: s.Protect, Validate: func(in owneraction.Invocation) (owneraction.Invocation, error) {
		inv, effect, err := s.ValidateInvocation(Invocation{Command: in.Command, Arguments: in.Arguments})
		if err != nil || effect != ChangeEffect {
			return owneraction.Invocation{}, ErrInvalidInvocation
		}
		return owneraction.Invocation{Command: inv.Command, Arguments: inv.Arguments}, nil
	}}
}

func (b OwnerActionDomain) actionDomain() owneractions.Actions {
	s := b.Service
	return owneractions.Actions{FallbackTransport: s.transport, Targets: s.factory.Targets, Protect: s.Protect, Research: s.research, Features: featureEffects{s}, Answer: owneractions.AnswerAdmission{Research: s.research, Protect: s.Protect, Render: s.renderQuestion}, Validate: func(in owneraction.Invocation) (owneraction.Invocation, error) {
		inv, effect, err := s.ValidateInvocation(Invocation{Command: in.Command, Arguments: in.Arguments})
		if err != nil || effect != ChangeEffect {
			return owneraction.Invocation{}, ErrInvalidInvocation
		}
		return owneraction.Invocation{Command: inv.Command, Arguments: inv.Arguments}, nil
	}}
}

type featureEffects struct{ service *Service }

func (e featureEffects) DraftAvailable() bool {
	s := e.service
	return s.drafter != nil && s.drafter.Model != nil && s.drafter.Redactor != nil && s.drafter.Store != nil && s.drafter.Repositories != nil
}
func (e featureEffects) ConfirmAvailable() bool { return e.service.confirmer != nil }
func assessedAction(assessment *presentation.Assessment, summary string) owneraction.Result {
	result := owneraction.Result{State: "succeeded", Summary: summary}
	switch assessment.Status {
	case presentation.Uncertain:
		result.State = "uncertain"
	case presentation.Refusal, presentation.ReportableError:
		result.State = "failed"
	}
	return result
}
func (e featureEffects) Draft(ctx context.Context, transport, channel, key, project, note string, at time.Time) (owneraction.Result, error) {
	work, assessment := presentation.WithAssessment(ctx)
	drafter := *e.service.drafter
	drafter.Bind = nil
	drafter.History = nil
	summary, _, err := drafter.Reply(work, transport, channel, key, project, note, at)
	return assessedAction(assessment, summary), err
}
func (e featureEffects) Confirm(ctx context.Context, transport, channel, key, code string, at time.Time) (owneraction.Result, error) {
	work, assessment := presentation.WithAssessment(ctx)
	summary, err := e.service.confirmer.Confirm(work, transport, channel, key, code, at)
	return assessedAction(assessment, summary), err
}
func (e featureEffects) RecoverFeature(ctx context.Context, transport, channel, code string, at time.Time) (string, bool, error) {
	if recovery, ok := e.service.confirmer.(interface {
		RecoverOwnerActionFeature(context.Context, string, string, string, time.Time) (string, bool, error)
	}); ok {
		return recovery.RecoverOwnerActionFeature(ctx, transport, channel, code, at)
	}
	return "", false, nil
}

func actionSelection(p naturalPayload, fallback string) owneractions.Selection {
	return owneractions.Selection{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, PreviewID: p.PreviewID, PreviewHash: p.PreviewHash, Question: p.Question, Revision: p.Revision, RepoID: p.RepoID, Project: p.Project, Channel: p.Channel, Transport: naturalTransport(p, fallback), ProjectPending: p.ProjectPending, Focus: p.focus}
}
