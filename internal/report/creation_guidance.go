package report

// CreationPublication is descriptive workflow provenance, never permission to
// execute or bind consent. Unknown is the safe zero value for retained replies.
type CreationPublication uint8

// Creation publication contexts describe observed publishing capability.
const (
	CreationPublicationUnknown CreationPublication = iota
	CreationPublicationLegacy
	CreationPublicationGraph
	CreationPublicationUnavailable
	// CreationPublicationFactory means the factory publishes an approved
	// proposal itself, whatever mode or workflow path the task is on.
	CreationPublicationFactory
)

// CreationGuidance remains conditional because a retained reply can arrive after
// the proposal, workflow configuration or approval deadline has changed.
func CreationGuidance(publication CreationPublication) string {
	if publication == CreationPublicationFactory {
		return "Approving publishes these exact issues right away; no further comment or run is needed. Only the newest proposal on this issue can be approved, and a changed draft needs a new proposal."
	}
	text := "Approval alone starts no publication. Its 30-minute lifetime begins at the trusted approval-comment timestamp; an eligible publishing attempt must bind the approval before expiry.\n\n"
	switch publication {
	case CreationPublicationLegacy:
		text += "For an eligible legacy Research publishing attempt, post a further ordinary comment to start the attempt. This requires the proposal and publishing capability to remain eligible and does not guarantee publication."
	case CreationPublicationGraph:
		text += "Graph Research cannot publish this batch. Another ordinary comment does not enable publication in that workflow."
	case CreationPublicationUnavailable:
		text += "Publishing capability was unavailable when this guidance was prepared. Another ordinary comment does not establish publishing eligibility."
	default:
		text += "Publishing eligibility is unknown. This proposal does not establish that another ordinary comment can start an eligible publishing attempt."
	}
	return text + "\n\nTo renew expired approval, post a fresh approval comment. Reuse the retained proposal and its approval command only while it remains current and eligible; otherwise a new eligible proposal is required. Replaying the old approval comment does not renew consent."
}
