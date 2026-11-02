package localstandards

import (
	"github.com/cloudvane/litefactory/internal/standardsexceptions"
	"time"
)

// ExceptionApproval is retained exception evidence for local review.
type ExceptionApproval struct {
	ProposalID string    `json:"proposal_id"`
	Digest     string    `json:"digest"`
	Owner      string    `json:"owner"`
	Provenance string    `json:"provenance"`
	CreatedAt  time.Time `json:"created_at"`
}

// ExceptionRevocation is retained exception evidence for local review.
type ExceptionRevocation struct {
	ProposalID string    `json:"proposal_id"`
	Owner      string    `json:"owner"`
	Provenance string    `json:"provenance"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}

// ExceptionPolicy is retained exception evidence for local review.
type ExceptionPolicy struct {
	Version                   int      `json:"version"`
	Mode                      string   `json:"mode"`
	AllRegisteredRepositories bool     `json:"all_registered_repositories,omitempty"`
	RepositoryIDs             []int64  `json:"repository_ids,omitempty"`
	TaskRefs                  []string `json:"task_refs,omitempty"`
	MaxLifetimeSeconds        int64    `json:"max_lifetime_seconds,omitempty"`
}

// ExceptionStandard is retained exception evidence for local review.
type ExceptionStandard struct {
	ID              string           `json:"id"`
	CategoryID      string           `json:"category_id"`
	Rule            string           `json:"rule"`
	Description     string           `json:"description"`
	Evidence        string           `json:"evidence"`
	Scope           string           `json:"scope"`
	Check           string           `json:"check"`
	ExceptionPolicy *ExceptionPolicy `json:"exception_policy,omitempty"`
	AppliesTo       []string         `json:"applies_to"`
}

// ExceptionView is retained exception evidence for local review.
type ExceptionView struct {
	Proposal    standardsexceptions.Proposal `json:"proposal"`
	Approval    *ExceptionApproval           `json:"approval,omitempty"`
	Revocation  *ExceptionRevocation         `json:"revocation,omitempty"`
	Effective   bool                         `json:"effective"`
	Expired     bool                         `json:"expired"`
	Status      string                       `json:"status"`
	EvaluatedAt time.Time                    `json:"evaluated_at"`
	Error       string                       `json:"error,omitempty"`
	Standard    *ExceptionStandard           `json:"standard,omitempty"`
}

func exceptionView(source standardsexceptions.View) ExceptionView {
	v := ExceptionView{Proposal: source.Proposal, Effective: source.Effective, Expired: source.Expired, Status: source.Status, EvaluatedAt: source.EvaluatedAt, Error: source.Error}
	if source.Approval != nil {
		approval := ExceptionApproval(*source.Approval)
		v.Approval = &approval
	}
	if source.Revocation != nil {
		revocation := ExceptionRevocation(*source.Revocation)
		v.Revocation = &revocation
	}
	if source.Standard != nil {
		standard := source.Standard
		v.Standard = &ExceptionStandard{ID: standard.ID, CategoryID: standard.CategoryID, Rule: standard.Rule, Description: standard.Description, Evidence: standard.Evidence, Scope: standard.Scope, Check: standard.Check, AppliesTo: standard.AppliesTo}
		if standard.ExceptionPolicy != nil {
			policy := ExceptionPolicy(*standard.ExceptionPolicy)
			v.Standard.ExceptionPolicy = &policy
		}
	}
	return v
}
