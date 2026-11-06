package store

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// LinearConfiguration is immutable, verified mapping evidence. JSON strings
// keep ConnectionVersion comparable and retain exactly the operator's mapping.
// No credentials belong here; authentication kind selects transport syntax.
type LinearConfiguration struct {
	WorkspaceID, TeamID, ProjectID string
	AuthKind                       string
	ModeLabelsJSON                 string
	LifecycleStatesJSON            string
	// CodingAgentLabelsJSON maps agent names to label IDs; empty means not mapped.
	CodingAgentLabelsJSON string
	// BugLabelID is the optional bug label ID; empty means not mapped.
	BugLabelID string
}

// ModeLabels returns the configured mode-to-label mapping; an absent map is
// empty ("labels not set up").
func (c LinearConfiguration) ModeLabels() (map[string]string, error) {
	return optionalLinearMapping(c.ModeLabelsJSON)
}

// CodingAgentLabels returns the configured agent-name-to-label mapping, empty when not mapped.
func (c LinearConfiguration) CodingAgentLabels() (map[string]string, error) {
	return optionalLinearMapping(c.CodingAgentLabelsJSON)
}

// BugLabel returns the configured bug label ID, empty when not mapped.
func (c LinearConfiguration) BugLabel() (string, error) {
	if c.BugLabelID != "" && !validLinearIdentity(c.BugLabelID) {
		return "", errors.New("store: invalid Linear mapping identity")
	}
	return c.BugLabelID, nil
}

// NormalizeEmptyLinearMapping returns the canonical empty form ("") for an empty JSON object.
func NormalizeEmptyLinearMapping(raw string) string {
	if strings.TrimSpace(raw) == "{}" {
		return ""
	}
	return raw
}

func optionalLinearMapping(raw string) (map[string]string, error) {
	if raw == "" {
		return map[string]string{}, nil
	}
	return linearMapping(raw)
}

func validLinearIdentity(v string) bool {
	return strings.TrimSpace(v) == v && v != "" && !strings.ContainsAny(v, "\r\n\x00")
}

func validAgentName(k string) bool {
	if k == "" || len(k) > 64 {
		return false
	}
	for _, r := range k {
		if r < 0x20 || r == 0x7f || r == ':' {
			return false
		}
	}
	return true
}

// LifecycleStates returns the explicitly configured lifecycle-to-state mapping.
func (c LinearConfiguration) LifecycleStates() (map[string]string, error) {
	return linearMapping(c.LifecycleStatesJSON)
}

func linearMapping(raw string) (map[string]string, error) {
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil || len(out) == 0 || len(out) > 32 {
		return nil, errors.New("store: invalid Linear mapping")
	}
	for key, value := range out {
		if strings.TrimSpace(key) != key || key == "" || strings.TrimSpace(value) != value || value == "" || strings.ContainsAny(key+value, "\r\n\x00") {
			return nil, errors.New("store: invalid Linear mapping identity")
		}
	}
	return out, nil
}

func validateLinearConnection(v ConnectionVersion) error {
	if v.Provider != ticket.Provider("linear") {
		if v.Linear != (LinearConfiguration{}) {
			return errors.New("store: Linear configuration requires a Linear ticket connection")
		}
		return nil
	}
	c := v.Linear
	if c.WorkspaceID == "" || c.TeamID == "" || c.ProjectID == "" || v.Destination != c.TeamID+"/"+c.ProjectID || strings.ContainsAny(c.WorkspaceID+c.TeamID+c.ProjectID, " \r\n\t\x00") || (c.AuthKind != "api_key" && c.AuthKind != "oauth") {
		return errors.New("store: Linear workspace, team, project and authentication kind are required")
	}
	modes, err := c.ModeLabels()
	if err != nil {
		return err
	}
	agents, err := c.CodingAgentLabels()
	if err != nil {
		return err
	}
	for key := range agents {
		if !validAgentName(key) {
			return errors.New("store: invalid coding agent name")
		}
	}
	bug, err := c.BugLabel()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	ids := []string{bug}
	for _, m := range []map[string]string{modes, agents} {
		for _, id := range m {
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		if id == "" {
			continue
		}
		if seen[id] {
			return errors.New("store: a Linear label ID is mapped more than once")
		}
		seen[id] = true
	}
	states, err := c.LifecycleStates()
	if err != nil {
		return err
	}
	if states["completed"] == "" || states["retired"] == "" || states["completed"] == states["retired"] {
		return errors.New("store: Linear completion and retirement require distinct state mappings")
	}
	return nil
}
