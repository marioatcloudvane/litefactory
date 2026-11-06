package projectconnection

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
)

// LabelScope says where a Linear label lives.
type LabelScope string

// LabelForm says whether a Linear label sits in a group.
type LabelForm string

// LinearLabelStatus is the planned outcome for one wanted label.
type LinearLabelStatus string

// LabelSeverity marks a finding as blocking or as a warning.
type LabelSeverity string

// Scopes, forms, statuses and severities of the Linear label plan.
const (
	// ScopeTeam is a label of the connection's team.
	ScopeTeam LabelScope = "team"
	// ScopeWorkspace is a workspace-level label.
	ScopeWorkspace LabelScope = "workspace"
	// FormGrouped is a label inside a label group.
	FormGrouped LabelForm = "grouped"
	// FormFlat is a label outside any group.
	FormFlat LabelForm = "flat"

	// StatusStored means a stored ID was found and is used.
	StatusStored LinearLabelStatus = "stored"
	// StatusAdopted means exactly one existing label matched by name.
	StatusAdopted LinearLabelStatus = "adopted"
	// StatusCreate means setup would create the label.
	StatusCreate LinearLabelStatus = "would create"
	// StatusAmbiguous means several labels match and none is stored.
	StatusAmbiguous LinearLabelStatus = "ambiguous"
	// StatusStale means the stored ID was not found in Linear.
	StatusStale LinearLabelStatus = "stored ID not found"
	// StatusMissing means the label is missing and setup cannot create it.
	StatusMissing LinearLabelStatus = "missing"

	// SeverityBlocking stops the whole setup run before any write.
	SeverityBlocking LabelSeverity = "blocking"
	// SeverityWarning is reported only.
	SeverityWarning LabelSeverity = "warning"
)

// LinearCandidate is one existing label that could be a wanted label.
type LinearCandidate struct {
	ID, Name, Group string
	Scope           LabelScope
	Form            LabelForm
}

// LinearLabelEntry is the plan for one wanted label.
type LinearLabelEntry struct {
	Wanted     agent.WantedLabel
	Status     LinearLabelStatus
	Chosen     *LinearCandidate
	Candidates []LinearCandidate // the ambiguity list, or name matches for a stale stored ID
	Mismatch   []LinearCandidate // other name matches beside a stored ID
	Severity   LabelSeverity
	Reason     string
	Create     *LinearCreate
}

// LinearCreate is one planned create in the connection's team.
type LinearCreate struct {
	Name, Color, Description string
	Family                   agent.LabelFamily
	Wanted                   string // the wanted label name; empty for a group
	IsGroup                  bool
	ParentID                 string // an existing group
	ParentPlaceholder        string // a group created earlier in the same run
	Placeholder              string
	Form                     LabelForm
}

// LinearLabelPlan is the pure plan setup and the connection check share.
type LinearLabelPlan struct {
	Entries []LinearLabelEntry
	Creates []LinearCreate
	// Modes, Agents and Bug are the maps setup would save; planned creates
	// appear as their placeholder IDs.
	Modes, Agents map[string]string
	Bug           string
	// VerifyIDs are every Mode ID plus the optional IDs this run adds.
	VerifyIDs  []string
	Applicable bool
	Changed    bool
}

const placeholderPrefix = "pending-create:"

var familyGroup = map[agent.LabelFamily]string{agent.LabelFamilyMode: "Mode", agent.LabelFamilyAgent: "Coding Agent"}

