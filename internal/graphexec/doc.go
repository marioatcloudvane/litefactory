// Package graphexec parses, validates and navigates workflow profiles without
// clocks, persistence, filesystem access or execution. Gonum's DOT AST is
// contained in dot.go; all other code uses the package's own values.
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
// lf_agent_markdown and lf_model_tier are the only action-specific reserved
// attributes. A third such attribute requires a declarative ActionSpec schema
// instead of another special case. Markdown references are syntax checked only;
// catalog-root resolution and actual agent invocation belong to later work.
package graphexec
