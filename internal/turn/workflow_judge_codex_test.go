package turn

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/codex"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// Exercise the workflow boundary through the native adapter, including the
// subprocess result broker, rather than replacing RunJudge with an in-process fake.
func TestWorkflowJudgeNativeCodex(t *testing.T) {
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan} {
		for _, unavailable := range []bool{false, true} {
			behavior := "success"
			if unavailable {
				behavior = "unavailable"
			}
			t.Run(string(mode)+"/"+behavior, func(t *testing.T) {
				f, forge, req, source, fallback := workflowJudgeFixture(t, mode)
				nativeBehavior := behavior
				if mode == agent.ModePlan && !unavailable {
					nativeBehavior = "plan-success"
				}
				binary, execution := judgeCodexFixtureBinary(t, nativeBehavior)
				service := f.opts.Judge
				runner, err := codex.New(codex.Options{Paths: service.Paths, Codex: config.Codex{Binary: binary, TimeoutSeconds: 20}, Model: "gpt-6-astra", Environ: []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
				mustProgress(t, err)
				service.Providers[agent.Kind("codex")] = runner
				judged, err := ExecuteNodeAction(t.Context(), f.opts, req)
				mustProgress(t, err)
				if judged.Activity.Outcome != "success" || forge.attemptCount() != 0 {
					t.Fatalf("native workflow assessment was not private: %+v", judged)
				}
				pinned, err := f.db.FindWorkflowJudgeRequest(t.Context(), req.InstanceID, judged.AttemptID)
				mustProgress(t, err)
				if pinned.SourceNodeAttemptID != source.ID || pinned.Source.Provider != "claude" {
					t.Fatalf("opposite provider source changed: %+v", pinned.Source)
				}
				attempts, err := f.db.ListJudgeAttempts(t.Context(), pinned.ID)
				mustProgress(t, err)
				count := 1
				if unavailable {
					count = 2
				}
				if len(attempts) != count || attempts[0].Provider != "codex" || attempts[0].ProviderVersion != "0.160.0" {
					t.Fatalf("native workflow attempts: %+v", attempts)
				}
				if unavailable && (len(fallback.calls) != 1 || attempts[1].Provider != "claude") || !unavailable && len(fallback.calls) != 0 {
					t.Fatalf("unexpected fallback: %+v calls=%d", attempts, len(fallback.calls))
				}
				nativeCalls, err := os.ReadFile(execution)
				mustProgress(t, err)
				if string(nativeCalls) != "1" {
					t.Fatalf("native launch count: %s", nativeCalls)
				}
				published, err := ExecuteNodeAction(t.Context(), f.opts, req)
				mustProgress(t, err)
				if published.Activity.Outcome != "published" || published.NextNode != "gate" || forge.attemptCount() != 1 {
					t.Fatalf("explicit native advisory publication: %+v", published)
				}
				body := forge.only(t).body
				expected := "Native Codex captured source inspected"
				if unavailable {
					expected = "Captured source inspected"
				}
				if !strings.Contains(body, expected) || !strings.Contains(body, "node source, attempt 1; graph:") || strings.Contains(body, "Availability fallback:") != unavailable {
					t.Fatalf("native advisory provenance missing: %s", body)
				}
				mustProgress(t, f.db.RetainGraphComment(t.Context(), store.GraphCommentIntent{Key: "required-native-owner-review", InstanceID: req.InstanceID, SourceAttemptID: source.ID, Node: "gate", Kind: "review", Candidate: []byte(`{"comment":"Review saved source"}`)}))
				blocked, err := ExecuteNodeAction(t.Context(), f.opts, req)
				mustProgress(t, err)
				if blocked.Status != "waiting_owner" || blocked.AttemptID != 0 || watermark(t, f) != 0 {
					t.Fatalf("native advisory bypassed owner gate: %+v", blocked)
				}
			})
		}
	}
}
