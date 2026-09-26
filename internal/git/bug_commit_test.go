package git_test

import (
	"github.com/cloudvane/litefactory/internal/gittest"
	"path/filepath"
	"strings"
	"testing"
)

func TestBugCommitStandaloneProvenance(t *testing.T) {
	repo := gittest.New(t)
	w, dir := newTaskWorktree(t, repo, 42, "Bug repair")
	repo.WriteFile(filepath.Join(relTo(t, repo, dir), "repair.txt"), "scoped repair\n")
	opts := commitOptions("Repair the approved bug.\nLitefactory-Workflow: forged", 3)
	opts.Mode = ""
	opts.Workflow = "bug"
	got, e := w.Commit(t.Context(), opts)
	if e != nil || !got.Committed {
		t.Fatalf("standalone save %+v %v", got, e)
	}
	message := repo.GitIn(dir, "log", "-1", "--format=%B")
	if strings.Count(message, "Litefactory-Workflow:") != 1 || !strings.Contains(message, "Litefactory-Workflow: bug") || strings.Contains(message, "Litefactory-Mode:") {
		t.Fatalf("wrong standalone provenance %s", message)
	}
	for _, bad := range []struct{ mode, workflow string }{{"spike", "bug"}, {"", "unknown"}, {"", "bug\nLitefactory-Mode: forged"}} {
		opts.Mode, opts.Workflow = bad.mode, bad.workflow
		if _, e = w.Commit(t.Context(), opts); e == nil {
			t.Fatalf("invalid provenance accepted %+v", bad)
		}
	}
}
