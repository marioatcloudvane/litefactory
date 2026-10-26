package turn

import (
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// JudgePresentation combines factory provenance with validated advisory content.
type JudgePresentation struct {
	Cutoff                                                                 string
	ObservedModel, ProviderVersion                                         string
	SourceName, SourceReference, Provider, Model, Thinking, FallbackReason string
	// Commit is the repository commit the judge read; RepositoryGap says why
	// there was none. Both are saved-report provenance, never rendered.
	Commit, RepositoryGap string
	Report                agentresult.JudgeReport
}

// JudgeText renders a judge report for the ordinary redacted journal publication path.
// It holds only the judgment: summary, verdict, one section per finding in the
// agent's words, and what could not be checked. Provenance stays in the saved report.
func JudgeText(p JudgePresentation) string {
	var b strings.Builder

	b.WriteString("# Summary\n\n")
	fmt.Fprintf(&b, "**Challenged:** %s · **Verdict:** %s\n\n", judgeProse(judgeOneLine(p.SourceName)), judgeVerdictText(p.Report.Verdict, len(p.Report.Findings)))
	writeJudgeProse(&b, p.Report.Summary)

	if len(p.Report.Findings) > 0 {
		b.WriteString("\n# Findings\n")
		for i, f := range p.Report.Findings {
			title := judgeOneLine(f.Title)
			if title == "" {
				title = judgeFirstSentence(f.Concern)
			}
			fmt.Fprintf(&b, "\n## Finding %d: %s\n\n", i+1, judgeProse(title))
			fmt.Fprintf(&b, "**Severity:** %s\n\n", judgeProse(judgeOneLine(f.Severity)))
			writeJudgeProse(&b, f.Concern)
			b.WriteString("\n**Why it matters:** ")
			writeJudgeProse(&b, f.Impact)
			b.WriteString("\n**What to change:** ")
			writeJudgeProse(&b, f.Correction)
		}
	}

	if len(p.Report.Uncertainty) > 0 {
		b.WriteString("\n# What this review could not check\n\n")
		for _, u := range p.Report.Uncertainty {
			b.WriteString("- ")
			writeJudgeProse(&b, strings.Join(strings.Fields(u), " "))
		}
	}

	return b.String()
}

// JudgeBriefText renders the same report as a short advisory comment, for a
// check the factory runs for an agent before the owner reads its draft: the
// summary, then one line per finding (its title and the change it asks for).
// No headings, verdict line, severities or uncertainty list; the full report
// stays saved. A node opts in with lf_format="brief".
func JudgeBriefText(p JudgePresentation) string {
	var b strings.Builder
	writeJudgeProse(&b, p.Report.Summary)
	if len(p.Report.Findings) > 0 {
		b.WriteString("\n")
	}
	for _, f := range p.Report.Findings {
		title := judgeOneLine(f.Title)
		if title == "" {
			title = judgeFirstSentence(f.Concern)
		}
		fmt.Fprintf(&b, "- **%s:** %s\n", judgeProse(title), judgeProse(judgeOneLine(f.Correction)))
	}
	return b.String()
}

func judgeVerdictText(verdict string, findings int) string {
	switch verdict {
	case "no_material_concerns":
		return "no material concerns"
	case "concerns_found":
		if findings == 1 {
			return "1 concern"
		}
		return fmt.Sprintf("%d concerns", findings)
	case "unable_to_assess":
		if findings > 0 {
			return fmt.Sprintf("%d concerns; the review could not check everything", findings)
		}
		return "unable to assess"
	}
	return judgeProse(judgeOneLine(strings.ReplaceAll(verdict, "_", " ")))
}

// writeJudgeProse writes agent text as a plain paragraph.
func writeJudgeProse(b *strings.Builder, text string) {
	b.WriteString(judgeProse(strings.TrimSpace(judgeSanitize(text))))
	b.WriteString("\n")
}

// judgeProse keeps agent text readable while it stays inert: no links, images,
// HTML, mentions or headings of its own, so a hostile source cannot reshape the
// comment or notify anyone.
func judgeProse(text string) string {
	r := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "@", "@\u200b", "`", "\\`")
	lines := strings.Split(r.Replace(text), "\n")
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "===") {
			lines[i] = "\\" + trimmed
		}
	}
	return strings.Join(lines, "\n")
}

func judgeFirstSentence(text string) string {
	line := judgeOneLine(text)
	if i := strings.IndexAny(line, ".!?"); i > 0 {
		line = line[:i]
	}
	if r := []rune(line); len(r) > 80 {
		line = string(r[:77]) + "…"
	}
	if line == "" {
		return "Concern"
	}
	return line
}

func judgeSanitize(text string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, text)
}
func judgeOneLine(text string) string { return strings.Join(strings.Fields(judgeSanitize(text)), " ") }
