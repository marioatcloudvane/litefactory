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
func JudgeText(p JudgePresentation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Advisory challenge of %s\n\nTarget: %s\nVerdict: %s\nProvider: %s; model: %s; thinking: %s\n", judgeOneLine(p.SourceName), judgeOneLine(p.SourceReference), strings.ReplaceAll(p.Report.Verdict, "_", " "), judgeOneLine(p.Provider), judgeOneLine(p.Model), judgeOneLine(p.Thinking))
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
	b.WriteString("\nThis report is advisory. Workflow decisions and saved source work are unchanged.\n\n")
	writeJudgeLiteral(&b, p.Report.Summary)
	b.WriteString("\nEvidence coverage:\n")
	captured := map[string]agentresult.JudgeEvidence{}
	for _, e := range p.Evidence {
		captured[e.ID] = e
	}
	for _, id := range p.Report.Coverage {
		if e, ok := captured[id]; ok {
			writeJudgeReference(&b, e, 0, 0)
		}
	}
	for _, e := range p.Evidence {
		if !e.Available {
			fmt.Fprintf(&b, "- Unavailable %s: %s\n", judgeOneLine(e.ID), judgeOneLine(e.Gap))
		}
	}
	if len(p.Report.Uncertainty) > 0 {
		b.WriteString("\nUncertainty:\n")
		for _, u := range p.Report.Uncertainty {
			writeJudgeLiteral(&b, u)
		}
	}
	for i, f := range p.Report.Findings {
		fmt.Fprintf(&b, "\nConcern %d (%s)\n", i+1, judgeOneLine(f.Severity))
		writeJudgeLiteral(&b, "Concern: "+f.Concern+"\nImpact: "+f.Impact+"\nSuggested correction: "+f.Correction)
		for _, c := range f.Evidence {
			if e, ok := captured[c.EvidenceID]; ok {
				writeJudgeReference(&b, e, c.StartLine, c.EndLine)
			}
		}
	}
	b.WriteString("\nEvidence digests identify the redacted captured bytes supplied to the judge. Code references identify captured bytes. Local changes require the snapshot digest and path; a commit alone does not identify those bytes.\n")
	return b.String()
}

func writeJudgeLiteral(b *strings.Builder, text string) {
	text = judgeSanitize(text)
	fence := judgeFenceFor(text)
	fmt.Fprintf(b, "\n%stext\n%s\n%s\n", fence, text, fence)
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
func judgeFenceFor(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence
}
