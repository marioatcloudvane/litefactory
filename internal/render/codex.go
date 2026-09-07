package render

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// init registers this file's Renderer under harness key "codex", per
// registry.go's documented convention: one call to [Register] from the
// implementing package's own init, before any concurrent Render/Lookup
// traffic starts.
func init() {
	Register("codex", RendererFunc(renderCodex))
}

// codexFrontmatter is the YAML header written at the top of a Codex
// SKILL.md file. Field order here is deliberately the struct's declaration
// order — yaml.Marshal walks struct fields in that order rather than
// sorting them (unlike a map, which this package would have to sort before
// ranging per the determinism obligation in doc.go) — so the emitted header
// is always "name" then "description", every time, in every process.
type codexFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// renderCodex implements [Renderer] for harness key "codex".
//
// # Output shape
//
// Codex reads agent-like definitions as skill directories rather than a
// flat per-agent file. This renderer emits exactly one standalone
// [RenderedFile] per call, at ".codex/skills/<agent>/SKILL.md" where
// <agent> is Frontmatter.Name, containing a YAML frontmatter block (name,
// description) followed by a blank line and the source Body verbatim. This
// mirrors the same "---\nyaml\n---\n\nbody" shape ResolvedSource itself was
// parsed from, since Codex's own skill format expects a frontmatter-style
// header. Codex's skill-directory shape has no equivalent to US-18's
// marker-tagged shared-root-file section, so this renderer only ever
// produces standalone files — a permanent asymmetry with any renderer that
// does participate in that shape, not a gap in this one.
//
// # Field mapping
//
// Only Name and Description have a Codex equivalent (the frontmatter's
// "name" and "description" keys). Model and Tools are Claude-shaped
// concepts — Codex's skill format has no per-skill model pin and no
// tool-allowlist concept — so a non-empty Model or Tools fails loudly with
// an [*UnsupportedFieldError] naming the dotted field ("model" or "tools")
// rather than being silently dropped, per AC-2. Any Frontmatter.Extra key
// is, by definition, a field this package does not know the meaning of, so
// it is rejected the same way, naming the offending key; when more than one
// Extra key is present, the lexicographically first name is reported, so
// two calls over the same source report the same field every time.
func renderCodex(source ResolvedSource) ([]RenderedFile, error) {
	fm := source.Frontmatter

	if fm.Name == "" {
		return nil, fmt.Errorf("codex: %w", ErrMissingAgentName)
	}
	if fm.Model != "" {
		return nil, &UnsupportedFieldError{Agent: fm.Name, Harness: "codex", Field: "model"}
	}
	if len(fm.Tools) > 0 {
		return nil, &UnsupportedFieldError{Agent: fm.Name, Harness: "codex", Field: "tools"}
	}
	if len(fm.Extra) > 0 {
		keys := make([]string, 0, len(fm.Extra))
		for k := range fm.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, &UnsupportedFieldError{Agent: fm.Name, Harness: "codex", Field: keys[0]}
	}

	header, err := yaml.Marshal(codexFrontmatter{Name: fm.Name, Description: fm.Description})
	if err != nil {
		return nil, fmt.Errorf("codex: marshal frontmatter for agent %q: %w", fm.Name, err)
	}

	var buf bytes.Buffer
	buf.WriteString("---\n")
	buf.Write(header)
	buf.WriteString("---\n")
	if body := strings.TrimRight(source.Body, "\n"); body != "" {
		buf.WriteString("\n")
		buf.WriteString(body)
		buf.WriteString("\n")
	}

	path := fmt.Sprintf(".codex/skills/%s/SKILL.md", fm.Name)
	return []RenderedFile{{Path: path, Bytes: buf.Bytes()}}, nil
}
