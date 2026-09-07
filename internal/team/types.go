package team

import "regexp"

// KeyPattern is the filename-safety rule every agent key and team name must
// satisfy: lowercase alphanumerics and interior hyphens, 1-64 characters,
// never leading or trailing punctuation. Keys and names decoded from the
// index end up as path segments once a team is composed and materialized,
// so this is checked at load time rather than left to fail later as a bad
// path.
var KeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// AgentEntry is one agent definition: where it is read from, and where it
// is materialized to for a harness to consume. It appears both inline under
// a team's Agents map and top-level under Index.Shared.
type AgentEntry struct {
	Source string `json:"source"`
	Dest   string `json:"dest"`
}

// TeamEntry is one team: its name, used as its slug in paths and in error
// messages, and the agents owned by that team alone (as opposed to
// Index.Shared, which every team can draw on).
type TeamEntry struct {
	Name   string                `json:"name"`
	Agents map[string]AgentEntry `json:"agents"`
}

// Index is the decoded and validated agent team index: every team, plus the
// agents shared across all of them.
type Index struct {
	Teams  []TeamEntry           `json:"teams"`
	Shared map[string]AgentEntry `json:"shared"`
}
