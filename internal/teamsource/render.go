package teamsource

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/render"
	"gopkg.in/yaml.v3"
	"strings"
)

// parseAgentSource splits embedded markdown into frontmatter and body.
func parseAgentSource(raw []byte) (render.ResolvedSource, error) {
	const delim = "---"
	text := string(raw)
	if !strings.HasPrefix(text, delim+"\n") {
		return render.ResolvedSource{}, fmt.Errorf("missing leading %q frontmatter delimiter", delim)
	}
	rest := text[len(delim+"\n"):]
	end := strings.Index(rest, "\n"+delim)
	if end < 0 {
		return render.ResolvedSource{}, fmt.Errorf("unterminated frontmatter block (no closing %q)", delim)
	}
	header := rest[:end]
	body := strings.TrimPrefix(rest[end+len("\n"+delim):], "\n")

	var fm render.Frontmatter
	if err := yaml.Unmarshal([]byte(header), &fm); err != nil {
		return render.ResolvedSource{}, fmt.Errorf("decode frontmatter: %w", err)
	}
	return render.ResolvedSource{Frontmatter: fm, Body: body}, nil
}
