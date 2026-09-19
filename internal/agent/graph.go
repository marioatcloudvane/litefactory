package agent

// InvocationKind distinguishes a private graph pass from whole-turn work.
// Only the factory sets this value; it does not widen host permissions.
type InvocationKind string

// InvocationResearchNode identifies a private Research pass for result retention.
const InvocationResearchNode InvocationKind = "research_node"

// GraphInvocation is the factory attribution for a reserved node attempt.
// The run's persisted segment supplies the provider conversation identity.
type GraphInvocation struct {
	InstanceID int64
	AttemptID  int64
	RunID      int64
	Node       string
}
