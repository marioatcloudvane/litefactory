package planbrief

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

// PlanDraftEdit is a recognized owner edit, not remote mutation authority.
// Stage is the earliest affected stage; empty means presentation-only.
type PlanDraftEdit struct {
	Draft            PlanDraft
	Stage            string
	PresentationOnly bool
}

var planTableRule = regexp.MustCompile(`^:?-+:?$`)

// ReconcilePlanDraftEdit parses the activity-owned visible scaffold, preserving
// hidden semantic links. The caller must verify remote parent, issue identity,
// provenance, open state and expected observed revision before retaining it.
// Unrecognized owner text returns an error and must remain untouched on the issue.
func ReconcilePlanDraftEdit(prior PlanDraft, parentNumber, ordinal int64, title, body string, scope ContentScope, parentKey ...string) (PlanDraftEdit, error) {
	var out PlanDraftEdit
	if parentNumber < 1 || ordinal < 1 || len(body) > MaxStoryBodyBytes || len(title) > 256 {
		return out, errors.New("pending: invalid Plan edit bounds or identity")
	}
	if strings.Contains(body, PlanShortenedMarker) {
		return out, errors.New("pending: draft shortened; reply with changes")
	}
	prefix := fmt.Sprintf("%d-%03d: ", parentNumber, ordinal)
	if len(parentKey) > 0 {
		displayPrefix := fmt.Sprintf("%s-%03d: ", parentKey[0], ordinal)
		if strings.HasPrefix(title, displayPrefix) {
			prefix = displayPrefix
		}
	}
	if !strings.HasPrefix(title, prefix) {
		return out, errors.New("pending: Plan title changed its reserved identity prefix")
	}
	raw, err := json.Marshal(prior)
	if err != nil {
		return out, err
	}
	var d PlanDraft
	if err = json.Unmarshal(raw, &d); err != nil {
		return out, err
	}
	d.Name = strings.TrimPrefix(title, prefix)
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var visible []string
	metadata := 0
	markers := map[string]bool{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "<!--") {
			if !strings.HasSuffix(line, " -->") {
				return out, errors.New("pending: malformed Plan metadata")
			}
			if strings.HasPrefix(line, "<!-- lf:plan-content v=1 data=") {
				metadata++
				data := strings.TrimSuffix(strings.TrimPrefix(line, "<!-- lf:plan-content v=1 data="), " -->")
				var retained PlanDraft
				dec := json.NewDecoder(strings.NewReader(data))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&retained); err != nil {
					return out, fmt.Errorf("pending: invalid Plan metadata: %w", err)
				}
				canonical, e := json.Marshal(retained)
				if e != nil || !bytes.Equal(canonical, []byte(data)) {
					return out, errors.New("pending: modified or noncanonical Plan metadata")
				}
				if err := samePlanHiddenContent(prior, retained); err != nil {
					return out, err
				}
			} else {
				kind := ""
				for _, name := range []string{"lf:plan-story ", "lf:plan-request "} {
					if strings.HasPrefix(line, "<!-- "+name) {
						kind = name
					}
				}
				if kind == "" || markers[kind] {
					return out, errors.New("pending: changed or duplicate Plan metadata marker")
				}
				markers[kind] = true
			}
			continue
		}
		if strings.Contains(line, "<!--") || strings.Contains(line, "-->") {
			return out, errors.New("pending: ambiguous embedded Plan metadata")
		}
		if line != "" {
			visible = append(visible, line)
		}
	}
	if metadata != 1 {
		return out, errors.New("pending: exact Plan content metadata required")
	}
	expected := []string{"# Userstory", "## What this story solves", "## Scope", "# Task", PlanAcceptanceHeading, "# Architecture"}
	sections := map[string][]string{}
	order := []string{}
	current := ""
	for _, line := range visible {
		if strings.HasPrefix(line, "#") {
			if _, exists := sections[line]; exists {
				return out, errors.New("pending: duplicate Plan section")
			}
			sections[line] = []string{}
			order = append(order, line)
			current = line
			continue
		}
		if current == "" {
			return out, errors.New("pending: unrecognized text outside Plan scaffold")
		}
		sections[current] = append(sections[current], line)
	}
	if len(order) < len(expected) || !slices.Equal(order[:len(expected)], expected) {
		return out, errors.New("pending: prescribed Plan heading order changed")
	}
	optional := []string{"## Architecture - General", "## Data Model", "## Service Design", "## Solution Design", "# Design"}
	last := -1
	for _, name := range order[len(expected):] {
		i := slices.Index(optional, name)
		if i < 0 || i <= last {
			return out, errors.New("pending: unknown or unordered Plan architecture/design section")
		}
		last = i
	}
	user := sections["# Userstory"]
	if len(user) == 0 || !strings.HasPrefix(user[0], "As ") || !strings.HasSuffix(user[0], ".") || strings.Count(user[0], ", I want ") != 1 || strings.Count(user[0], ", so that ") != 1 {
		return out, errors.New("pending: ambiguous actor/want/benefit sentence")
	}
	actor, rest, _ := strings.Cut(strings.TrimPrefix(user[0], "As "), ", I want ")
	want, benefit, _ := strings.Cut(strings.TrimSuffix(rest, "."), ", so that ")
	d.UserStory.Actor = editedText(actor)
	d.UserStory.Want = editedText(want)
	d.UserStory.Benefit = editedText(benefit)
	d.UserStory.Notes, err = planEditList(user[1:])
	if err != nil {
		return out, err
	}
	problem := sections["## What this story solves"]
	if len(problem) != 1 {
		return out, errors.New("pending: Plan problem must be one plain-language paragraph")
	}
	d.Problem = editedText(problem[0])
	scopeLines := sections["## Scope"]
	split := slices.Index(scopeLines, "Not in Scope")
	if len(scopeLines) == 0 || scopeLines[0] != "In-Scope" || split < 1 {
		return out, errors.New("pending: separate In-Scope and Not in Scope lists required")
	}
	d.InScope, err = planEditList(scopeLines[1:split])
	if err != nil {
		return out, err
	}
	d.NotInScope, err = planEditList(scopeLines[split+1:])
	if err != nil {
		return out, err
	}
	rows, err := planEditTable(sections["# Task"], []string{"Task Id", "Description", "Owner", "Dependencies"})
	if err != nil {
		return out, err
	}
	oldTasks := map[string]TaskContent{}
	for _, task := range prior.Tasks {
		oldTasks[task.ID] = task
	}
	d.Tasks = nil
	seen := map[string]bool{}
	for _, row := range rows {
		task, ok := oldTasks[row[0]]
		if !ok || seen[row[0]] {
			return out, errors.New("pending: new or duplicate task needs explicit criterion links")
		}
		seen[row[0]] = true
		task.Description = row[1]
		task.OwnerRole = row[2]
		task.DependsOn = editedList(row[3])
		d.Tasks = append(d.Tasks, task)
	}
	oldCriteria := map[string]PlanCriterion{}
	for _, criterion := range prior.Criteria {
		oldCriteria[criterion.ID] = criterion
	}
	d.Criteria = nil
	seen = map[string]bool{}
	for _, line := range sections[PlanAcceptanceHeading] {
		_, value, ok := criterionItem(line)
		if !ok {
			return out, errors.New("pending: malformed criterion")
		}
		id, text, ok := strings.Cut(value, ":")
		id = strings.TrimSpace(id)
		criterion, exists := oldCriteria[id]
		if !ok || !exists || seen[id] {
			return out, errors.New("pending: new, missing or duplicate criterion ID needs explicit proof method")
		}
		seen[id] = true
		criterion.Text = editedText(text)
		d.Criteria = append(d.Criteria, criterion)
	}
	if len(sections["# Architecture"]) != 0 {
		return out, errors.New("pending: architecture must use prescribed typed tables")
	}
	d.Architecture = nil
	if general, exists := sections["## Architecture - General"]; exists {
		rows, err := planEditTable(general, []string{"Area", "Decision", "Rationale"})
		if err != nil {
			return out, err
		}
		d.Architecture = &PlanArchitecture{}
		for _, row := range rows {
			d.Architecture.General = append(d.Architecture.General, GeneralDecision{Area: row[0], Decision: row[1], Rationale: row[2]})
		}
	}
	for _, section := range optional[1:4] {
		data, exists := sections[section]
		if !exists {
			continue
		}
		if d.Architecture == nil {
			return out, errors.New("pending: Architecture - General required before optional tables")
		}
		var headers []string
		switch section {
		case "## Data Model":
			headers = []string{"Entity / Relationship", "Definition", "Constraints / Rationale"}
		case "## Service Design":
			headers = []string{"Layer / Component", "Responsibility", "Dependencies / Access"}
		case "## Solution Design":
			headers = []string{"Element", "Collaboration / Contract", "Rationale"}
		}
		rows, err := planEditTable(data, headers)
		if err != nil {
			return out, err
		}
		for _, row := range rows {
			switch section {
			case "## Data Model":
				d.Architecture.DataModel = append(d.Architecture.DataModel, DataDefinition{Entity: row[0], Definition: row[1], Constraints: row[2]})
			case "## Service Design":
				d.Architecture.ServiceDesign = append(d.Architecture.ServiceDesign, ServiceDefinition{Component: row[0], Responsibility: row[1], Access: row[2]})
			case "## Solution Design":
				d.Architecture.SolutionDesign = append(d.Architecture.SolutionDesign, SolutionDefinition{Element: row[0], Contract: row[1], Rationale: row[2]})
			}
		}
	}
	d.Design = nil
	if lines, exists := sections["# Design"]; exists {
		if !d.Frontend {
			return out, errors.New("pending: Design requires approved frontend scope")
		}
		d.Design, err = planEditList(lines)
		if err != nil {
			return out, err
		}
	}
	stage := "story"
	if d.Architecture != nil {
		stage = "technical"
	}
	if len(d.Tasks) > 0 || len(d.Criteria) > 0 {
		stage = "acceptance"
	}
	if err := d.ValidateRetained(stage, scope); err != nil {
		return out, fmt.Errorf("pending: incomplete edited Plan content: %w", err)
	}
	// A visible deletion is an explicit scoped change; absent unrelated hidden
	// fields are preserved from prior rather than fabricated by parsing prose.
	out.Draft = d
	for _, candidateStage := range planStages {
		if !equalPlanStage(prior, d, candidateStage) {
			out.Stage = candidateStage
			break
		}
	}
	out.PresentationOnly = out.Stage == ""
	if out.PresentationOnly {
		out.Draft = prior
	}
	return out, nil
}

