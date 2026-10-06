package render

// Frontmatter is the structured YAML header of a built-in role.
type Frontmatter struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Tools       []string       `yaml:"tools,omitempty"`
	Model       string         `yaml:"model,omitempty"`
	Extra       map[string]any `yaml:",inline"`
}

// ResolvedSource holds parsed role metadata and the verbatim markdown body.
type ResolvedSource struct {
	Frontmatter Frontmatter
	Body        string
}

// RenderedFile holds the complete immutable output of native rendering.
type RenderedFile struct {
	Path  string
	Bytes []byte
}
