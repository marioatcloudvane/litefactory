package report_test

import (
	"github.com/cloudvane/litefactory/internal/report"
	"strings"
	"testing"
)

func TestTaskAndBaseSyncDisclosure(t *testing.T) {
	t.Parallel()
	rep := newReporter(t)
	body := render(t, rep, report.Run{Ref: testRef, Ordinal: 1, TaskSync: &report.Sync{Phase: "task", Base: "origin/task", LocalSHA: "local-task-input", RemoteSHA: "remote-task-input", Outcome: report.SyncSynced, Behind: 2, Ahead: 1, CommitSHA: "task-merge-result"}, Sync: &report.Sync{Phase: "base", Base: "origin/main", LocalSHA: "task-merge-result", RemoteSHA: "remote-base-input", Outcome: report.SyncSynced, Behind: 3, Ahead: 4, CommitSHA: "base-merge-result"}}).String()
	for _, text := range []string{"origin/task", "origin/main", "local-task-input", "remote-task-input", "remote-base-input", "task-merge-result", "base-merge-result"} {
		if !strings.Contains(body, text) {
			t.Errorf("missing %q in independent phase report:\n%s", text, body)
		}
	}
}