func equalPlanStage(a, b PlanDraft, stage string) bool {
	// Rendering makes nil/empty collections and whitespace-only layout equivalent.
	normalize := func(d PlanDraft) string {
		d.Tasks = slices.Clone(d.Tasks)
		if len(d.UserStory.Notes) == 0 {
			d.UserStory.Notes = nil
		}
		if len(d.Tasks) == 0 {
			d.Tasks = nil
		}
		if len(d.Criteria) == 0 {
			d.Criteria = nil
		}
		if len(d.Design) == 0 {
			d.Design = nil
		}
		for i := range d.Tasks {
			if len(d.Tasks[i].DependsOn) == 0 {
				d.Tasks[i].DependsOn = nil
			}
		}
		return d.StageRevision(stage)
	}
	return normalize(a) == normalize(b)
}

func samePlanHiddenContent(prior, retained PlanDraft) error {
	// Visible values may differ after a prior owner edit; hidden associations are
	// retained only from trusted prior content and must not be rewritten in HTML.
	if prior.Key != retained.Key || prior.Frontend != retained.Frontend || !reflect.DeepEqual(prior.Dependencies, retained.Dependencies) || !reflect.DeepEqual(prior.Guidance, retained.Guidance) || !reflect.DeepEqual(prior.Decisions, retained.Decisions) || !reflect.DeepEqual(prior.Assumptions, retained.Assumptions) || !reflect.DeepEqual(prior.Questions, retained.Questions) {
		return errors.New("pending: owner edit changed hidden Plan identity or semantic metadata")
	}
	if len(prior.Tasks) != len(retained.Tasks) || len(prior.Criteria) != len(retained.Criteria) {
		return errors.New("pending: hidden task or criterion identity set changed")
	}
	tasks := map[string]TaskContent{}
	for _, task := range retained.Tasks {
		if _, exists := tasks[task.ID]; exists {
			return errors.New("pending: duplicate hidden task identity")
		}
		tasks[task.ID] = task
	}
	for _, task := range prior.Tasks {
		old, exists := tasks[task.ID]
		if !exists || !slices.Equal(task.CriterionIDs, old.CriterionIDs) {
			return errors.New("pending: hidden task identity or criterion links changed")
		}
	}
	criteria := map[string]PlanCriterion{}
	for _, criterion := range retained.Criteria {
		if _, exists := criteria[criterion.ID]; exists {
			return errors.New("pending: duplicate hidden criterion identity")
		}
		criteria[criterion.ID] = criterion
	}
	for _, criterion := range prior.Criteria {
		old, exists := criteria[criterion.ID]
		if !exists || criterion.ProofMethod != old.ProofMethod {
			return errors.New("pending: hidden criterion identity or proof method changed")
		}
	}
	return nil
}

