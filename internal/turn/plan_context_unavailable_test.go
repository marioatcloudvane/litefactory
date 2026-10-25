package turn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestPreparePlanAttemptPreservesUnavailableContext(t *testing.T) {
	for _, kind := range []string{"malformed", "foreign", "ambiguous", "unreadable", "unsafe path"} {
		t.Run(kind, func(t *testing.T) {
			worktree := t.TempDir()
			ref, err := naming.ParseRef("github:o/r#170")
			mustProgress(t, err)
			now := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
			in := harness.PromptInput{Ref: ref, Branch: "lf/170-modes", FeatureSlug: "modes", Mode: "plan", PlanProposalPath: planbrief.ProposalFileName, PlanGeneration: 1}
			dir, err := naming.AITeamSessionDir(in.Branch, now, in.FeatureSlug)
			mustProgress(t, err)
			path := filepath.Join(worktree, dir, planbrief.ContextFileName)
			working := planbrief.WorkingContext{Version: 1, Parent: ref.String(), Branch: in.Branch, Feature: in.FeatureSlug, Scope: "scope", Decisions: "None", Assumptions: "None", Questions: "None"}
			if kind == "foreign" {
				working.Parent = "github:o/r#171"
			}
			writePlanJSON(t, path, working)
			switch kind {
			case "malformed":
				mustProgress(t, os.WriteFile(path, []byte("unadmitted secret context"), 0600))
			case "ambiguous":
				oldDir, err := naming.AITeamSessionDir(in.Branch, now.AddDate(0, 0, -1), in.FeatureSlug)
				mustProgress(t, err)
				writePlanJSON(t, filepath.Join(worktree, oldDir, planbrief.ContextFileName), working)
			case "unreadable":
				mustProgress(t, os.Remove(path))
				mustProgress(t, os.Mkdir(path, 0700))
			case "unsafe path":
				in.PlanContextPath = "../outside/PLAN_CONTEXT.json"
			}
			before, err := os.ReadFile(path)
			if kind != "unreadable" {
				mustProgress(t, err)
			}
			mustProgress(t, preparePlanAttempt(worktree, in.Branch, store.Run{ID: 7, StartedAt: now}, &in, discardLogger()))
			if in.PlanWorkingContext != "" || in.PlanContextUnavailable == "" {
				t.Fatalf("unavailable context admitted: %+v", in)
			}
			if !filepath.IsLocal(in.PlanContextPath) || !strings.Contains(in.PlanContextPath, "plan-attempt-") {
				t.Fatalf("fresh safe destination missing: %s", in.PlanContextPath)
			}
			if kind == "unreadable" {
				info, err := os.Stat(path)
				mustProgress(t, err)
				if !info.IsDir() {
					t.Fatal("unusable source replaced")
				}
			} else {
				after, err := os.ReadFile(path)
				mustProgress(t, err)
				if string(before) != string(after) {
					t.Fatal("unusable source changed")
				}
			}
			// A resumed subprocess also receives no unavailable bytes and a new output.
			previous := in.PlanContextPath
			mustProgress(t, preparePlanAttempt(worktree, in.Branch, store.Run{ID: 7, StartedAt: now}, &in, discardLogger()))
			if previous == in.PlanContextPath || in.PlanWorkingContext != "" {
				t.Fatal("resumed attempt reused unavailable input or output")
			}
		})
	}
}
