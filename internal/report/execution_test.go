package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func TestExecutionEvidenceRetainsMixedMissingAndInvocationIdentity(t *testing.T) {
	t.Parallel()
	entries := []report.Execution{
		{Run: 1, Step: "analyst", Attempt: "first", Invocation: "root", Agent: "codex", Model: "model-one", ModelProvenance: "provider"},
		{Run: 1, Step: "analyst", Attempt: "first", Invocation: "child", Agent: "codex", Model: "model-two"},
		{Run: 2, Step: "fixer", Attempt: "failed-launch", Agent: "claude"},
	}
	entries = append(entries, entries[0])
	got := report.RenderExecutions(entries, report.ExecutionReportLimit)
	for _, want := range []string{"Run 1", "Run 2", "model-one", "model-two", "child", "failed-launch", "not reported", "provider"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q: %s", want, got)
		}
	}
	if strings.Count(got, "model-one") != 1 {
		t.Fatal("duplicate measurement repeated execution")
	}
}

func TestExecutionEvidenceEscapesAndBoundsProviderText(t *testing.T) {
	t.Parallel()
	entries := make([]report.Execution, 100)
	for i := range entries {
		entries[i] = report.Execution{Run: i + 1, Agent: "<script>\n@owner", Model: strings.Repeat("模型", 200)}
	}
	got := report.RenderExecutions(entries, 1024)
	if len(got) > 1024 || !strings.Contains(got, "`<script> @owner`") || !strings.Contains(got, "overlong value omitted") || !strings.Contains(got, "Further execution entries omitted") {
		t.Fatalf("unsafe or unbounded evidence: %s", got)
	}
}

func TestExecutionEvidenceRemainsVisibleToOutboundRedaction(t *testing.T) {
	t.Parallel()
	const secret = "secret<with>&encoded-markup"
	renderer := newReporter(t, func(options *redact.Options) { options.Environ = []string{"MODEL_SECRET=" + secret} })
	run := successRun()
	run.Executions = []report.Execution{{Run: 1, Agent: "codex", Model: secret}}
	body, err := renderer.RenderRun(t.Context(), run)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.String(), secret) || strings.Contains(body.String(), "secret&lt;with&gt;") || !strings.Contains(body.String(), "[redacted]") {
		t.Fatalf("model escaped final hygiene: %s", body.String())
	}
}
