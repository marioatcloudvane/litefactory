package planbrief

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// PlanDraft is the progressive graph-Plan contract. It is deliberately separate
// from TypedStory: publishing a draft never satisfies complete-story validation.
type PlanDraft struct {
	Key          string            `json:"key"`
	Name         string            `json:"name"`
	UserStory    UserStoryContent  `json:"user_story"`
	Problem      string            `json:"problem"`
	InScope      []string          `json:"in_scope"`
	NotInScope   []string          `json:"not_in_scope"`
	Architecture *PlanArchitecture `json:"architecture,omitempty"`
	Criteria     []PlanCriterion   `json:"criteria,omitempty"`
	Tasks        []TaskContent     `json:"tasks,omitempty"`
	Dependencies DependencyContent `json:"dependencies"`
	Frontend     bool              `json:"frontend"`
	Design       []string          `json:"design,omitempty"`
	Guidance     []string          `json:"guidance,omitempty"`
	Decisions    []string          `json:"decisions,omitempty"`
	Assumptions  []string          `json:"assumptions,omitempty"`
	Questions    []string          `json:"questions,omitempty"`
}

// PlanArchitecture carries only applicable, reusable design sections.
type PlanArchitecture struct {
	General        []GeneralDecision    `json:"general"`
	DataModel      []DataDefinition     `json:"data_model,omitempty"`
	ServiceDesign  []ServiceDefinition  `json:"service_design,omitempty"`
	SolutionDesign []SolutionDefinition `json:"solution_design,omitempty"`
}

// GeneralDecision is an applicable architecture choice and rationale.
type GeneralDecision struct {
	Area      string `json:"area"`
	Decision  string `json:"decision"`
	Rationale string `json:"rationale"`
}

// DataDefinition describes an entity or relationship.
type DataDefinition struct {
	Entity      string `json:"entity"`
	Definition  string `json:"definition"`
	Constraints string `json:"constraints"`
}

// ServiceDefinition describes layering and access responsibilities.
type ServiceDefinition struct {
	Component      string `json:"component"`
	Responsibility string `json:"responsibility"`
	Access         string `json:"access"`
}

// SolutionDefinition describes collaboration and contracts.
type SolutionDefinition struct {
	Element   string `json:"element"`
	Contract  string `json:"contract"`
	Rationale string `json:"rationale"`
}

// PlanCriterion separates future proof methods from executed evidence.
type PlanCriterion struct {
	ID          string `json:"id"`
	Text        string `json:"text"`
	ProofMethod string `json:"proof_method"`
}

var prefixedPlanName = regexp.MustCompile(`^[0-9]+-[0-9]+:`)

// Validate applies stage-local shape and factory-supplied role/reference bounds.
func (d PlanDraft) Validate(stage string, scope ContentScope) error {
	var faults []error
	if !slices.Contains([]string{"story", "technical", "acceptance"}, stage) {
		faults = append(faults, errors.New("invalid Plan stage"))
	}
	if !validContentUTF8(reflect.ValueOf(d)) {
		faults = append(faults, errors.New("draft contains invalid UTF-8"))
	}
	if !proposalKey.MatchString(d.Key) {
		faults = append(faults, errors.New("invalid story key"))
	}
	if strings.TrimSpace(d.Name) == "" || len(d.Name) > 200 || strings.ContainsAny(d.Name, "\r\n") || prefixedPlanName.MatchString(strings.TrimSpace(d.Name)) {
		faults = append(faults, errors.New("name must be unprefixed, nonblank and single-line (200 bytes maximum)"))
	}
	for _, value := range []string{d.UserStory.Actor, d.UserStory.Want, d.UserStory.Benefit, d.Problem} {
		if strings.TrimSpace(value) == "" {
			faults = append(faults, errors.New("actor, want, benefit and problem are required"))
		}
	}
	if len(d.InScope) == 0 || d.NotInScope == nil {
		faults = append(faults, errors.New("explicit in_scope and not_in_scope lists required"))
	}
	if stage == "story" && (d.Architecture != nil || len(d.Tasks) > 0 || len(d.Criteria) > 0 || len(d.Design) > 0 || len(d.Guidance) > 0) {
		faults = append(faults, errors.New("story stage cannot author architecture, tasks, criteria, design or guidance"))
	}
	if stage == "technical" && (len(d.Tasks) > 0 || len(d.Criteria) > 0) {
		faults = append(faults, errors.New("technical stage cannot author tasks or criteria"))
	}
	if stage != "story" && (d.Architecture == nil || len(d.Architecture.General) == 0) {
		faults = append(faults, errors.New("populated Architecture - General is required"))
	}
	if !d.Frontend && len(d.Design) > 0 {
		faults = append(faults, errors.New("design references require frontend scope"))
	}
	seen := map[string]bool{}
	for _, g := range d.Guidance {
		if !slices.Contains([]string{"go", "agent-setup"}, g) || seen[g] {
			faults = append(faults, errors.New("invalid or duplicate selected guidance"))
		}
		seen[g] = true
	}
	seen = map[string]bool{}
	allowed := contentSet(scope.IssueRefs)
	for _, ref := range d.Dependencies.IssueRefs {
		if !allowed[ref] || seen[ref] {
			faults = append(faults, errors.New("unauthorized or duplicate issue dependency"))
		}
		seen[ref] = true
	}
	seen = map[string]bool{}
	for _, key := range d.Dependencies.StoryKeys {
		if !proposalKey.MatchString(key) || key == d.Key || seen[key] {
			faults = append(faults, errors.New("invalid or duplicate story dependency"))
		}
		seen[key] = true
	}
	if d.Architecture != nil {
		faults = append(faults, validatePlanRows(reflect.ValueOf(*d.Architecture))...)
	}
	for _, list := range [][]string{d.InScope, d.NotInScope, d.Design, d.Decisions, d.Assumptions, d.Questions} {
		if len(list) > 256 {
			faults = append(faults, errors.New("draft list exceeds 256 entries"))
		}
		for _, v := range list {
			if strings.TrimSpace(v) == "" {
				faults = append(faults, errors.New("blank draft list entry"))
			}
		}
	}
	if stage == "acceptance" {
		faults = append(faults, d.validateCompletion(scope)...)
	}
	return errors.Join(faults...)
}

