package render

// Renderer turns one agent's [ResolvedSource] into the [RenderedFile]s a
// harness needs to consume that agent. Implementations MUST be pure: no
// filesystem, network or clock access, so that the caller — not this
// package — owns retrying, hashing and writing. See the package doc for the
// determinism obligations every implementation carries, and
// [UnsupportedFieldError] for how an implementation must fail when it is
// asked to express a frontmatter field its target format has no room for.
type Renderer interface {
	Render(source ResolvedSource) ([]RenderedFile, error)
}

// RendererFunc adapts a plain function to the [Renderer] interface, the
// same pattern as http.HandlerFunc. Every renderer this contract expects is
// a pure function with no state of its own, so most implementations need
// nothing more than this — a named type implementing Renderer directly is
// only worth it if a renderer genuinely needs fields (a lookup table it
// closes over, say).
type RendererFunc func(source ResolvedSource) ([]RenderedFile, error)

// Render calls f.
func (f RendererFunc) Render(source ResolvedSource) ([]RenderedFile, error) {
	return f(source)
}
