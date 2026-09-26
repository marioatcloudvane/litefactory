// Package agentresult defines the pure, versioned work-result contract. It has
// no persistence or workflow authority; callers supply trusted scope separately.
package agentresult

import (
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/planbrief"
)

// SchemaVersion identifies the application format independently of workflow.
const SchemaVersion = 1

// MaxBytes bounds serialized authored input before decoding.
const MaxBytes = agent.MaxContributionBytes

// MaxDepth bounds nested JSON containers, including the root.
const MaxDepth = 32

// Diagnostic describes a safe, actionable validation fault.
type Diagnostic = agent.ContributionDiagnostic

// Diagnostics distinguishes rejection from incomplete inspection.
type Diagnostics = agent.ContributionDiagnostics

// CreateInput contains only agent-authored content.
type CreateInput struct {
	Result Result `json:"result"`
}

// Metadata contains factory-owned attribution, never credentials.
type Metadata struct {
	Workflow agent.Workflow `json:"workflow,omitempty"`

	SchemaVersion             int    `json:"schema_version"`
	Task                      string `json:"task"`
	RunID                     int64  `json:"run_id"`
	AttemptID                 string `json:"attempt_id"`
	Mode                      string `json:"mode"`
	ResultID                  string `json:"result_id"`
	ResultRevision            int64  `json:"result_revision"`
	ScopeRef                  string `json:"scope_ref,omitempty"`
	RequestID                 string `json:"request_id,omitempty"`
	WorkflowVersion           int    `json:"workflow_version,omitempty"`
	SubmissionContractVersion int    `json:"submission_contract_version,omitempty"`
}

// Snapshot combines trusted attribution and an authored result.
type Snapshot struct {
	Meta   Metadata `json:"meta"`
	Result Result   `json:"result"`
}

// Context is factory input, never decoded from authored JSON. AggregateValidator
// resolves retained aggregate facts; absence cannot establish aggregate proof.
type Context struct {
	Bug *BugBinding

	Workflow            agent.Workflow
	SchemaVersion       int
	Mode                string
	WorkflowVersion     int
	QuestionsAllowed    bool
	PlanScope           planbrief.ContentScope
	Stories             map[string]agent.ContributionStory
	EngineeringComplete map[string]bool
	RequireAggregate    bool
	AggregateValidator  func(agent.EngineeringContribution) Diagnostics
	// PlanGraph is non-nil only for a graph Plan node. It carries the values the
	// factory binds to the request and marks the run as graph Plan, which is what
	// selects the story request and story set branches over the legacy planning
	// fields. Absent, the mode-plan contract is the legacy one, unchanged.
	PlanGraph *PlanGraphBinding
}

// Result contains common content and exactly one mode payload.
type Result struct {
	Outcome        string          `json:"outcome"`
	Comment        string          `json:"comment"`
	Reason         string          `json:"reason,omitempty"`
	Questions      []Question      `json:"questions,omitempty"`
	Recommendation *Recommendation `json:"recommendation,omitempty"`
	Artifacts      []Artifact      `json:"artifacts,omitempty"`
	Payload        Payload         `json:"payload"`
}

// Question requests permitted owner input without recording an answer. Audience
// names the role that can settle it, so one report can carry questions for
// several people and each of them reads only their own.
type Question struct {
	ID                string   `json:"id"`
	Audience          string   `json:"audience"`
	Text              string   `json:"text"`
	Options           []Option `json:"options,omitempty"`
	RecommendedOption string   `json:"recommended_option,omitempty"`
}

// QuestionAudiences are the roles a question may be directed at. The set is
// deliberately small; widening it is a schema change, not an agent's choice.
var QuestionAudiences = []string{"business", "technical"}

// Option is a stable choice within one question.
type Option struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Recommendation is advisory and cannot select a workflow mode.
type Recommendation struct {
	Text   string `json:"text"`
	Action string `json:"action,omitempty"`
}

// ResearchArtifactKinds are the kinds a Research result may declare. Closing the
// set is what lets a later stage select the document it needs instead of
// guessing from a file name: "brief" is the owner's document, "technical" is the
// evidence a claim rests on, and "data" is structured reference material a later
// stage consumes verbatim — a data model, a schema, an interface table — rather
// than reads as argument. Other modes keep an open vocabulary; only Research
// renders its own documents.
var ResearchArtifactKinds = []string{"brief", "technical", "data"}

