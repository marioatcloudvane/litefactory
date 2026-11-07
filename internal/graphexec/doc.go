// Package graphexec parses, validates and navigates workflow profiles without
// clocks, persistence, filesystem access or execution. Neutral DOT mechanics and
// structural checks live in pkg/execution/graph; factory policy and identities
// remain here.
//
// Profiles and registries are built, validated, then frozen by their owner before
// sharing. Exported slices and maps are construction values, not concurrency-safe
// mutation APIs. Lookup boundaries return copies of mutable values.
//
// Cycles are legal. One step budget per instance (graph attribute
// lf_step_budget, default DefaultStepBudget) caps node dispatches since the
// last accepted owner reply; the execution layer enforces it and parks the
// instance for the owner when it runs out. Decide only navigates.
//
// Validation is structural: every action is known, every declared outcome has
// its edge, every node reaches a terminal from the start, and attributes parse.
// Workflow shape belongs to the DOT, not to Go.
//
// Profile version 1 uses graph attributes team, mode, profile_version, start and
// lf_step_budget; node attributes lf_action, lf_outcomes (comma-separated) and
// lf_terminal; and edge attribute on (or lf_on). Node declarations must be
// explicit and unique. Defaults apply to subsequent declarations. Subgraphs and
// ports are rejected rather than partially interpreted. Arbitrary attributes are
// retained as data, never evaluated or templated.
//
// lf_agent_markdown and lf_model_tier retain foundation syntax validation.
// Other action-specific attributes use ActionSpec's declarative string schema.
// Research loop/if actions additionally validate safe question control flow;
// instruction resolution and invocation remain execution-layer responsibilities.
package graphexec
