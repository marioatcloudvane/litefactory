package team

import "regexp"

// KeyPattern restricts registry identifiers to safe, bounded path segments.
var KeyPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$`)

// AgentEntry is either a shared reference or a complete inline definition.
type AgentEntry struct {
	Ref    string   `json:"ref,omitempty"`
	ID     string   `json:"id,omitempty"`
	Desc   string   `json:"desc,omitempty"`
	Path   string   `json:"path,omitempty"`
	Tier   string   `json:"tier,omitempty"`
	Skills []string `json:"skills,omitempty"`
}

// Entry declares a team's description and explicit ordered mode rosters.
type Entry struct {
	Desc      string       `json:"desc"`
	Research  []AgentEntry `json:"research"`
	Plan      []AgentEntry `json:"plan"`
	Implement []AgentEntry `json:"implement"`
	Validate  []AgentEntry `json:"validate"`
	Spike     []AgentEntry `json:"spike"`
}

// Index is the versioned central catalog of teams and shared definitions.
type Index struct {
	SchemaVersion int                   `json:"schema_version"`
	Teams         map[string]Entry      `json:"teams"`
	Shared        map[string]AgentEntry `json:"shared"`
}

// Modes returns a fresh slice of supported roster names in workflow order.
func Modes() []string { return []string{"research", "plan", "implement", "validate", "spike"} }

// Mode returns the declared roster and whether the requested mode is known.
func (t Entry) Mode(m string) ([]AgentEntry, bool) {
	switch m {
	case "research":
		return t.Research, true
	case "plan":
		return t.Plan, true
	case "implement":
		return t.Implement, true
	case "validate":
		return t.Validate, true
	case "spike":
		return t.Spike, true
	default:
		return nil, false
	}
}