func validatePlanRows(value reflect.Value) []error {
	var faults []error
	for i := 0; i < value.NumField(); i++ {
		rows := value.Field(i)
		if rows.Len() > 256 {
			faults = append(faults, errors.New("architecture table exceeds 256 rows"))
		}
		for j := 0; j < rows.Len(); j++ {
			row := rows.Index(j)
			for k := 0; k < row.NumField(); k++ {
				if strings.TrimSpace(row.Field(k).String()) == "" {
					faults = append(faults, errors.New("architecture table cells must be nonblank"))
				}
			}
		}
	}
	return faults
}

func (d PlanDraft) validateCompletion(scope ContentScope) []error {
	var faults []error
	if len(d.Criteria) == 0 || len(d.Criteria) > MaxCriteria || len(d.Tasks) == 0 || len(d.Tasks) > 256 {
		faults = append(faults, errors.New("complete Plan requires bounded nonempty criteria and tasks"))
	}
	ids := map[string]bool{}
	covered := map[string]bool{}
	for _, c := range d.Criteria {
		if !ValidCriterionID(c.ID) || ids[c.ID] || strings.TrimSpace(c.Text) == "" || len(c.Text) > MaxCriterionBytes || strings.TrimSpace(c.ProofMethod) == "" {
			faults = append(faults, errors.New("criterion requires unique stable ID, text and proof method"))
		}
		ids[c.ID] = true
	}
	graph := map[string][]string{}
	roles := contentSet(scope.Roles)
	for _, t := range d.Tasks {
		if !proposalKey.MatchString(t.ID) || !roles[t.OwnerRole] || strings.TrimSpace(t.Description) == "" || len(t.CriterionIDs) == 0 {
			faults = append(faults, errors.New("task requires ID, description, authorized role and criterion links"))
		}
		if _, ok := graph[t.ID]; ok {
			faults = append(faults, errors.New("duplicate task ID"))
		}
		graph[t.ID] = t.DependsOn
		linked := map[string]bool{}
		for _, id := range t.CriterionIDs {
			if !ids[id] || linked[id] {
				faults = append(faults, errors.New("invalid or duplicate task criterion link"))
			}
			covered[id] = true
			linked[id] = true
		}
	}
	for id := range ids {
		if !covered[id] {
			faults = append(faults, fmt.Errorf("criterion %s has no task", id))
		}
	}
	faults = append(faults, validateContentGraph("tasks", graph)...)
	return faults
}

// Revision binds exact semantic content. Presentation changes are not revisions.
func (d PlanDraft) Revision() string {
	raw, _ := json.Marshal(d)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
}

// MergePlanDraft preserves settled sections unless explicitly selected for
// replacement. Empty replacement slices intentionally remove optional sections.
// An unknown section name is ignored; the Plan result check reports it to the agent.
func MergePlanDraft(prior, next PlanDraft, changed []string) (PlanDraft, error) {
	if prior.Key != next.Key {
		return PlanDraft{}, errors.New("story identity cannot change")
	}
	out := prior
	seen := map[string]bool{}
	for _, section := range changed {
		if seen[section] {
			return PlanDraft{}, errors.New("duplicate changed section")
		}
		seen[section] = true
		switch section {
		case "intent":
			out.Name = next.Name
			out.UserStory = next.UserStory
			out.Problem = next.Problem
			out.InScope = next.InScope
			out.NotInScope = next.NotInScope
			out.Frontend = next.Frontend
		case "architecture":
			out.Architecture = next.Architecture
		case "criteria":
			out.Criteria = next.Criteria
		case "tasks":
			out.Tasks = next.Tasks
		case "dependencies":
			out.Dependencies = next.Dependencies
		case "design":
			out.Design = next.Design
		case "guidance":
			out.Guidance = next.Guidance
		case "conversation":
			out.Decisions = next.Decisions
			out.Assumptions = next.Assumptions
			out.Questions = next.Questions
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return PlanDraft{}, err
	}
	var detached PlanDraft
	if err := json.Unmarshal(raw, &detached); err != nil {
		return PlanDraft{}, err
	}
	return detached, nil
}

// PlanAcceptanceHeading preserves the exact frozen story body protocol spelling.
const PlanAcceptanceHeading = "# Acceptance Criterias" //nolint:misspell // Frozen renderer contract mandates these exact bytes.
