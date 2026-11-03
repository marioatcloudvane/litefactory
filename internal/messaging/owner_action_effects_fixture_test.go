package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

func (b OwnerActionDomain) diagnosisDomain() owneractions.Diagnosis {
	s := b.Service
	return owneractions.Diagnosis{Targets: s.factory.Targets, Protect: s.Protect, Effects: diagnosisEffects{diagnosis: s.diagnosis}, Availability: s.actionAvailability()}
}

type diagnosisEffects struct{ diagnosis Diagnosis }

func (e diagnosisEffects) DiagnosisAvailable() bool {
	_, ok := e.diagnosis.(interface {
		OwnerActionDiagnosis(context.Context, string, string, string) (string, error)
	})
	return ok
}

func (e diagnosisEffects) Diagnose(ctx context.Context, task, note, key string) (owneraction.Result, error) {
	diagnosis, ok := e.diagnosis.(interface {
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
	if recovery, ok := e.diagnosis.(interface {
		RecoverOwnerActionDiagnosis(context.Context, string, string) (owneraction.Result, bool, error)
	}); ok {
		return recovery.RecoverOwnerActionDiagnosis(ctx, task, key)
	}
	return owneraction.Result{}, false, nil
}

func (b OwnerActionDomain) standardsDomain() owneractions.Standards {
	s := b.Service
	frozen, _ := s.standards.(owneractions.FrozenStandards)
	return owneractions.Standards{Targets: s.factory.Targets, Available: s.standards != nil, Frozen: frozen, Protect: s.Protect}
}

func (b OwnerActionDomain) actionDomain() owneractions.Actions {
	s := b.Service
	return owneractions.Actions{FallbackTransport: s.transport, Targets: s.factory.Targets, Protect: s.Protect, Research: s.research, Features: featureEffects{drafter: factoryDraft(s.drafter), confirmer: s.confirmer}, Answer: owneractions.AnswerAdmission{Research: s.research, Protect: s.Protect, Render: s.renderQuestion}, Availability: s.actionAvailability()}
}

func factoryDraft(drafter *feature.Drafter) *featuredraft.Service {
	if drafter == nil {
		return nil
	}
	core := drafter.Factory()
	core.Bind = nil
	core.History = nil
	return core
}

type featureEffects struct {
	drafter   *featuredraft.Service
	confirmer FeatureConfirmer
}

func (e featureEffects) DraftAvailable() bool {
	return e.drafter.Available()
}
func (e featureEffects) ConfirmAvailable() bool { return e.confirmer != nil }
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
	result, err := e.drafter.Reply(ctx, transport, channel, key, project, note, at)
	return result.ActionResult(), err
}
func (e featureEffects) Confirm(ctx context.Context, transport, channel, key, code string, at time.Time) (owneraction.Result, error) {
	work, assessment := presentation.WithAssessment(ctx)
	summary, err := e.confirmer.Confirm(work, transport, channel, key, code, at)
	return assessedAction(assessment, summary), err
}
func (e featureEffects) RecoverFeature(ctx context.Context, transport, channel, code string, at time.Time) (string, bool, error) {
	if recovery, ok := e.confirmer.(interface {
		RecoverOwnerActionFeature(context.Context, string, string, string, time.Time) (string, bool, error)
	}); ok {
		return recovery.RecoverOwnerActionFeature(ctx, transport, channel, code, at)
	}
	return "", false, nil
}
