// Package bugrepair defines what a Bug fixer reports about its repair. It is
// a report for the owner, never check or delivery authority.
package bugrepair

import (
	"errors"
	"strings"
)

// Check is one command the fixer ran and how it ended.
type Check struct {
	Command string `json:"command"`
	Outcome string `json:"outcome"`
}

// Evidence is the fixer's plain account of the repair: what changed and which
// of the repository's own checks it ran. Git holds the change itself.
type Evidence struct {
	Summary string  `json:"summary"`
	Checks  []Check `json:"checks,omitempty"`
}

// Validate requires only a summary the owner can read.
func (e Evidence) Validate() error {
	if strings.TrimSpace(e.Summary) == "" {
		return errors.New("bug repair: summary is empty")
	}
	return nil
}
