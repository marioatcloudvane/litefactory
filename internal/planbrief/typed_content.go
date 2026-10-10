package planbrief

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// ContentVersion versions typed content independently of the execution workflow.
const ContentVersion = 2

// Shared content and evidence budgets are UTF-8/serialized byte counts, not rune counts.
const (
	MaxCriteria             = 256
	MaxCriterionBytes       = 4096
	MaxSubmissionBytes      = 4 * 1024 * 1024
	MaxRetainedRunBytes     = 64 * 1024 * 1024
	MaxEvidenceExplanations = 16
	MaxEvidenceSummaryBytes = 8192
)

// StoryGuidance is rendered outside the obligations section.
const StoryGuidance = "Want to change this story? Comment with the requested change and litefactory will update it. Direct edits are supported, but comments help avoid conflicting revisions."

// TypedPlan contains agent-authored content only; the factory binds identities.
type TypedPlan struct {
	Summary string       `json:"summary"`
	Stories []TypedStory `json:"stories"`
}

// TypedStory contains the complete typed content of one proposed story.
type TypedStory struct {
	Key                string              `json:"key"`
	Intent             string              `json:"intent"`
	Title              string              `json:"title"`
	UserStory          UserStoryContent    `json:"user_story"`
	AcceptanceCriteria []CriterionContent  `json:"acceptance_criteria"`
	Tasks              []TaskContent       `json:"tasks"`
	Architecture       ArchitectureContent `json:"architecture"`
	Dependencies       DependencyContent   `json:"dependencies"`
}

// UserStoryContent separates the actor, requested behavior and benefit from notes.
type UserStoryContent struct {
	Actor   string   `json:"actor"`
	Want    string   `json:"want"`
	Benefit string   `json:"benefit"`
	Notes   []string `json:"notes"`
}

// CriterionContent binds one explicit obligation to its stable identifier.
type CriterionContent struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// TaskContent assigns work to a supplied role and references its obligations and prerequisites.
type TaskContent struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	OwnerRole    string   `json:"owner_role"`
	CriterionIDs []string `json:"criterion_ids"`
	DependsOn    []string `json:"depends_on"`
}

// ArchitectureContent preserves the narrative and collections of a story design.
type ArchitectureContent struct {
	Summary     string             `json:"summary"`
	Components  []ComponentContent `json:"components"`
	Interfaces  []InterfaceContent `json:"interfaces"`
	Constraints []string           `json:"constraints"`
	Decisions   []DecisionContent  `json:"decisions"`
}

// ComponentContent names a design component and its responsibility.
type ComponentContent struct {
	Name           string `json:"name"`
	Responsibility string `json:"responsibility"`
}

// InterfaceContent describes a named boundary and its input and output.
type InterfaceContent struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Output string `json:"output"`
}

// DecisionContent records a design choice together with its reason.
type DecisionContent struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// DependencyContent separates local story prerequisites from authorized external references.
type DependencyContent struct {
	StoryKeys []string `json:"story_keys"`
	IssueRefs []string `json:"issue_refs"`
}

// ContentScope is supplied by the factory, never inferred from submitted content.
// IssueRefs contains only resolved, authorized external task references.
type ContentScope struct {
	Roles     []string
	IssueRefs []string
	// Guidance is nil only for historical content; a nonnil roster is release-scoped.
	Guidance []string
}

// taskOwnerRoles lists the historical Go Implement specialists that may own planned
// tasks. The orchestrator sequences work and owns no task.
var taskOwnerRoles = []string{"go-implementation-engineer", "go-unit-test-engineer", "go-integration-test-engineer"}

// TaskOwnerScope is the explicit historical content scope. New releases use
// ReleaseScope with their admitted roles and selected guidance.
func TaskOwnerScope() ContentScope {
	return ContentScope{Roles: slices.Clone(taskOwnerRoles), IssueRefs: []string{}}
}

// ReleaseScope retains closed rosters supplied by the admitted team release.
// Empty guidance explicitly admits no stack guidance; it never falls back to Go.
func ReleaseScope(roles, guidance []string) ContentScope {
	return ContentScope{Roles: slices.Clone(roles), IssueRefs: []string{}, Guidance: append([]string{}, guidance...)}
}

func guidanceScope(scope ContentScope) []string {
	if scope.Guidance == nil {
		return []string{"go", "agent-setup"}
	}
	return scope.Guidance
}

var canonicalCriterion = regexp.MustCompile(`^AC-(?:00[1-9]|0[1-9][0-9]|[1-9][0-9]{2,5})$`)

// ValidCriterionID reports canonical positive IDs, with minimum-three-digit padding.
func ValidCriterionID(id string) bool { return canonicalCriterion.MatchString(id) }

// ParseTypedPlan strictly checks all object shapes before semantic validation.
// Publication must additionally validate rendered sizes using report.RenderTypedPlan.
func ParseTypedPlan(data []byte, scope ContentScope) (TypedPlan, error) {
	var p TypedPlan
	if len(data) > MaxProposalBytes {
		return p, fmt.Errorf("plan exceeds %d serialized bytes", MaxProposalBytes)
	}
	if err := DecodeTypedContent(data, &p); err != nil {
		return p, err
	}
	if err := p.Validate(scope); err != nil {
		return TypedPlan{}, err
	}
	return p, nil
}