func planEditList(lines []string) ([]string, error) {
	out := []string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "- ") {
			return nil, errors.New("pending: unrecognized text in Plan list")
		}
		out = append(out, editedText(strings.TrimPrefix(line, "- ")))
	}
	return out, nil
}
func planEditTable(lines, headers []string) ([][]string, error) {
	if len(lines) < 2 {
		return nil, errors.New("pending: required Plan table header missing")
	}
	cells := func(line string) ([]string, error) {
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			return nil, errors.New("pending: malformed Plan table row")
		}
		values := strings.Split(strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|"), "|")
		if len(values) != len(headers) {
			return nil, errors.New("pending: ambiguous Plan table columns")
		}
		for i := range values {
			values[i] = strings.TrimSpace(values[i])
		}
		return values, nil
	}
	got, err := cells(lines[0])
	if err != nil {
		return nil, err
	}
	if !slices.Equal(got, headers) {
		return nil, errors.New("pending: prescribed Plan table columns changed")
	}
	rules, err := cells(lines[1])
	if err != nil {
		return nil, err
	}
	for _, rule := range rules {
		if !planTableRule.MatchString(rule) {
			return nil, errors.New("pending: malformed Plan table separator")
		}
	}
	var rows [][]string
	for _, line := range lines[2:] {
		values, err := cells(line)
		if err != nil {
			return nil, err
		}
		for i := range values {
			values[i] = editedText(values[i])
		}
		rows = append(rows, values)
	}
	return rows, nil
}

// PlanShortenedMarker is the hidden line a shortened story body carries; the full
// draft then lives in numbered part comments on the same issue.
const PlanShortenedMarker = "<!-- lf:plan-shortened -->"
