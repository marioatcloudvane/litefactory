package team

// RenderedFile is one already-rendered artifact for one agent/harness pair,
// carrying only what Add needs: the target path a renderer produced (either
// an Owned standalone path, or one of this package's own root-file target
// literals, RootFileClaude/RootFileOpencode) and the bytes to place there.
//
// This is this package's own copy of render.RenderedFile's shape rather
// than that type itself: internal/team may only ever import internal/config
// (see doc.go's Invariants), so it cannot depend on internal/render.
// internal/teamsource, which does import both, is what converts a
// render.RenderedFile into this type before calling Add.
type RenderedFile struct {
	Path  string
	Bytes []byte
}

// AgentRender is the write engine's input for one (agent, harness) pair:
// everything Add needs to place its rendered files, scan them for literal
// secrets, and record their provenance in the lockfile.
//
// Teams lists every team that contributes this agent for this harness. For
// an agent that lives in Index.Shared and is drawn on by more than one
// team, the caller (internal/teamsource) collects every contributing team
// into this one slice before calling Add, so a shared agent's root-file
// section is composed once, listing every contributing team, rather than
// once per team (US-18's dedup rule). For an agent owned by exactly one
// team, Teams has exactly that one entry.
type AgentRender struct {
	// AgentKey is the agent's key, matching KeyPattern.
	AgentKey string
	// Harness is the renderer harness key ("claude", "codex", "opencode")
	// that produced Files.
	Harness string
	// Teams is every team contributing this agent for this harness,
	// deduplicated. Order does not matter: markerSpan sorts before joining.
	Teams []string
	// SourcePath is the AgentEntry.Source that was rendered.
	SourcePath string
	// SourceHash is a hex sha256 of the source markdown bytes that were
	// rendered, computed by the caller before invoking a renderer.
	SourceHash string
	// Files is every RenderedFile a renderer produced for this agent and
	// harness — today, always a standalone file plus a root-file section,
	// per internal/render's claude and opencode renderers.
	Files []RenderedFile
}

// AddResult reports what one Add call actually did.
type AddResult struct {
	// Written lists the lockfile records for files this call actually wrote
	// (created or changed on disk).
	Written []LockRecord
	// Skipped lists the lockfile records left untouched because a fresh
	// render matched the existing record's SourceHash and RenderedHash
	// exactly — Add's only kind of no-op.
	Skipped []LockRecord
}
