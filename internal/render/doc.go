// Package render defines the pluggable per-harness renderer contract: one
// pure function per harness (Claude, Codex, opencode, and whatever comes
// after), registered under its harness name in a plain map, that turns one
// agent's harness-neutral markdown source into that harness's native output
// shape.
//
// # Input is a resolved source, not a merged config
//
// [ResolvedSource] is built from the markdown file an internal/team
// AgentEntry's Source names: a small structured [Frontmatter] block (YAML, at the top of
// the file) plus the markdown Body that follows it. It deliberately
// replaces the old three-layer merged config object — once the index model
// is pure (US-2), a per-agent structured field has to live somewhere, and
// the natural place is frontmatter on the file the index already points at,
// so there is one file to edit rather than two. Building a real
// ResolvedSource by reading and splitting an actual markdown file is not
// this package's job; see the note on [Frontmatter].
//
// # Renderers are pure
//
// A [Renderer] MUST NOT touch the filesystem, the network, or the system
// clock. It receives a [ResolvedSource] and returns the [RenderedFile]s it
// produces, or an error. Determinism obligations for every implementation:
// sort before iterating any map, never range a map inside a template,
// terminate every produced file with exactly one trailing newline, and never
// embed a timestamp, a version string, or a hostname in the output. These
// are what make running the same renderer over the same source twice, in
// two different processes, byte-identical.
//
// # Failing loudly instead of dropping silently
//
// A renderer that is given a frontmatter field it cannot express in its
// target harness's format MUST return an [*UnsupportedFieldError] (or an
// error wrapping it) rather than silently omitting the field. The caller —
// not this package — MUST discard every [RenderedFile] a renderer produced
// once that renderer returns any error: there is no partial write.
//
// # The registry is a plain map, on purpose
//
// [Register] and [Lookup] work against one unexported map keyed by harness
// name. Adding a fourth harness is calling [Register] from that harness's
// own package's init — no other package's source changes. This is what
// makes the extensibility acceptance criterion (a fake harness passing the
// same contract test suite as the three real ones) true by construction
// rather than by convention.
package render
