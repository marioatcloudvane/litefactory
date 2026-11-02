// Package graphexec parses, validates and navigates workflow profiles without
// clocks, persistence, filesystem access or execution. Neutral DOT mechanics and
// structural checks live in pkg/execution/graph; factory policy and identities
// remain here.
//
// Profiles and registries are built, validated, then frozen by their owner before
// sharing. Exported slices and maps are construction values, not concurrency-safe
// mutation APIs. Lookup boundaries return copies of mutable values.
//
// Cycles are legal. Every cycle member needs a positive lf_limit: the maximum
// dispatches of that node per instance, counting both retries and healthy loop
// re-entry. The execution layer enforces these bounds; Decide only navigates.
//
// Profile version 1 uses graph attributes team, mode, profile_version and start;
// node attributes lf_action, lf_outcomes (comma-separated), lf_limit and
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
