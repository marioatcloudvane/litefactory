package render

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// opencodeHarness is this renderer's registry key.
const opencodeHarness = "opencode"

// opencodeAgentsDir is the directory opencode reads its agent definitions
// from, relative to a project's root. See the package doc's note on
// [RenderedFile].Path: this is the standalone-file shape of the two Path
// meanings that type documents.
const opencodeAgentsDir = ".opencode/agents"

// opencodeRootFile is the second, marker-tagged-section [RenderedFile].Path
// this renderer emits alongside its standalone file, per US-18's root-file
// composition convention. internal/team recognizes this exact literal value
// to mean "wrap Bytes in a <!-- litefactory:team --> marker span and
// upsert it into the repo's AGENTS.md", rather than writing Bytes to a file
// named literally "AGENTS.md" in the current directory. This package does
// not itself understand markers or root-file upsert — see [RenderedFile]'s
// doc comment — it only produces the section content.
const opencodeRootFile = "AGENTS.md"

func init() {
	Register(opencodeHarness, RendererFunc(renderOpencode))
}

// opencodeFrontmatter is the YAML header written at the top of an opencode
// agent file. Field order here is deliberately the struct's declaration
// order — yaml.Marshal walks struct fields in that order rather than
// sorting them (unlike a map, which this package would have to sort before
// ranging per the determinism obligation in doc.go) — so the emitted header
// is always "name", "description", then the two optional fields, every
// time, in every process.
//
// This mirrors Claude's own YAML-frontmatter-plus-body convention
// (name/description/tools/model) rather than a confirmed opencode-specific
// schema: opencode's exact agent-file format is itself an open research
// item (Open Question 10, still thin in RESEARCH_BRIEF.md at the time this
// renderer was written). Tools is emitted as a YAML list rather than
// Claude's comma-joined string, since that is the more natural shape for a
// []string and nothing in the (thin) research rules it out. If opencode's
// real schema turns out to differ, this struct is the only place that
// needs to change.
type opencodeFrontmatter struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools,omitempty"`
	Model       string   `yaml:"model,omitempty"`
}

// renderOpencode implements [Renderer] for harness key "opencode".
//
// # Output shape
//
// Two [RenderedFile]s: the standalone file at ".opencode/agents/<agent>.md"
// containing a YAML frontmatter block (name, description, tools as a list
// when set, model when set) followed by a blank line and the source Body
// verbatim; and, per US-18, a second entry at [opencodeRootFile]
// ("AGENTS.md") whose Bytes is the same Body, trimmed, with no frontmatter
// and no markers — the section content internal/team's write engine wraps
// in a marker span and upserts into the repo's shared AGENTS.md.
//
// # Field mapping
//
// Name, Description, Tools and Model all map directly — see
// [opencodeFrontmatter]'s doc comment for the format-uncertainty risk this
// mapping carries. Frontmatter.Extra has no opencode equivalent under this
// mapping, so any key found there fails loudly with an
// [*UnsupportedFieldError] naming the agent, the harness, and the offending
// key, per AC-2, rather than being silently dropped. When more than one
// Extra key is present, keys are sorted first so the lexicographically
// first name is reported — the same offending key, every time, regardless
// of map iteration order.
func renderOpencode(source ResolvedSource) ([]RenderedFile, error) {
	fm := source.Frontmatter

	if fm.Name == "" {
		return nil, fmt.Errorf("opencode: %w", ErrMissingAgentName)
	}

	if len(fm.Extra) > 0 {
		keys := make([]string, 0, len(fm.Extra))
		for k := range fm.Extra {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, &UnsupportedFieldError{Agent: fm.Name, Harness: opencodeHarness, Field: keys[0]}
	}

	header, err := yaml.Marshal(opencodeFrontmatter{
		Name:        fm.Name,
		Description: fm.Description,
		Tools:       fm.Tools,
		Model:       fm.Model,
	})
	if err != nil {
		return nil, fmt.Errorf("opencode: marshal frontmatter for agent %q: %w", fm.Name, err)
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

	path := fmt.Sprintf("%s/%s.md", opencodeAgentsDir, fm.Name)
	section := strings.TrimRight(source.Body, "\n") + "\n"

	return []RenderedFile{
		{Path: path, Bytes: buf.Bytes()},
		{Path: opencodeRootFile, Bytes: []byte(section)},
	}, nil
}
