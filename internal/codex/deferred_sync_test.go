package codex

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
)

func TestRunnerDeliversDeferredSyncOnEveryPath(t *testing.T) {
	for _, path := range []agent.RunPath{agent.PathCold, agent.PathResume, agent.PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			runner, _ := fakeRunner(t, `cat > prompt.txt
printf '%s\n' '{"type":"turn.completed"}'`)
			req := request(t, path)
			req.Session.Resume = "retained-thread"
			in := harness.PromptInput{Ref: req.Ref, RunOrdinal: 1, FeatureSlug: "recovery", Branch: "lf/171-recovery", Mode: "spike", Instruction: "finish retained work", Resume: path == agent.PathResume, DeferredSync: harness.DeferredSyncContext{Deferred: true, Branch: "lf/171-recovery", Base: "origin/main", Behind: 3, Ahead: 2}}
			var err error
			req.Prompt, err = harness.BuildPrompt(in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := runner.Run(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			got := string(mustRead(t, filepath.Join(req.WorktreePath, "prompt.txt")))
			for _, want := range []string{"preserved uncommitted changes", "3 commit(s) behind and 2 ahead", "origin/main", "lf/171-recovery", "The base was not merged"} {
				if !strings.Contains(got, want) {
					t.Errorf("stdin lacks %q: %s", want, got)
				}
			}
		})
	}
}
