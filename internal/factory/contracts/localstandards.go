package contracts

// StandardsCategoryChange identifies one amendment to an existing proposal.
type StandardsCategoryChange struct {
	Proposal                      string
	Expected                      int64
	Action, ID, Name, Description string
}

// StandardsTaskRun selects the existing task's newest or numbered run.
type StandardsTaskRun struct {
	Issue, Ordinal    int
	Owner, Repository string
}

// StandardsTaskLookupError preserves lookup error rendering separately from run selection.
type StandardsTaskLookupError struct{ Err error }

// Error returns the underlying task lookup diagnostic.
func (e *StandardsTaskLookupError) Error() string { return e.Err.Error() }

// Unwrap preserves the existing lookup error for CLI exit mapping.
func (e *StandardsTaskLookupError) Unwrap() error { return e.Err }

// StandardsAuthorStart is the local authoring intent and registered project selector.
type StandardsAuthorStart struct {
	ID, TargetID, Intent                    string
	Categories                              []string
	ProjectForge, ProjectOwner, ProjectName string
}
