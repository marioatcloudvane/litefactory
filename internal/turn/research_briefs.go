package turn

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// ResearchBriefName and ResearchTechnicalName are the two documents the factory
// renders from a node result. They are split by reader: the brief is what the
// owner and the next stage need, the technical file is the proof behind it.
const (
	ResearchBriefName     = "RESEARCH_BRIEF.md"
	ResearchTechnicalName = "RESEARCH_TECHNICAL.md"
)

// WriteResearchBriefs renders both documents from the retained result and
// replaces whatever was there before.
//
// Rendering rather than authoring is what makes supersession real. The agent
// emits identified decisions, assumptions and findings and the factory lays them
// out, so a changed decision replaces its own entry instead of being appended
// below the old one, and a retired identity simply stops being rendered. An
// agent writing Markdown by hand has no such pressure, which is how a brief
// reaches a hundred and seventy thousand bytes of mostly superseded text.
//
// Nothing is lost by overwriting: the session directory is committed on every
// run, so every earlier version of both documents is in the repository history.
func WriteResearchBriefs(worktree, session string, result agentresult.Result) error {
	if result.Payload.Research == nil {
		return nil
	}
	if !agentresult.ValidPath(session) {
		return fmt.Errorf("research briefs: invalid session path")
	}
	root, err := os.OpenRoot(worktree)
	if err != nil {
		return fmt.Errorf("research briefs root: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(session, 0o750); err != nil {
		return fmt.Errorf("research briefs directory: %w", err)
	}
	for name, body := range map[string]string{
		ResearchBriefName:     renderResearchBrief(result),
		ResearchTechnicalName: renderResearchTechnical(result),
	} {
		if err := root.WriteFile(path.Join(session, name), []byte(body), 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

// renderResearchBrief is the owner's document: what was asked, what was settled,
// what is being assumed and what is still open. Evidence deliberately stays out
// of it — a decision the owner has to make does not read better with a file and
// line number attached.
func renderResearchBrief(result agentresult.Result) string {
	r := result.Payload.Research
	var b strings.Builder
	b.WriteString("# Research brief\n\n## Request\n\n")
	b.WriteString(blankFallback(r.Request, "_The request was not recorded._"))
	section(&b, "Decisions", r.Decisions, "_No decisions have been settled yet._")
	section(&b, "Assumptions", r.Assumptions, "_Nothing is being assumed._")
	if len(result.Questions) > 0 {
		b.WriteString("\n\n## Open questions\n")
		for _, group := range researchAudienceHeadings {
			for _, q := range result.Questions {
				if q.Audience != group.audience {
					continue
				}
				b.WriteString("\n### ")
				b.WriteString(q.ID)
				b.WriteString(" — ")
				b.WriteString(strings.TrimSpace(q.Text))
				b.WriteString("\n\n_Asked of: ")
				b.WriteString(group.heading)
				b.WriteString("_\n")
				for _, o := range q.Options {
					b.WriteString("\n- **")
					b.WriteString(o.ID)
					b.WriteString("** — ")
					b.WriteString(strings.TrimSpace(o.Text))
					if o.ID == q.RecommendedOption {
						b.WriteString("  *(recommended)*")
					}
				}
				b.WriteString("\n")
			}
		}
	}
	if result.Recommendation != nil && strings.TrimSpace(result.Recommendation.Text) != "" {
		b.WriteString("\n\n## Recommendation\n\n")
		b.WriteString(strings.TrimSpace(result.Recommendation.Text))
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// renderResearchTechnical is the engineer's document: every finding with the
// source it rests on, so a claim can be checked rather than believed.
func renderResearchTechnical(result agentresult.Result) string {
	r := result.Payload.Research
	var b strings.Builder
	b.WriteString("# Research findings\n\n")
	b.WriteString("_Technical detail behind [the brief](")
	b.WriteString(ResearchBriefName)
	b.WriteString("). Findings carry the source they rest on._\n")
	if len(r.Findings) == 0 {
		b.WriteString("\n_No findings have been recorded yet._\n")
		return b.String()
	}
	for _, f := range r.Findings {
		b.WriteString("\n## ")
		b.WriteString(f.ID)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimSpace(f.Text))
		b.WriteString("\n")
		if len(f.Evidence) == 0 {
			b.WriteString("\n_No source reference was recorded for this finding._\n")
			continue
		}
		b.WriteString("\nEvidence:\n\n")
		for _, e := range f.Evidence {
			b.WriteString("- `")
			b.WriteString(e.Path)
			if e.Line > 0 {
				fmt.Fprintf(&b, ":%d", e.Line)
			}
			b.WriteString("`")
			if e.Revision != "" {
				b.WriteString(" @ `")
				b.WriteString(e.Revision)
				b.WriteString("`")
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

func section(b *strings.Builder, heading string, items []agentresult.Statement, empty string) {
	b.WriteString("\n\n## ")
	b.WriteString(heading)
	b.WriteString("\n\n")
	if len(items) == 0 {
		b.WriteString(empty)
		return
	}
	for _, s := range items {
		b.WriteString("- **")
		b.WriteString(s.ID)
		b.WriteString("** — ")
		b.WriteString(strings.TrimSpace(s.Text))
		b.WriteString("\n")
	}
}

func blankFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
