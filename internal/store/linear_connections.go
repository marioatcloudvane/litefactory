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
}

// ModeLabels returns the explicitly configured mode-to-label mapping.
func (c LinearConfiguration) ModeLabels() (map[string]string, error) {
	return linearMapping(c.ModeLabelsJSON)
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
	if _, err := c.ModeLabels(); err != nil {
		return err
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
