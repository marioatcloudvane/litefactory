// Package agent holds the vendor-neutral request, result and permission
// types that cross the boundary between internal/turn and whichever
// coding-agent tool a run is addressed to, plus the [Runner] interface every
// implementation satisfies.
//
// # What "vendor-neutral" means here, precisely
//
// Nothing exported by this package may name a vendor: no "Claude", no
// "Anthropic", no "Codex", no "OpenAI", anywhere in an identifier —
// including inside [Permissions]. That is not a style preference. The
// M0 harness shipped a field literally named ClaudeSessionID on the type
// that was supposed to be the vendor-neutral seam, and a later design review
// caught a follow-up spec repeating the exact same mistake with
// Permissions.ClaudeTools / Permissions.CodexSandbox fields. Both leaked the
// one thing a boundary like this exists to hide. [SessionRef] and
// [Permissions] are shaped the way they are — a litefactory-derived handle
// plus a tool-reported one, a profile name plus an opaque payload — because
// that is what is actually common to every coding-agent tool; anything more
// specific than that belongs one layer in, on the implementation that alone
// knows what to do with it.
//
// # Kind has no fixed vocabulary
//
// [Kind] is a bare string type with no exported constants in this package.
// Its validity is decided by whether a [Runner] is registered under it in
// the internal/turn.RunnerSet the daemon's factory built at startup, never
// by membership in a hardcoded switch here — see internal/turn's own doc for
// why that is what lets a third tool be an additive implementation rather
// than an edit to this package or to internal/report.
//
// # Where the concrete shapes live
//
// internal/harness is the Claude implementation of [Runner], and
// internal/codex is (once its wire shape is measured) the Codex one. Both
// import this package; this package imports neither of them, and imports
// nothing else from litefactory beyond internal/naming — see
// internal/agent/purity_test.go, which enforces both halves of that.
package agent
