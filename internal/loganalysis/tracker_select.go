package loganalysis

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
)

// TicketTracker is the one rule that picks the issue tracker of a project's
// ticket connection: a connection that loaded a Linear client searches that
// Linear project; otherwise one that loaded a GitHub client searches the
// owner/name destination. It reports false when the connection has neither.
func TicketTracker(linearClient *linear.Client, code *github.Client, destination string) (Tracker, bool) {
	switch {
	case linearClient != nil:
		return LinearTracker{Client: linearClient}, true
	case code != nil:
		owner, name, _ := strings.Cut(destination, "/")
		return GitHubTracker{Client: code, Repo: github.Repo{Owner: owner, Name: name}}, true
	}
	return nil, false
}