// Artifact references a rooted artifact without proving retained bytes.
type Artifact struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Description string `json:"description,omitempty"`
}

// Payload selects one of the five factory-bound work modes.
type Payload struct {
	Bug *Bug `json:"bug,omitempty"`

	Research  *Research  `json:"research,omitempty"`
	Plan      *Plan      `json:"plan,omitempty"`
	Implement *Implement `json:"implement,omitempty"`
	Validate  *Validate  `json:"validate,omitempty"`
	Spike     *Spike     `json:"spike,omitempty"`
}

// Research records research claims and their source references.
type Research struct {
	Request     string      `json:"request"`
	Findings    []Finding   `json:"findings,omitempty"`
	Decisions   []Statement `json:"decisions,omitempty"`
	Assumptions []Statement `json:"assumptions,omitempty"`
}

// Statement is a stable narrative item.
type Statement struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Finding is an identified research claim with optional source evidence.
type Finding struct {
	ID       string            `json:"id"`
	Text     string            `json:"text"`
	Evidence []SourceReference `json:"evidence,omitempty"`
}

// SourceReference identifies source context without granting read authority.
type SourceReference struct {
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Revision string `json:"revision,omitempty"`
}

// Spike records bounded exploratory work.
type Spike struct {
	Request   string   `json:"request"`
	WorkDone  []string `json:"work_done,omitempty"`
	Findings  []string `json:"findings,omitempty"`
	Decisions []string `json:"decisions,omitempty"`
	NotDone   []string `json:"not_done,omitempty"`
}

// Plan draft fields are omitted until supplied; completion invokes the existing
// complete typed validator, including roles, criteria and graph obligations.
//
// A graph Plan node uses neither: its done result carries exactly one of
// StoryRequest and StorySet, and its ask carries neither. Which contract applies
// is decided by the trusted Context (PlanGraph), never by the payload, so a
// legacy Plan can never be turned into a graph Plan by what it sends.
type Plan struct {
	Summary      string                    `json:"summary,omitempty"`
	Context      *agent.PlanningDiscussion `json:"context,omitempty"`
	Stories      []PlanStory               `json:"stories,omitempty"`
	StoryRequest *PlanStoryRequest         `json:"storyrequest,omitempty"`
	StorySet     *PlanStorySet             `json:"storyset,omitempty"`
}

// PlanStory retains stable identity while draft detail is enriched.
type PlanStory struct {
	Key                string                         `json:"key"`
	Intent             string                         `json:"intent,omitempty"`
	Title              string                         `json:"title"`
	UserStory          UserStory                      `json:"user_story"`
	AcceptanceCriteria []planbrief.CriterionContent   `json:"acceptance_criteria,omitempty"`
	Tasks              []planbrief.TaskContent        `json:"tasks,omitempty"`
	Architecture       *planbrief.ArchitectureContent `json:"architecture,omitempty"`
	Dependencies       *planbrief.DependencyContent   `json:"dependencies,omitempty"`
}

// UserStory separates required intent from optional draft context.
type UserStory struct {
	Actor   string   `json:"actor,omitempty"`
	Want    string   `json:"want"`
	Benefit string   `json:"benefit,omitempty"`
	Notes   []string `json:"notes,omitempty"`
}

// Implement references retained story and aggregate engineering proof.
type Implement struct {
	Stories         []ImplementationStory          `json:"stories,omitempty"`
	AggregateChecks *agent.EngineeringContribution `json:"aggregate_checks,omitempty"`
}

// ImplementationStory reports work against an authorized frozen story.
type ImplementationStory struct {
	Story       string                         `json:"story"`
	Outcome     string                         `json:"outcome"`
	Reason      string                         `json:"reason,omitempty"`
	Engineering *agent.EngineeringContribution `json:"engineering,omitempty"`
	Assessment  *agent.AssessmentContribution  `json:"assessment,omitempty"`
}

// Validate contains reference-only assessments of selected stories.
type Validate struct {
	Stories []AssessmentStory `json:"stories,omitempty"`
}

// AssessmentStory retains criterion and review findings without acceptance.
type AssessmentStory struct {
	Story      string                      `json:"story"`
	SnapshotID string                      `json:"snapshot_id"`
	Criteria   []agent.ContributionFinding `json:"criteria"`
	Reviews    []agent.ContributionFinding `json:"reviews"`
}
