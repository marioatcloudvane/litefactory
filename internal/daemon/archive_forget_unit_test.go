package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestForgetResourceReason(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   RetirementEvaluation
		want string
	}{
		{"no decisions/keeps for resources", RetirementEvaluation{}, ForgetReasonResources},
		{"all already absent/forgettable", RetirementEvaluation{Decisions: []RetirementDecision{{Action: "already_absent"}, {Action: "already_absent"}}}, ""},
		{"active work/not settled", RetirementEvaluation{Decisions: []RetirementDecision{{Action: "keep", Reason: "active_work"}}}, ForgetReasonNotSettled},
		{"pending effects/not settled", RetirementEvaluation{Decisions: []RetirementDecision{{Action: "keep", Reason: "pending_effects"}}}, ForgetReasonNotSettled},
		{"worktree present/keeps for resources", RetirementEvaluation{Decisions: []RetirementDecision{{Action: "already_absent"}, {Action: "remove", Reason: "merged"}}}, ForgetReasonResources},
		{"other keep reason/keeps for resources", RetirementEvaluation{Decisions: []RetirementDecision{{Action: "keep", Reason: "unmerged"}}}, ForgetReasonResources},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := forgetResourceReason(tt.in); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDaemon_ForgetFilesVisit(t *testing.T) {
	t.Parallel()
	setup := func(t *testing.T) (*archiveFixture, store.ArchiveOwner, string) {
		t.Helper()
		x := newArchiveFixture(t)
		s := x.owner(t, 7, 0)
		runs := t.TempDir()
		o := store.ArchiveOwner{}
		o.Repo = x.reg
		o.Session = s
		o.Family = store.ArchiveFamily{SessionID: s.ID, RepoID: s.RepoID, TaskRef: s.TaskRef}
		return x, o, runs
	}

	t.Run("archive file present/keeps with archive reason", func(t *testing.T) {
		t.Parallel()
		x, o, runs := setup(t)
		dir := archive.TaskDir(x.dir, o.Repo.Forge, o.Repo.Owner, o.Repo.Name, 7)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "a"+archive.FileSuffix), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		d := x.daemon(ArchiveOptions{RunsDir: runs})

		v, stop, err := d.forgetFilesVisit(t.Context(), o)

		if err != nil || !stop {
			t.Fatalf("stop=%v err=%v", stop, err)
		}
		if v.Outcome != ArchiveOutcomeKept || v.Reason != ForgetReasonArchive {
			t.Fatalf("visit = %+v", v)
		}
	})

	t.Run("run log folder present/keeps with run log reason", func(t *testing.T) {
		t.Parallel()
		x, o, runs := setup(t)
		if err := os.MkdirAll(filepath.Join(runs, o.Repo.Forge, o.Repo.Owner, o.Repo.Name, strconv.Itoa(7)), 0o755); err != nil {
			t.Fatal(err)
		}
		d := x.daemon(ArchiveOptions{RunsDir: runs})

		v, stop, err := d.forgetFilesVisit(t.Context(), o)

		if err != nil || !stop {
			t.Fatalf("stop=%v err=%v", stop, err)
		}
		if v.Outcome != ArchiveOutcomeKept || v.Reason != ForgetReasonRunLogs {
			t.Fatalf("visit = %+v", v)
		}
	})

	t.Run("runs dir unset/defers with leftover check failure", func(t *testing.T) {
		t.Parallel()
		x, o, _ := setup(t)
		d := x.daemon(ArchiveOptions{})

		v, stop, err := d.forgetFilesVisit(t.Context(), o)

		if err == nil || !stop || v.Outcome != ArchiveOutcomeDeferred || v.Reason != "leftover_check_failed" {
			t.Fatalf("visit=%+v stop=%v err=%v", v, stop, err)
		}
	})

	t.Run("nothing left/does not stop", func(t *testing.T) {
		t.Parallel()
		x, o, runs := setup(t)
		d := x.daemon(ArchiveOptions{RunsDir: runs})

		_, stop, err := d.forgetFilesVisit(t.Context(), o)

		if err != nil || stop {
			t.Fatalf("stop=%v err=%v", stop, err)
		}
	})
}
