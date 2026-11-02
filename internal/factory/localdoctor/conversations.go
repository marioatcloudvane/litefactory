package localdoctor

import (
	"context"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/naming"
)

// ConversationHeader is the task identity and diagnostic lifecycle shown locally.
type ConversationHeader struct {
	ID, Generation int64
	TaskRef, State string
}

// AttemptView contains the diagnosis attempt outcome shown locally.
type AttemptView struct{ State, Failure string }

// RepairView contains the retained repair identity and outcome shown locally.
type RepairView struct{ ID, State, Failure string }

// RepairStepView contains the retained effect and backup outcome shown locally.
type RepairStepView struct {
	Number                 int
	State, BackupReference string
}

// RestartView contains the retained restart outcome shown locally.
type RestartView struct{ State, Report string }

// FindingView is one diagnostic explanation and its evidence labels.
type FindingView struct {
	Explanation string
	Evidence    []string
	Uncertainty string
}

// DiagnosisView contains only the findings and questions rendered locally.
type DiagnosisView struct {
	Findings                []FindingView
	CoverageGaps, Questions []string
}

// ActionView contains the exact proposed operation shown for owner review.
type ActionView struct{ Kind, Before, After string }

// ProposalStepView contains the proposal terms shown for owner review.
type ProposalStepView struct {
	Number                                                        int
	Target, Precondition, Change, Reason, Reversibility, Rollback string
	Action                                                        *ActionView
}

// Conversation is the local diagnostic presentation. The original domain
// snapshot stays private so presentation acknowledgment records exactly the
// displayed proposal without exposing persistence records to the handler.
type Conversation struct {
	Conversation           ConversationHeader
	AlertsText, ReviewText string
	Attempts               []AttemptView
	Diagnosis              *DiagnosisView
	CurrentProposal        []ProposalStepView
	Repair                 *RepairView
	RepairSteps            []RepairStepView
	Restart                *RestartView
	ApplyAvailable         bool
	RepairGuidance         string
	displayed              doctor.Conversation
}

// InspectConversation projects a trusted diagnostic result for local rendering.
func InspectConversation(source doctor.Conversation) Conversation {
	view := Conversation{Conversation: ConversationHeader{ID: source.Conversation.ID, Generation: source.Conversation.Generation, TaskRef: source.Conversation.TaskRef, State: source.Conversation.State}, AlertsText: doctor.RenderAlerts(source.Alerts), ApplyAvailable: source.ApplyAvailable, displayed: source}
	if source.Diagnosis != nil {
		view.Diagnosis = &DiagnosisView{CoverageGaps: source.Diagnosis.CoverageGaps, Questions: source.Diagnosis.Questions}
		for _, finding := range source.Diagnosis.Findings {
			view.Diagnosis.Findings = append(view.Diagnosis.Findings, FindingView{Explanation: finding.Explanation, Evidence: finding.Evidence, Uncertainty: finding.Uncertainty})
		}
	}
	for _, step := range source.CurrentProposal {
		proposal := ProposalStepView{Number: step.Number, Target: step.Target, Precondition: step.Precondition, Change: step.Change, Reason: step.Reason, Reversibility: step.Reversibility, Rollback: step.Rollback}
		if step.Action != nil {
			proposal.Action = &ActionView{Kind: step.Action.Kind, Before: step.Action.Before, After: step.Action.After}
		}
		view.CurrentProposal = append(view.CurrentProposal, proposal)
	}
	if source.Review != nil {
		view.ReviewText = doctor.RenderRejectedDiagnosisReview(*source.Review)
	}
	for _, attempt := range source.Attempts {
		view.Attempts = append(view.Attempts, AttemptView{State: attempt.State, Failure: attempt.Failure})
	}
	if source.Repair != nil {
		view.Repair = &RepairView{ID: source.Repair.ID, State: source.Repair.State, Failure: source.Repair.Failure}
		view.RepairGuidance = doctor.RepairGuidance(source)
	}
	for _, step := range source.RepairSteps {
		view.RepairSteps = append(view.RepairSteps, RepairStepView{Number: step.Number, State: step.State, BackupReference: step.BackupReference})
	}
	if source.Restart != nil {
		view.Restart = &RestartView{State: source.Restart.State, Report: source.Restart.Report}
	}
	return view
}

// RequireRepairInspection retains the domain's post-error presentation context.
func RequireRepairInspection(view Conversation, required bool) Conversation {
	source := view.displayed
	source.RepairInspectionRequired = required
	return InspectConversation(source)
}

// ReadConversation loads the saved diagnostic presentation.
func ReadConversation(ctx context.Context, service *doctor.Service, ref naming.Ref) (Conversation, error) {
	result, err := service.ReadConversation(ctx, ref)
	return InspectConversation(result), err
}

// Diagnose retains the existing diagnosis operation and failure presentation.
func Diagnose(ctx context.Context, service *doctor.Service, request doctor.DiagnoseRequest) (Conversation, error) {
	result, err := service.Diagnose(ctx, request)
	return InspectConversation(result), err
}

// RecordPresentation acknowledges only the exact successfully displayed snapshot.
func RecordPresentation(ctx context.Context, service *doctor.Service, ref naming.Ref, view Conversation) (string, error) {
	return service.RecordPresentation(ctx, ref, view.displayed)
}

// StopView contains only the selected workflow and local stop presentation.
type StopView struct {
	RunID        int64
	Conversation bool
	Summary      string
}

// StopIssue performs the existing stop operation and returns its factual result.
func StopIssue(ctx context.Context, service *doctor.Service, ref naming.Ref) (StopView, error) {
	result, err := service.StopIssue(ctx, ref)
	return StopView{RunID: result.RunID, Conversation: result.Conversation, Summary: doctor.StopSummary(result)}, err
}
