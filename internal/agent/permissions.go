package agent

// Permissions is what the resolved mode's tool profile grants one run, carried across
// the [Runner] boundary with no tool-vocabulary field of its own.
//
// Profile is the canonical workflow profile selected from the resolved mode,
// carried here for logging and metadata only. It is never forge text.
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
// The zero value means no mode profile was supplied to this runner. Every
// implementation MUST treat that as "use my own configured default", never
// as "grant nothing" — an empty surface and an omitted one are not the same
// thing to at least one implementation (internal/harness's own --tools
// boundary: an omitted --tools is allow-all).
type Permissions struct {
	// Profile is the mode-selected profile name, or "" when none was supplied.
	Profile string

	// Payload is the implementation-specific grant a profile resolved to.
	// nil means Profile is also "" — see the zero-value note above.
	Payload any
}
