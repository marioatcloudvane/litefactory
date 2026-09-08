package agent

import "slices"

// ApprovedStory is an immutable execution revision, not an editable story source.
type ApprovedStory struct {
	State        string   `json:"state"`
	Key          string   `json:"key"`
	IssueNumber  int      `json:"issue_number"`
	IssueID      int64    `json:"issue_id"`
	ParentID     int64    `json:"parent_id"`
	Source       string   `json:"source"`
	Title        string   `json:"title"`
	Body         string   `json:"body"`
	Revision     string   `json:"revision"`
	FetchedAt    string   `json:"fetched_at"`
	Dependencies []string `json:"dependencies"`
}

// ApprovedBundle repeats explicit scope even when a runner resumes history.
type ApprovedBundle struct {
	ParentRecovery        bool             `json:"parent_recovery,omitempty"`
	EventDirectory        string           `json:"event_directory,omitempty"`
	ParentTitle           string           `json:"parent_title"`
	ParentBody            string           `json:"parent_body"`
	CompletedDependencies []CompletedStory `json:"completed_dependencies,omitempty"`
	Parent                string           `json:"parent"`
	RunID                 int64            `json:"run_id"`
	RunOrdinal            int              `json:"run_ordinal"`
	Generation            int              `json:"generation"`
	Stories               []ApprovedStory  `json:"stories"`
}

// Clone isolates nested dependency slices across preparation and runner boundaries.
func (b *ApprovedBundle) Clone() *ApprovedBundle {
	if b == nil {
		return nil
	}
	c := *b
	c.Stories = slices.Clone(b.Stories)
	c.CompletedDependencies = slices.Clone(b.CompletedDependencies)
	for i := range c.Stories {
		c.Stories[i].Dependencies = slices.Clone(c.Stories[i].Dependencies)
	}
	return &c
}

// CompletedStory identifies satisfied dependencies retained across replanning.
type CompletedStory struct {
	Key         string `json:"key"`
	IssueNumber int    `json:"issue_number"`
	IssueID     int64  `json:"issue_id"`
}
