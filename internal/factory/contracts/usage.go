package contracts

import "time"

// UsageFilter is a bounded selection of retained measurements, not a database query.
type UsageFilter struct {
	RepoID, Issue, RunID     int64
	IncludeStories           bool
	CodingAgent, Mode, Model string
	JudgeAttemptID           string
	TimeFrom, TimeTo         *time.Time
}

// UsageRequest contains the server-authorized filter and resolved time bounds.
type UsageRequest struct {
	Repository string
	Filter     UsageFilter
	View       string
	By         []string
	Empty      bool
}

// UsageProjection is fully sanitized before it can enter the view cache.
type UsageProjection struct {
	Facts    map[string]any
	Items    []map[string]any
	Revision string
	Policy   string
}