// Validate validates the complete story graph and escaped JSON budget.
func (p TypedPlan) Validate(scope ContentScope) error {
	var faults []error
	if !validContentUTF8(reflect.ValueOf(p)) {
		return errors.New("plan: invalid UTF-8")
	}
	if strings.TrimSpace(p.Summary) == "" {
		faults = append(faults, errors.New("summary: must be nonblank"))
	}
	if len(p.Stories) == 0 || len(p.Stories) > 100 {
		faults = append(faults, errors.New("stories: requires 1–100 stories"))
	}
	graph := map[string][]string{}
	for i, s := range p.Stories {
		if _, exists := graph[s.Key]; exists {
			faults = append(faults, fmt.Errorf("stories[%d].key: duplicate", i))
		}
		graph[s.Key] = s.Dependencies.StoryKeys
		if err := s.Validate(scope); err != nil {
			faults = append(faults, fmt.Errorf("stories[%d]: %w", i, err))
		}
	}
	faults = append(faults, validateContentGraph("stories", graph)...)
	data, err := json.Marshal(p)
	if err != nil {
		faults = append(faults, err)
	} else if len(data) > MaxProposalBytes {
		faults = append(faults, fmt.Errorf("plan exceeds %d escaped JSON bytes", MaxProposalBytes))
	}
	return errors.Join(faults...)
}

// Validate checks typed fields, local references, and authorized roles/issues.
func (s TypedStory) Validate(scope ContentScope) error {
	var faults []error
	if !validContentUTF8(reflect.ValueOf(s)) {
		return errors.New("story: invalid UTF-8")
	}
	// Applying the schema to serialized Go values also rejects nil collections.
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	var checked TypedStory
	if err := DecodeTypedContent(data, &checked); err != nil {
		faults = append(faults, err)
	}
	if !proposalKey.MatchString(s.Key) {
		faults = append(faults, errors.New("key: invalid story key"))
	}
	if s.Intent != "create" && s.Intent != "update" && s.Intent != "unchanged" {
		faults = append(faults, errors.New("intent: expected create, update or unchanged"))
	}
	if len(s.Title) > 256 || strings.ContainsAny(s.Title, "\r\n") {
		faults = append(faults, errors.New("title: requires single line at most 256 bytes"))
	}
	if len(s.AcceptanceCriteria) < 1 || len(s.AcceptanceCriteria) > MaxCriteria {
		faults = append(faults, fmt.Errorf("acceptance_criteria: requires 1–%d entries", MaxCriteria))
	}
	ids := map[string]bool{}
	for i, c := range s.AcceptanceCriteria {
		if !ValidCriterionID(c.ID) || ids[c.ID] {
			faults = append(faults, fmt.Errorf("acceptance_criteria[%d].id: invalid or duplicate canonical ID", i))
		}
		ids[c.ID] = true
		if len(c.Text) > MaxCriterionBytes {
			faults = append(faults, fmt.Errorf("acceptance_criteria[%d].text: exceeds %d UTF-8 bytes", i, MaxCriterionBytes))
		}
	}
	if len(s.Tasks) == 0 {
		faults = append(faults, errors.New("tasks: requires at least one task"))
	}
	graph := map[string][]string{}
	for i, t := range s.Tasks {
		if !proposalKey.MatchString(t.ID) {
			faults = append(faults, fmt.Errorf("tasks[%d].id: invalid ID", i))
		}
		if _, exists := graph[t.ID]; exists {
			faults = append(faults, fmt.Errorf("tasks[%d].id: duplicate", i))
		}
		graph[t.ID] = t.DependsOn
		if !scopeAdmitsRole(scope, t.OwnerRole) {
			faults = append(faults, fmt.Errorf("tasks[%d].owner_role: unknown role", i))
		}
		seen := map[string]bool{}
		for j, id := range t.CriterionIDs {
			if !ids[id] || seen[id] {
				faults = append(faults, fmt.Errorf("tasks[%d].criterion_ids[%d]: unknown or duplicate criterion", i, j))
			}
			seen[id] = true
		}
	}
	faults = append(faults, validateContentGraph("tasks", graph)...)
	allowed := contentSet(scope.IssueRefs)
	seen := map[string]bool{}
	for i, ref := range s.Dependencies.IssueRefs {
		if !allowed[ref] || seen[ref] {
			faults = append(faults, fmt.Errorf("dependencies.issue_refs[%d]: unauthorized or duplicate reference", i))
		}
		seen[ref] = true
	}
	return errors.Join(faults...)
}

// scopeAdmitsRole reports whether a stored task owner belongs to the scope. A
// release scope also admits go-<role> for its <role>: the Go team renamed its
// task owners when it gained a stack profile, and stories drafted before that
// keep their stored names. The historical scope stays exact.
func scopeAdmitsRole(scope ContentScope, role string) bool {
	if slices.Contains(scope.Roles, role) {
		return true
	}
	neutral, aliased := strings.CutPrefix(role, "go-")
	return aliased && scope.Guidance != nil && slices.Contains(scope.Roles, neutral)
}

func contentSet(values []string) map[string]bool {
	result := make(map[string]bool, len(values))
	for _, v := range values {
		result[v] = true
	}
	return result
}
