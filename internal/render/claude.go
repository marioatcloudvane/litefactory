package render

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// claudeHarness is this renderer's registry key.
const claudeHarness = "claude"

// claudeAgentsDir is the directory Claude reads its sub-agent definitions
// from, relative to a project's root. See the package doc's note on
// [RenderedFile].Path: this is the standalone-file shape of the two Path
// meanings that type documents.
const claudeAgentsDir = ".claude/agents"

// claudeRootFile is the second, marker-tagged-section [RenderedFile].Path
// this renderer emits alongside its standalone file, per US-18's root-file
// composition convention. internal/team recognizes this exact literal value
// to mean "wrap Bytes in a <!-- litefactory:team --> marker span and
// upsert it into the repo's CLAUDE.md", rather than writing Bytes to a file
// named literally "CLAUDE.md" in the current directory. This package does
// not itself understand markers or root-file upsert — see [RenderedFile]'s
// doc comment — it only produces the section content.
const claudeRootFile = "CLAUDE.md"

func init() {
	Register(claudeHarness, RendererFunc(renderClaude))
}

// claudeFrontmatter is the ordered subset of [Frontmatter] Claude's own
// sub-agent convention understands, marshalled as YAML. Field order here is
// fixed by struct declaration order (yaml.v3 does not reorder struct
// fields), which is what keeps two renders of the same [ResolvedSource]
// byte-identical.
type claudeFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Tools       string `yaml:"tools,omitempty"`
	Model       string `yaml:"model,omitempty"`
}

// renderClaude implements [Renderer] for harness key "claude", producing two
// [RenderedFile]s: the standalone file at ".claude/agents/<agent>.md"
// containing a YAML frontmatter block (name, description, tools as a
// comma-separated list, and model when set) followed by the markdown body,
// verbatim; and, per US-18, a second entry at [claudeRootFile] ("CLAUDE.md")
// whose Bytes is the same markdown body, trimmed, with no frontmatter and no
// markers — the section content internal/team's write engine wraps in a
// marker span and upserts into the repo's shared CLAUDE.md.
//
// It fails loudly, per the package doc, on any frontmatter field it cannot
// express: today that is anything landing in [Frontmatter].Extra, since
// Claude's sub-agent frontmatter convention as this renderer understands it
// has no room for arbitrary additional keys.
func renderClaude(source ResolvedSource) ([]RenderedFile, error) {
	fm := source.Frontmatter

	if fm.Name == "" {
		return nil, fmt.Errorf("render claude: cannot derive %s path: %w", claudeAgentsDir, ErrMissingAgentName)
	}

	if len(fm.Extra) > 0 {
		keys := make([]string, 0, len(fm.Extra))
		for k := range fm.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, &UnsupportedFieldError{Agent: fm.Name, Harness: claudeHarness, Field: keys[0]}
	}

	header := claudeFrontmatter{
		Name:        fm.Name,
		Description: fm.Description,
		Tools:       strings.Join(fm.Tools, ", "),
		Model:       fm.Model,
	}

	yamlBytes, err := yaml.Marshal(&header)
	if err != nil {
		return nil, fmt.Errorf("render claude: marshal frontmatter for agent %q: %w", fm.Name, err)
	}

	var b strings.Builder
	b.WriteString("---\n")
	b.Write(yamlBytes)
	b.WriteString("---\n")
	b.WriteString(strings.TrimRight(source.Body, "\n"))
	b.WriteString("\n")

	path := fmt.Sprintf("%s/%s.md", claudeAgentsDir, fm.Name)
	section := strings.TrimRight(source.Body, "\n") + "\n"

	return []RenderedFile{
		{Path: path, Bytes: []byte(b.String())},
		{Path: claudeRootFile, Bytes: []byte(section)},
	}, nil
}
