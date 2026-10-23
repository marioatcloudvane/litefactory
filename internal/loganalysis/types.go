// Package loganalysis coordinates bounded, standalone execution-log analysis.
package loganalysis

import (
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
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
	// DryRun records what would be filed and files nothing.
	DryRun bool `json:"dry_run,omitempty"`
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

// Scope retains the operator selection, the frozen reader boundary and what
// the analyst may check and file against, pinned at start.
type Scope struct {
	Request      Request      `json:"request"`
	Cutoff       time.Time    `json:"cutoff"`
	HighWater    int64        `json:"high_water"`
	Repositories []Repository `json:"repositories,omitempty"`
	// Upstream is litefactory's own issue tracker, "" when none is configured.
	Upstream string `json:"upstream,omitempty"`
}

// Repository is one analysed repository as the analyst sees it.
type Repository struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	BaseBranch string `json:"base_branch"`
	// Commit is the base branch head the repository tools read; Gap says why
	// there is none.
	Commit string `json:"commit,omitempty"`
	Gap    string `json:"gap,omitempty"`
	// Team is the linked team; TeamOwned marks a user-owned (cloned, editable)
	// team whose role prompts belong to the project owner.
	Team      string `json:"team,omitempty"`
	TeamOwned bool   `json:"team_owned"`
	// Tickets names the project's ticket source, "" when it is unavailable.
	Tickets string `json:"tickets,omitempty"`
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

// Kinds of findings, routed by who can fix them.
const (
	// FindingProjectSetup is the analysed project's own setup: its tests, make
	// targets, environment or repository configuration. Its owner fixes it.
	FindingProjectSetup = "project_setup"
	// FindingFactoryDefect is litefactory itself. Its maintainer fixes it.
	FindingFactoryDefect = "factory_defect"
	// FindingRolePrompt is a role's own instructions. A catalogue team's role is
	// the maintainer's; a user-owned team's role is the project owner's.
	FindingRolePrompt = "role_prompt"
)

// maxFiled is the hard cap on findings one analysis may file.
const maxFiled = 2

// Finding is a cause the analyst places outside the book.
type Finding struct {
	Key         string                 `json:"key"`
	Kind        string                 `json:"kind"`
	Repository  string                 `json:"repository,omitempty"`
	Target      string                 `json:"target"`
	Title       string                 `json:"title"`
	Expected    string                 `json:"expected"`
	Actual      string                 `json:"actual"`
	Cause       string                 `json:"cause"`
	Suggestion  string                 `json:"suggestion"`
	IncidentIDs []string               `json:"incident_ids"`
	Citations   []logevidence.Citation `json:"citations"`
	Existing    string                 `json:"existing,omitempty"`
	File        bool                   `json:"file"`
}

// Report is the analyst's judgement; what it read is recorded by the host.
type Report struct {
	Summary        string     `json:"summary"`
	Proposals      []Proposal `json:"proposals"`
	Findings       []Finding  `json:"findings"`
	NoChangeReason string     `json:"no_change_reason,omitempty"`
}

// Reading is what the analyst actually received, from the evidence ledger.
type Reading struct {
	IncidentsOffered int     `json:"incidents_offered"`
	RunsListed       int     `json:"runs_listed"`
	Digests          []int64 `json:"digests"`
	RawReads         []int64 `json:"raw_reads"`
	Withheld         int     `json:"withheld"`
	IssueSearches    int     `json:"issue_searches"`
	RepositoryReads  int     `json:"repository_reads"`
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
	// IncidentTasks names each offered incident's task, for filed issue text.
	IncidentTasks map[string]string      `json:"incident_tasks,omitempty"`
	Citations     []logevidence.Citation `json:"citations,omitempty"`
	Coverage      logevidence.Coverage   `json:"coverage"`
	Next          bool                   `json:"has_more"`
	Budget        logevidence.Usage      `json:"budget"`
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
	Filings      []store.LogAnalysisFiling       `json:"filings"`
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
	// Git reads the analysed repositories at their pinned base commits; nil
	// leaves the repository tools unavailable.
	Git *git.Binary
	// Projects are the analysed repositories' ticket sources by repository ID;
	// Upstream is litefactory's own, named UpstreamName. Missing ones are
	// unavailable to search and never filed into.
	Projects     map[int64]Tracker
	Upstream     Tracker
	UpstreamName string
	// Started displays the durable identity before any provider work.
	Started func(string) error
}
