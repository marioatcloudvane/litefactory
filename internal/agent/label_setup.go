package agent

import (
	"regexp"
	"strings"
)

// LabelFamily groups the labels the factory owns on a GitHub project.
type LabelFamily string

const (
	// LabelFamilyMode holds the five Mode labels.
	LabelFamilyMode LabelFamily = "mode"
	// LabelFamilyAgent holds one Coding Agent label per runner.
	LabelFamilyAgent LabelFamily = "agent"
	// LabelFamilyBug holds the bug label.
	LabelFamilyBug LabelFamily = "bug"
)

// BugLabel is the exact name the daemon's bug routing compares against.
const BugLabel = "bug"

// CodingAgentLabelPrefix is the prefix the daemon reads for agent selection.
const CodingAgentLabelPrefix = "Coding Agent: "

var codingAgentValue = regexp.MustCompile(`^[A-Z][A-Za-z0-9_-]{0,39}$`)

// WantedLabel is one label setup makes sure exists.
type WantedLabel struct {
	Name        string
	Family      LabelFamily
	Color       string // six hex digits, no '#'
	Description string
}

// WantedLabels builds the fixed-order list: Mode labels, one Coding Agent label
// per agent name, then bug. Agent names are lower-case kinds such as "claude".
func WantedLabels(agents []string) []WantedLabel {
	var out []WantedLabel
	for _, m := range []Mode{ModeResearch, ModePlan, ModeImplement, ModeValidate, ModeSpike} {
		out = append(out, WantedLabel{Name: m.Label(), Family: LabelFamilyMode, Color: "1d76db", Description: "Litefactory workflow mode: " + string(m)})
	}
	seen := map[string]bool{}
	for _, a := range agents {
		a = strings.TrimSpace(a)
		if a == "" || seen[strings.ToLower(a)] {
			continue
		}
		seen[strings.ToLower(a)] = true
		display := strings.ToUpper(a[:1]) + a[1:]
		out = append(out, WantedLabel{Name: CodingAgentLabelPrefix + display, Family: LabelFamilyAgent, Color: "5319e7", Description: "Litefactory coding agent: " + strings.ToLower(a)})
	}
	return append(out, WantedLabel{Name: BugLabel, Family: LabelFamilyBug, Color: "d73a4a", Description: "Something is broken; routes to the bug workflow"})
}

// ReaderAcceptsLabel says whether today's daemon reader accepts an existing
// label name as the wanted label. The reason is plain text when it does not.
func ReaderAcceptsLabel(w WantedLabel, existing string) (bool, string) {
	switch w.Family {
	case LabelFamilyMode:
		if existing != w.Name {
			return false, "the factory reads a mode label only as exactly \"" + w.Name + "\""
		}
	case LabelFamilyBug:
		if existing != BugLabel {
			return false, "the factory reads the bug label only as exactly \"" + BugLabel + "\""
		}
	case LabelFamilyAgent:
		value, ok := strings.CutPrefix(existing, CodingAgentLabelPrefix)
		if !ok {
			return false, "the factory reads an agent label only with the exact prefix \"" + CodingAgentLabelPrefix + "\""
		}
		if !codingAgentValue.MatchString(value) {
			return false, "the agent name after the prefix must start with a capital letter"
		}
		if !strings.EqualFold(CodingAgentLabelPrefix+value, w.Name) {
			return false, "the label does not name the runner \"" + w.Name + "\""
		}
	}
	return true, ""
}

// LabelAdoption pairs a wanted label with the existing label that satisfies it.
type LabelAdoption struct {
	Wanted   string
	Existing string
}

// LabelWarning names an adopted label the reader refuses.
type LabelWarning struct {
	Label  string
	Reason string
}

// LabelPlan is the pure outcome of comparing wanted and existing labels.
type LabelPlan struct {
	Adopted  []LabelAdoption
	Create   []WantedLabel
	Warnings []LabelWarning
}

// PlanLabels adopts existing labels that match a wanted name ignoring case,
// lists the rest to create in wanted order, and warns about adopted names the
// reader refuses. It never plans a change to an existing label.
func PlanLabels(wanted []WantedLabel, existing []string) LabelPlan {
	var plan LabelPlan
	for _, w := range wanted {
		found := ""
		for _, e := range existing {
			if e == w.Name {
				found = e
				break
			}
			if found == "" && strings.EqualFold(e, w.Name) {
				found = e
			}
		}
		if found == "" {
			plan.Create = append(plan.Create, w)
			continue
		}
		plan.Adopted = append(plan.Adopted, LabelAdoption{Wanted: w.Name, Existing: found})
		if ok, reason := ReaderAcceptsLabel(w, found); !ok {
			plan.Warnings = append(plan.Warnings, LabelWarning{Label: found, Reason: reason})
		}
	}
	return plan
}
