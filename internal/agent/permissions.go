package agent

// Permissions is what a resolved tool profile grants one run, carried across
// the [Runner] boundary with no tool-vocabulary field of its own.
//
// Profile is the operator-authored profile name that selected it — already
// validated against the operator's own configuration, and carried here for
// logging and metadata only. It is never text originating from a forge
// label; only the validated lookup key is.
//
// Payload is deliberately opaque. What a profile actually grants differs by
// Kind — internal/harness's --tools and --allowedTools lists, internal/codex's
// sandbox mode — and this package must not know either shape: that is
// precisely the mistake [Permissions.ClaudeTools] / [Permissions.CodexSandbox]
// would have been, had they shipped. Only the [Runner] implementation a
// request is addressed to may narrow Payload into its own package-local
// type; nothing else in the tree may inspect it, and internal/turn passes it
// through unopened.
//
// The zero value means no profile was resolved for this run. Every
// implementation MUST treat that as "use my own configured default", never
// as "grant nothing" — an empty surface and an omitted one are not the same
// thing to at least one implementation (internal/harness's own --tools
// boundary: an omitted --tools is allow-all).
type Permissions struct {
	// Profile is the operator-authored profile name, or "" when none was
	// resolved for this run.
	Profile string

	// Payload is the implementation-specific grant a profile resolved to.
	// nil means Profile is also "" — see the zero-value note above.
	Payload any
}
