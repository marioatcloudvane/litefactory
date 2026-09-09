package harness

import (
	"strings"
	"testing"
)

func TestRunnerDeliversDeferredSyncOnEveryPath(t *testing.T) {
	for _, path := range []RunPath{PathCold, PathResume, PathColdAfterResumeRefused} {
		t.Run(string(path), func(t *testing.T) {
			f := newFixture(t, `cat > prompt.txt
printf '%s\n' '{"type":"result","subtype":"success","is_error":false,"result":"done"}'`)
			in := testPromptInput()
			in.Resume = path == PathResume
			in.DeferredSync = DeferredSyncContext{Deferred: true, Branch: "lf/171-recovery", Base: "origin/main", Behind: 3, Ahead: 2}
			req := f.request()
			req.Path = path
			req.Prompt = buildPrompt(t, in)
			if _, err := f.runner.Run(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			got := f.dump(t, "prompt.txt")
			for _, want := range []string{"preserved uncommitted changes", "3 commit(s) behind and 2 ahead", "origin/main", "lf/171-recovery", "The base was not merged"} {
				if !strings.Contains(got, want) {
					t.Errorf("stdin lacks %q: %s", want, got)
				}
			}
		})
	}
}
