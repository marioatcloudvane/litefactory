package report

import (
	"fmt"
	"strings"
)

// Execution describes recorded execution evidence, never requested model settings.
// Separate entries preserve changes between runs, attempts and invocations.
type Execution struct {
	Run                                                      int
	Step, Attempt, Invocation, Agent, Model, ModelProvenance string
}

// ExecutionReportLimit leaves room for the diagnosis and durable delivery marker.
const ExecutionReportLimit = 8 << 10

// RenderExecutions renders a bounded observation list. Callers still apply the
// ordinary outbound redaction before retaining or publishing the result.
func RenderExecutions(entries []Execution, limit int) string {
	if len(entries) == 0 || limit < 160 {
		return ""
	}
	limit = min(limit, ExecutionReportLimit)
	const heading = "### Agents and reported models\n\n"
	const omitted = "\nFurther execution entries omitted to fit the report."
	var b strings.Builder
	b.WriteString(heading)
	seen := make(map[Execution]bool)
	for _, entry := range entries {
		if seen[entry] {
			continue
		}
		seen[entry] = true
		agent := entry.Agent
		if agent == "" {
			agent = "not reported"
		}
		model := entry.Model
		if model == "" {
			model = "not reported"
		}
		scope := "Execution"
		if entry.Run > 0 {
			scope = fmt.Sprintf("Run %d", entry.Run)
		}
		if entry.Step != "" {
			scope += " · " + executionSpan(entry.Step)
		}
		if entry.Attempt != "" {
			scope += " · attempt " + executionSpan(entry.Attempt)
		}
		if entry.Invocation != "" {
			scope += " · invocation " + executionSpan(entry.Invocation)
		}
		line := fmt.Sprintf("- %s: agent %s; model %s", scope, executionSpan(agent), executionSpan(model))
		if entry.Model != "" && entry.ModelProvenance != "" {
			line += " (" + executionSpan(entry.ModelProvenance) + ")"
		}
		line += "\n"
		if b.Len()+len(line)+len(omitted) > limit {
			b.WriteString(omitted)
			break
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(b.String())
}

func executionSpan(text string) string {
	text = oneLine(text)
	// Omit an overlong field whole, so shortening cannot expose a credential
	// prefix that the final redactor could no longer recognize.
	if len(text) > 160 {
		text = "overlong value omitted"
	}
	// Dynamic code delimiters keep literal provider text from creating Markdown
	// while leaving credential bytes intact for final outbound redaction.
	return codeSpan(text)
}
