package turn

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestJudgeOptionsQuotedLiteralSettings(t *testing.T) {
	got, err := ParseJudgeOptions(`--agent other --focus "Check $(echo secret) and missing evidence" --target='result:7:2:digest' --model flagship --thinking high`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Agent != "other" || got.Target != "result:7:2:digest" || got.Focus != "Check $(echo secret) and missing evidence" || got.Model != "flagship" || got.Thinking != "high" {
		t.Fatalf("unexpected parsed settings: %+v", got)
	}
	if got, err := ParseJudgeOptions(""); err != nil || got.Target != "" || got.Agent != "" {
		t.Fatalf("bare judge settings: %+v %v", got, err)
	}
}

func TestJudgeOptionsRejectAmbiguousSettings(t *testing.T) {
	for _, text := range []string{
		`--agent codex --agent=claude`, `--focus a --focus b`, `--unknown value`,
		`--agent`, `--focus --target x`, `--target=""`, `--focus 'unclosed`,
		`--agent shell`, `--focus a extra`, `extra --focus a`, strings.Repeat("x", 16385),
	} {
		t.Run(text[:min(len(text), 48)], func(t *testing.T) {
			if _, err := ParseJudgeOptions(text); err == nil {
				t.Fatalf("accepted %q", text)
			}
		})
	}
}

func TestJudgeOptionsFreeTextBecomesFocus(t *testing.T) {
	for text, focus := range map[string]string{
		`this`:                                "this",
		`free text`:                           "free text",
		`check the evidence --agent codex`:    "check the evidence",
		`--target=result:1:1:d "quoted" tail`: "quoted tail",
	} {
		got, err := ParseJudgeOptions(text)
		if err != nil || got.Focus != focus {
			t.Fatalf("%q: focus=%q err=%v", text, got.Focus, err)
		}
	}
	if got, err := ParseJudgeOptions(`check the evidence --agent codex`); err != nil || got.Agent != "codex" {
		t.Fatalf("options after free text: %+v %v", got, err)
	}
}

func TestJudgeReportNamesNewestLegacySource(t *testing.T) {
	service, request, _ := judgeRecoveryFixture(t)
	source, err := service.DB.ResolveJudgeSource(t.Context(), request.Ref.String(), "")
	if err != nil {
		t.Fatal(err)
	}
	source.NewestLegacy = true
	q, err := service.DB.AdmitJudgeRequest(t.Context(), store.JudgeRequest{Task: request.Ref.String(), TicketKey: request.TicketKey, CommentID: request.RemoteCommentID, SessionID: request.SessionID, TriggerCommentID: request.CommentID, Source: source, Configuration: []byte(`{}`), Evidence: []byte(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !q.Source.NewestLegacy {
		t.Fatal("pinned request lost the newest-legacy selection")
	}
	text, err := service.save(t.Context(), q, judgeConfiguration{}, JudgeEvidenceSnapshot{}, unableJudge("controlled"), "completed")
	if err != nil {
		t.Fatal(err)
	}
	saved, err := service.DB.GetJudgeRequest(t.Context(), q.ID)
	if err != nil {
		t.Fatal(err)
	}
	report := decodeJudgeSavedReport(t, saved.Report)
	if !strings.HasPrefix(report.Presentation.SourceReference, source.Reference) || !strings.Contains(report.Presentation.SourceReference, "newest legacy save") {
		t.Fatalf("saved report does not name the chosen legacy source: %s", saved.Report)
	}
	assertJudgeTextWithoutProvenance(t, text)
}