// PlanLinearLabels resolves every wanted label against the labels found in the
// team and workspace and the stored maps. It never plans a change to an
// existing label and never picks one of several matches.
func PlanLinearLabels(wanted []agent.WantedLabel, found []linear.SetupLabel, modes, agents map[string]string, bug string) LinearLabelPlan {
	plan := LinearLabelPlan{Modes: maps.Clone(modes), Agents: maps.Clone(agents), Bug: bug, Applicable: true}
	if plan.Modes == nil {
		plan.Modes = map[string]string{}
	}
	if plan.Agents == nil {
		plan.Agents = map[string]string{}
	}
	byID := map[string]linear.SetupLabel{}
	for _, l := range found {
		byID[l.ID] = l
	}
	groupCreated := map[agent.LabelFamily]string{}
	var added []string
	for _, w := range wanted {
		e := LinearLabelEntry{Wanted: w}
		storedID := storedLabelID(w, modes, agents, bug)
		names := nameCandidates(w, found)
		switch {
		case storedID != "":
			l, ok := byID[storedID]
			if !ok || l.IsGroup {
				e.Status, e.Candidates = StatusStale, names
				e.Severity, e.Reason = severityFor(w), "stored ID "+storedID+" not found in the team or workspace; store the right ID with `litefactory repo connection set`"
				break
			}
			c := candidate(l)
			e.Status, e.Chosen = StatusStored, &c
			for _, n := range names {
				if n.ID != storedID {
					e.Mismatch = append(e.Mismatch, n)
				}
			}
			if w.Family == agent.LabelFamilyBug {
				readerCheck(&e)
			}
		case len(names) == 1:
			e.Status, e.Chosen = StatusAdopted, &names[0]
			readerCheck(&e)
			if e.Severity != SeverityBlocking {
				id := names[0].ID
				storeID(&plan, w, id)
				added = append(added, id)
			}
		case len(names) > 1:
			e.Status, e.Candidates = StatusAmbiguous, names
			e.Severity, e.Reason = SeverityBlocking, "several labels match; store the wanted ID with `litefactory repo connection set`"
		default:
			planCreate(&plan, &e, found, groupCreated)
			if e.Create != nil {
				storeID(&plan, w, e.Create.Placeholder)
				added = append(added, e.Create.Placeholder)
			}
		}
		if e.Severity == SeverityBlocking {
			plan.Applicable = false
		}
		plan.Entries = append(plan.Entries, e)
	}
	for _, key := range slices.Sorted(maps.Keys(plan.Modes)) {
		plan.VerifyIDs = append(plan.VerifyIDs, plan.Modes[key])
	}
	for _, id := range added {
		if !slices.Contains(plan.VerifyIDs, id) {
			plan.VerifyIDs = append(plan.VerifyIDs, id)
		}
	}
	plan.Changed = len(plan.Creates) > 0 || !maps.Equal(plan.Modes, modes) || !maps.Equal(plan.Agents, agents) || plan.Bug != bug
	return plan
}

// IsPlaceholder says whether an ID stands for a label not created yet.
func IsPlaceholder(id string) bool { return strings.HasPrefix(id, placeholderPrefix) }

// Candidate builds the configuration setup would save from the current one.
func (p LinearLabelPlan) Candidate(current store.LinearConfiguration) (store.LinearConfiguration, error) {
	return p.CandidateWith(current, nil)
}

// CandidateWith replaces placeholders with created IDs; unknown placeholders stay.
func (p LinearLabelPlan) CandidateWith(current store.LinearConfiguration, created map[string]string) (store.LinearConfiguration, error) {
	resolve := func(id string) string {
		if createdID, ok := created[id]; ok {
			return createdID
		}
		return id
	}
	out := current
	encode := func(m map[string]string) (string, error) {
		if len(m) == 0 {
			return "", nil
		}
		resolved := map[string]string{}
		for k, v := range m {
			resolved[k] = resolve(v)
		}
		return marshalMapping(resolved)
	}
	var err error
	if out.ModeLabelsJSON, err = encode(p.Modes); err != nil {
		return out, err
	}
	if out.CodingAgentLabelsJSON, err = encode(p.Agents); err != nil {
		return out, err
	}
	out.BugLabelID = resolve(p.Bug)
	return out, nil
}

func storedLabelID(w agent.WantedLabel, modes, agents map[string]string, bug string) string {
	switch w.Family {
	case agent.LabelFamilyMode:
		for _, key := range slices.Sorted(maps.Keys(modes)) {
			if linear.CanonicalModeLabel(key) == w.Name {
				return modes[key]
			}
		}
	case agent.LabelFamilyAgent:
		if kind, ok := linear.LabelToKind(w.Name); ok {
			if key, ok := linear.KeyForKind(agents, kind); ok {
				return agents[key]
			}
		}
	case agent.LabelFamilyBug:
		return bug
	}
	return ""
}

func storeID(plan *LinearLabelPlan, w agent.WantedLabel, id string) {
	switch w.Family {
	case agent.LabelFamilyMode:
		if m, ok := agent.ParseModeLabel(w.Name); ok {
			plan.Modes[string(m)] = id
		}
	case agent.LabelFamilyAgent:
		plan.Agents[strings.TrimPrefix(w.Name, agent.CodingAgentLabelPrefix)] = id
	case agent.LabelFamilyBug:
		plan.Bug = id
	}
}

func candidate(l linear.SetupLabel) LinearCandidate {
	c := LinearCandidate{ID: l.ID, Name: l.Name, Group: l.ParentName, Scope: ScopeTeam, Form: FormFlat}
	if l.TeamID == "" {
		c.Scope = ScopeWorkspace
	}
	if l.ParentID != "" {
		c.Form = FormGrouped
	}
	return c
}

