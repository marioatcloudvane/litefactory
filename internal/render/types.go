package render

// Frontmatter is the small structured header parsed from the top of an
// agent's markdown source file — the file an internal/team AgentEntry's
// Source names — delimited by a leading and trailing "---" line, YAML in
// between. Name, Description, Tools and Model are the fields a renderer is
// expected to understand; Extra holds anything else a harness-specific
// renderer knows how to read.
//
// yaml:",inline" on Extra means a frontmatter key that is not one of the
// four named fields lands here during decode rather than causing a strict
// decode to fail — one harness's private field (a permission object, say)
// does not force every other harness's frontmatter shape to expect it.
//
// Splitting a real markdown source file into a Frontmatter and a Body is
// not this task's job. It lands with whichever story first calls [Renderer]
// (likely US-9, which resolves one AgentEntry against
// internal/teamsource.Resolved's index and markdown FS). No caller in the
// tree constructs a Frontmatter from a real file yet.
type Frontmatter struct {
	// Name is the agent's own declared name. It is expected to match the
	// key the agent is registered under in internal/team's Index (a team's
	// Agents map, or Index.Shared) — a renderer error names this value as
	// "the agent key" (see [UnsupportedFieldError]).
	Name string `yaml:"name"`
	// Description is the one-line summary of what the agent does.
	Description string `yaml:"description"`
	// Tools is the agent's requested tool list, in the vendor-neutral shape
	// every harness is expected to be able to translate from — though not
	// every harness can express every entry in it (see [UnsupportedFieldError]).
	Tools []string `yaml:"tools,omitempty"`
	// Model is the vendor-neutral capability tier or model name the agent
	// asks to run on.
	Model string `yaml:"model,omitempty"`
	// Extra holds any frontmatter key beyond the four named above. A
	// harness-specific renderer that needs one of these MUST fail loudly
	// (see [UnsupportedFieldError]) rather than silently drop it, if it does
	// not know how to express it in its own output format.
	Extra map[string]any `yaml:",inline"`
}

// ResolvedSource is one agent's fully-resolved markdown source: the
// frontmatter block plus the body content that follows it, ready for a
// [Renderer]. It is built from the markdown file an AgentEntry.Source
// names — internal/teamsource.Resolved's Index plus the FS it is rooted
// at — never from a merged, three-layer config object; see the package doc.
type ResolvedSource struct {
	// Frontmatter is the decoded YAML header.
	Frontmatter Frontmatter
	// Body is the markdown prompt content that follows the frontmatter
	// block, verbatim.
	Body string
}

// RenderedFile is one file — or, per US-18, one marker-tagged section of a
// shared root file — a [Renderer] produced.
//
// Bytes is the renderer's complete output for Path, computed once and
// returned as a plain, immutable []byte: a Renderer MUST NOT retain a
// reference it mutates after returning, which is what keeps two calls to
// the same renderer over the same [ResolvedSource], in two different
// processes, byte-identical. This is deliberately not the lockfile's
// Artifact type (US-10): a renderer does not compute a hash or classify its
// own output — the caller does both, once every renderer in a run has
// succeeded.
//
// Path names one of two things, and this package does not distinguish
// between them or validate which one a given Path is — that is US-18's own
// scope:
//   - a standalone destination file, matching AgentEntry.Dest's shape; or
//   - a target root file (AGENTS.md, CLAUDE.md) plus a marker-tagged
//     section identity, per US-18's root-file composition convention.
type RenderedFile struct {
	Path  string
	Bytes []byte
}
