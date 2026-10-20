package turn

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
)

// JudgePresentation combines factory provenance with validated advisory content.
type JudgePresentation struct {
	Cutoff                                                                 string
	ObservedModel, ProviderVersion                                         string
	SourceName, SourceReference, Provider, Model, Thinking, FallbackReason string
	Commit, LocalDigest                                                    string
	Report                                                                 agentresult.JudgeReport
	Evidence                                                               []agentresult.JudgeEvidence
}

// JudgeText renders a judge report for the ordinary redacted journal publication path.
// The owner reads the judgment first: summary, verdict and one section per finding
// in the agent's words. Provenance, coverage and digests follow in a collapsed block.
func JudgeText(p JudgePresentation) string {
	var b strings.Builder
	captured := map[string]agentresult.JudgeEvidence{}
	for _, e := range p.Evidence {
		captured[e.ID] = e
	}

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
			var refs []string
			for _, c := range f.Evidence {
				if _, ok := captured[c.EvidenceID]; ok {
					ref := judgeOneLine(c.EvidenceID)
					if c.StartLine > 0 {
						ref += fmt.Sprintf(" lines %d–%d", c.StartLine, c.EndLine)
					}
					refs = append(refs, judgeProse(ref))
				}
			}
			if len(refs) > 0 {
				fmt.Fprintf(&b, "\n<sub>Based on: %s</sub>\n", strings.Join(refs, "; "))
			}
		}
	}

	if len(p.Report.Uncertainty) > 0 {
		b.WriteString("\n# What this review could not check\n\n")
		for _, u := range p.Report.Uncertainty {
			b.WriteString("- ")
			writeJudgeProse(&b, strings.Join(strings.Fields(u), " "))
		}
	}

	b.WriteString("\nThis review is advisory. It changes no workflow decision and no saved work.\n")

	b.WriteString("\n<details>\n<summary>Review details</summary>\n\n")
	fmt.Fprintf(&b, "Target: %s\n", judgeOneLine(p.SourceReference))
	fmt.Fprintf(&b, "Provider: %s; model: %s; thinking: %s\n", judgeOneLine(p.Provider), judgeOneLine(p.Model), judgeOneLine(p.Thinking))
	if p.ObservedModel != "" {
		fmt.Fprintf(&b, "Observed model: %s; provider version: %s\n", judgeOneLine(p.ObservedModel), judgeOneLine(p.ProviderVersion))
	} else {
		b.WriteString("Observed model: unavailable; the model above is the configured native request.\n")
		if p.ProviderVersion != "" {
			fmt.Fprintf(&b, "Provider version: %s\n", judgeOneLine(p.ProviderVersion))
		}
	}
	if p.FallbackReason != "" {
		fmt.Fprintf(&b, "Availability fallback: %s\n", judgeOneLine(p.FallbackReason))
	}
	if p.Cutoff != "" {
		fmt.Fprintf(&b, "Evidence cutoff: %s\n", judgeOneLine(p.Cutoff))
	}
	if p.Commit != "" {
		fmt.Fprintf(&b, "Captured repository commit: %s\nLocal snapshot digest (before outbound hygiene): %s\n", judgeOneLine(p.Commit), judgeOneLine(p.LocalDigest))
	}
	b.WriteString("\nEvidence read:\n")
	for _, id := range p.Report.Coverage {
		if e, ok := captured[id]; ok {
			writeJudgeReference(&b, e, 0, 0)
		}
	}
	for _, f := range p.Report.Findings {
		for _, c := range f.Evidence {
			if e, ok := captured[c.EvidenceID]; ok && c.StartLine > 0 {
				writeJudgeReference(&b, e, c.StartLine, c.EndLine)
			}
		}
	}
	unavailable := 0
	for _, e := range p.Evidence {
		if !e.Available {
			unavailable++
		}
	}
	if unavailable > 0 {
		fmt.Fprintf(&b, "- %d other captured inventory entries were unavailable; the full inventory is retained locally with this request.\n", unavailable)
	}
	b.WriteString("\nEvidence digests identify the redacted captured bytes supplied to the judge. Code references identify captured bytes. Local changes require the snapshot digest and path; a commit alone does not identify those bytes.\n")
	b.WriteString("\n</details>\n")
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

func writeJudgeReference(b *strings.Builder, e agentresult.JudgeEvidence, start, end int) {
	ref := judgeOneLine(e.Reference)
	u, err := url.Parse(ref)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil && !strings.ContainsAny(ref, "()[] \\<>\"") {
		if e.Kind == "code" && start > 0 {
			ref += fmt.Sprintf("#L%d-L%d", start, end)
		}
		fmt.Fprintf(b, "- [%s](%s)", judgeOneLine(e.ID), ref)
	} else {
		fmt.Fprintf(b, "- %s: %s", judgeOneLine(e.ID), ref)
	}
	if start > 0 {
		fmt.Fprintf(b, " (lines %d–%d)", start, end)
	}
	if e.Digest != "" {
		fmt.Fprintf(b, "; digest %s", judgeOneLine(e.Digest))
	}
	b.WriteByte('\n')
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
