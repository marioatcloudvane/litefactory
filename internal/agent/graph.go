package agent

// InvocationKind distinguishes a private graph pass from whole-turn work.
// Only the factory sets this value; it does not widen host permissions.
type InvocationKind string

// InvocationGraphNode identifies a private graph pass for result retention.
const InvocationGraphNode InvocationKind = "graph_node"

// InvocationResearchNode is the Research adapter spelling of a generic invocation.
const InvocationResearchNode = InvocationGraphNode

// GraphInvocation is the factory attribution for a reserved node attempt.
// The run's persisted segment supplies the provider conversation identity.
type GraphInvocation struct {
	InstanceID int64
	AttemptID  int64
	RunID      int64
	Node       string
}