// nameCandidates lists every non-group label that fits a wanted label by name, ignoring case.
func nameCandidates(w agent.WantedLabel, found []linear.SetupLabel) []LinearCandidate {
	var out []LinearCandidate
	group, value, _ := strings.Cut(w.Name, ": ")
	for _, l := range found {
		if l.IsGroup {
			continue
		}
		var fits bool
		switch {
		case w.Family == agent.LabelFamilyBug:
			fits = strings.EqualFold(l.Name, agent.BugLabel)
		case l.ParentID == "":
			fits = strings.EqualFold(l.Name, w.Name)
		default:
			fits = strings.EqualFold(l.ParentName, group) && strings.EqualFold(l.Name, value)
		}
		if fits {
			out = append(out, candidate(l))
		}
	}
	return out
}

// ReadName is how the factory's reader reads a label without ID maps.
func (c LinearCandidate) ReadName() string {
	if c.Form == FormGrouped && (c.Group == "Mode" || c.Group == "Coding Agent") {
		return c.Group + ": " + c.Name
	}
	return c.Name
}

func readerCheck(e *LinearLabelEntry) {
	if ok, reason := agent.ReaderAcceptsLabel(e.Wanted, e.Chosen.ReadName()); !ok {
		e.Severity, e.Reason = severityFor(e.Wanted), "found as \""+e.Chosen.ReadName()+"\", which the factory cannot read: "+reason+"; setup does not rename labels"
	}
}

func severityFor(w agent.WantedLabel) LabelSeverity {
	if w.Family == agent.LabelFamilyMode {
		return SeverityBlocking
	}
	return SeverityWarning
}

// planCreate decides where a missing label goes: under the one team group of
// its family, nowhere when only a workspace group exists, flat when flat labels
// of the family exist, otherwise under a new team group.
func planCreate(plan *LinearLabelPlan, e *LinearLabelEntry, found []linear.SetupLabel, groupCreated map[agent.LabelFamily]string) {
	w := e.Wanted
	c := LinearCreate{Name: w.Name, Color: w.Color, Description: w.Description, Family: w.Family, Wanted: w.Name, Placeholder: placeholderPrefix + w.Name, Form: FormFlat}
	groupName, isGroupFamily := familyGroup[w.Family]
	if isGroupFamily {
		var teamGroups, workspaceGroups []LinearCandidate
		flat := false
		for _, l := range found {
			switch {
			case l.IsGroup && strings.EqualFold(l.Name, groupName):
				g := candidate(l)
				g.Form = FormGrouped
				if l.TeamID != "" {
					teamGroups = append(teamGroups, g)
				} else {
					workspaceGroups = append(workspaceGroups, g)
				}
			case !l.IsGroup && l.ParentID == "" && len(l.Name) > len(groupName)+2 && strings.EqualFold(l.Name[:len(groupName)+2], groupName+": "):
				flat = true
			}
		}
		_, value, _ := strings.Cut(w.Name, ": ")
		switch {
		case len(teamGroups) > 1:
			e.Status, e.Candidates = StatusAmbiguous, teamGroups
			e.Severity, e.Reason = SeverityBlocking, "several \""+groupName+"\" groups exist in the team; setup cannot choose where to create it"
			return
		case len(teamGroups) == 1:
			c.Name, c.ParentID, c.Form = value, teamGroups[0].ID, FormGrouped
		case len(workspaceGroups) > 0:
			e.Status, e.Candidates = StatusMissing, workspaceGroups
			e.Severity, e.Reason = severityFor(w), "the \""+groupName+"\" group exists only at workspace level; add it to the workspace group or store an ID"
			return
		case flat:
		default:
			if groupCreated[w.Family] == "" {
				g := LinearCreate{Name: groupName, Color: w.Color, Description: "Litefactory " + strings.ToLower(groupName) + " labels", Family: w.Family, IsGroup: true, Placeholder: placeholderPrefix + "group:" + groupName, Form: FormGrouped}
				groupCreated[w.Family] = g.Placeholder
				plan.Creates = append(plan.Creates, g)
			}
			c.Name, c.ParentPlaceholder, c.Form = value, groupCreated[w.Family], FormGrouped
		}
	}
	e.Status, e.Create = StatusCreate, &c
	plan.Creates = append(plan.Creates, c)
}

func marshalMapping(m map[string]string) (string, error) {
	raw, err := json.Marshal(m)
	return string(raw), err
}
