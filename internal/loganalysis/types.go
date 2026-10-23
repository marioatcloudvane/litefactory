// Package loganalysis coordinates bounded, standalone execution-log analysis.
package loganalysis

import (
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Request contains explicit local-owner selections and lower-only work limits.
type Request struct {
	RepositoryIDs []int64   `json:"repository_ids"`
	RunIDs        []int64   `json:"run_ids,omitempty"`
	TaskRefs      []string  `json:"task_refs,omitempty"`
	After         time.Time `json:"after,omitempty"`
	Before        time.Time `json:"before,omitempty"`
	StartKey      string    `json:"start_key,omitempty"`
	Limits        Limits    `json:"limits"`
}

// Limits never exceed the settled invocation ceilings.
type Limits struct {
	ReportBytes int                `json:"report_bytes"`
	Proposals   int                `json:"proposals"`
	Evidence    logevidence.Limits `json:"evidence"`
	Calls       int                `json:"calls"`
	Bytes       int                `json:"bytes"`
	Seconds     int                `json:"seconds"`
}

// Scope retains both the operator selection and the frozen reader boundary.
type Scope struct {
	Request   Request   `json:"request"`
	Cutoff    time.Time `json:"cutoff"`
	HighWater int64     `json:"high_water"`
}

// Justification is the analyst's own case for a proposal.
type Justification struct {
	IncidentIDs   []string `json:"incident_ids"`
	Tasks         []string `json:"tasks"`
	Cost          string   `json:"cost"`
	WhyItPrevents string   `json:"why_it_prevents"`
	LoadAfter     string   `json:"load_after"`
}

// Proposal is one proposed standard, optionally with the category it opens.
type Proposal struct {
	NewCategory   *standards.Category    `json:"new_category,omitempty"`
	Operation     standards.Operation    `json:"operation"`
	Justification Justification          `json:"justification"`
	Citations     []logevidence.Citation `json:"citations"`
}

// Kinds of findings that belong outside the book.
const (
	NotBookRolePrompt    = "role_prompt"
	NotBookFactoryDefect = "factory_defect"
)

// NotBook is a cause the analyst places in a role prompt or the factory.
type NotBook struct {
	Kind        string                 `json:"kind"`
	Target      string                 `json:"target"`
	Finding     string                 `json:"finding"`
	IncidentIDs []string               `json:"incident_ids"`
	Citations   []logevidence.Citation `json:"citations"`
}

// Report is the analyst's judgement; what it read is recorded by the host.
type Report struct {
	Summary        string     `json:"summary"`
	Proposals      []Proposal `json:"proposals"`
	NotBook        []NotBook  `json:"not_book"`
	NoChangeReason string     `json:"no_change_reason,omitempty"`
}

// Reading is what the analyst actually received, from the evidence ledger.
type Reading struct {
	IncidentsOffered int     `json:"incidents_offered"`
	RunsListed       int     `json:"runs_listed"`
	Digests          []int64 `json:"digests"`
	RawReads         []int64 `json:"raw_reads"`
	Withheld         int     `json:"withheld"`
	BudgetExhausted  bool    `json:"budget_exhausted"`
}

// OfferedEvent retains delivery metadata without retaining source bodies.
type OfferedEvent struct {
	Citation   logevidence.Citation `json:"citation"`
	Redactions int                  `json:"redactions"`
	Truncated  bool                 `json:"truncated"`
}

// Evidence strips source bodies before retaining offered ranges and host coverage.
type Evidence struct {
	Events    []OfferedEvent            `json:"events,omitempty"`
	Tool      string                    `json:"tool"`
	Selector  json.RawMessage           `json:"selector"`
	Status    string                    `json:"status"`
	Runs      []logevidence.RunMetadata `json:"runs,omitempty"`
	Incidents []string                  `json:"incidents,omitempty"`
	Citations []logevidence.Citation    `json:"citations,omitempty"`
	Coverage  logevidence.Coverage      `json:"coverage"`
	Next      bool                      `json:"has_more"`
	Budget    logevidence.Usage         `json:"budget"`
}

// Inspection works without any provider or original source logs.
type Inspection struct {
	Invocation   store.LogAnalysis               `json:"invocation"`
	Standards    standards.RunInspection         `json:"standards"`
	Read         Reading                         `json:"read"`
	Evidence     []store.LogAnalysisEvidence     `json:"evidence"`
	EvidenceNext int64                           `json:"evidence_next,omitempty"`
	Report       json.RawMessage                 `json:"report,omitempty"`
	Proposals    []store.LogAnalysisProposalLink `json:"proposals"`
}

// Service has no ambient workflow identity or SQL access.
type Service struct {
	DB         *store.DB
	Redactor   *redact.Redactor
	RunsRoot   string
	Executable string
	Runner     agent.LogAnalystRunner
	Model      string
	DisableMCP bool
	// Started displays the durable identity before any provider work.
	Started func(string) error
}
