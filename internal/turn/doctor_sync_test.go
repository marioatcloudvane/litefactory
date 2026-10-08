package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
)

func TestDoctorSyncCleanAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "clean", true: "push failure"}[fail], func(t *testing.T) {
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 539, "doctor sync")
			before, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			advanceSyncBase(t, repo, 1)
			base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if fail {
				repo.Git("remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "missing"))
			}
			backups := 0
			result, e := SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: before, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, RecordBackup: func(_ context.Context, h string) error {
				backups++
				if h != before {
					t.Fatal("wrong backup")
				}
				actual, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
				if x != nil || actual != before {
					t.Fatal("backup after mutation")
				}
				return nil
			}})
			if backups != 1 || result.BackupHead != before {
				t.Fatalf("backup %+v count %d", result, backups)
			}
			after, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if x != nil {
				t.Fatal(x)
			}
			if fail {
				if e == nil || after != before || result.Rechecked {
					t.Fatalf("failed rollback %+v %v head %s", result, e, after)
				}
			} else {
				if e != nil || !result.Rechecked || after == before {
					t.Fatalf("sync %+v %v", result, e)
				}
				if remoteHead := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remoteHead != after {
					t.Fatalf("remote head %s differs from rechecked head %s", remoteHead, after)
				}

			}
		})
	}
}

func TestDoctorSyncRefusalsPreserveHead(t *testing.T) {
	for _, kind := range []string{"branch", "head", "base", "dirty", "backup"} {
		t.Run(kind, func(t *testing.T) {
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 539, "doctor refusal")
			head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			backups := 0
			o := DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: head, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { backups++; return errors.New("backup unavailable") }}
			switch kind {
			case "branch":
				o.Branch = "wrong"
			case "head":
				o.BeforeHead = "wrong"
			case "base":
				o.BaseHead = "wrong"
			case "dirty":
				if e := os.WriteFile(filepath.Join(w.Path(), "retained"), []byte("keep"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			_, e = SyncDoctorBranch(t.Context(), o)
			if e == nil {
				t.Fatal("expected refusal")
			}
			after, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if e != nil || after != head {
				t.Fatalf("head changed %s %v", after, e)
			}
			want := 0
			if kind == "backup" {
				want = 1
			}
			if backups != want {
				t.Fatalf("backup count %d", backups)
			}
		})
	}
}

type doctorSyncAdvisor struct {
	calls int
	fail  bool
}

func (a *doctorSyncAdvisor) ProposeConflictResolution(_ context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	a.calls++
	if a.fail {
		return agent.ConflictResolution{}, errors.New("advisor failed")
	}
	result := agent.ConflictResolution{Version: git.RegionProposalVersion}
	for _, f := range req.Files {
		out := agent.ConflictResolutionFile{Path: f.Path}
		for _, r := range f.Regions {
			text := "resolved change\n"
			out.Regions = append(out.Regions, agent.ConflictRegionResolution{ID: r.ID, Resolution: &text})
		}
		result.Files = append(result.Files, out)
	}
	return result, nil
}
func TestDoctorSyncConflictAdvisor(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "resolve", true: "advisor failure"}[fail], func(t *testing.T) {
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 539, "doctor conflict")
			conflictBaseAndBranch(t, repo, w.Path())
			head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			advisor := &doctorSyncAdvisor{fail: fail}
			backups := 0
			result, e := SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, Advisor: advisor, OperationID: 1, Task: "github:fixture/repo#539", Owner: "owner", Model: "test", RecordBackup: func(context.Context, string) error { backups++; return nil }})
			if advisor.calls != 1 || backups != 1 {
				t.Fatalf("calls %d backups %d", advisor.calls, backups)
			}
			after, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if x != nil {
				t.Fatal(x)
			}
			if fail {
				if e == nil || after != head {
					t.Fatalf("failure changed head %s %v", after, e)
				}
			} else {
				if e != nil || !result.Rechecked || after == head {
					t.Fatalf("resolution %+v %v", result, e)
				}
			}
		})
	}
}

func TestDoctorSyncRefusesChangedCheckout(t *testing.T) {
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 539, "doctor checkout")
	head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	approvedBranch := w.Branch()
	repo.GitIn(w.Path(), "switch", "-c", "alternate-doctor-branch")
	backups := 0
	_, e = SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: approvedBranch, BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: head, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { backups++; return nil }})
	if e == nil || backups != 0 {
		t.Fatalf("changed checkout accepted: %v backups %d", e, backups)
	}
	for _, ref := range []string{approvedBranch, "alternate-doctor-branch"} {
		actual, x := w.RevParse(t.Context(), ref, git.RevParseOptions{})
		if x != nil || actual != head {
			t.Fatalf("branch %s changed: %s %v", ref, actual, x)
		}
	}
}

func TestDoctorSyncRequiresCloneGateBinding(t *testing.T) {
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 539, "doctor missing gate")
	head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	for _, kind := range []string{"missing gate", "missing root", "wrong root"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			o := DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: head, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { calls++; return nil }}
			switch kind {
			case "missing gate":
				o.CloneGate = nil
			case "missing root":
				o.CloneRoot = ""
			case "wrong root":
				o.CloneRoot = t.TempDir()
			}
			if _, e := SyncDoctorBranch(t.Context(), o); e == nil || calls != 0 {
				t.Fatalf("unbound gate accepted %v backups %d", e, calls)
			}
		})
	}
}

type doctorSyncAdvisorFunc func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error)

func (f doctorSyncAdvisorFunc) ProposeConflictResolution(ctx context.Context, r agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
	return f(ctx, r)
}

func TestDoctorSyncAdvisorOutsideGateAndMutationRechecksDrift(t *testing.T) {
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 539, "doctor advisor gate")
	conflictBaseAndBranch(t, repo, w.Path())
	head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	gate := newTestGate()
	changedHead := ""
	advisor := doctorSyncAdvisorFunc(func(ctx context.Context, r agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		gate.mu.Lock()
		held := gate.held[w.ClonePath()]
		gate.mu.Unlock()
		if held != 0 {
			t.Fatal("advisor invoked inside gate")
		}
		release, e := gate.Acquire(ctx, w.ClonePath())
		if e != nil {
			return agent.ConflictResolution{}, e
		}
		defer release()
		commitInSyncDir(t, repo, w.Path(), "intervening.txt", "new work during advice")
		changedHead, e = w.RevParse(ctx, "HEAD", git.RevParseOptions{})
		if e != nil {
			return agent.ConflictResolution{}, e
		}
		return (&doctorSyncAdvisor{}).ProposeConflictResolution(ctx, r)
	})
	result, e := SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: gate, CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, Advisor: advisor, RecordBackup: func(context.Context, string) error { return nil }})
	if e == nil || result.Rechecked {
		t.Fatalf("drift during advice accepted %+v %v", result, e)
	}
	after, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if x != nil || after != changedHead || after == head {
		t.Fatalf("intervening work lost %s %s %v", after, changedHead, x)
	}
	acquisitions, overlaps, deadlined := gate.report()
	if acquisitions[w.ClonePath()] != 3 || overlaps != 0 || deadlined != 0 {
		t.Fatalf("gate %v overlaps %d deadlines %d", acquisitions, overlaps, deadlined)
	}
}

func TestDoctorSyncSerializesSharedCloneWrites(t *testing.T) {
	repo := gittest.New(t)
	gate := newTestGate()
	worktrees := []*git.Worktree{newSyncWorktree(t, repo, 539, "doctor first"), newSyncWorktree(t, repo, 540, "doctor second")}
	advanceSyncBase(t, repo, 1)
	options := make([]DoctorSyncOptions, 0, 2)
	for _, w := range worktrees {
		head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
		if e != nil {
			t.Fatal(e)
		}
		base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
		if e != nil {
			t.Fatal(e)
		}
		options = append(options, DoctorSyncOptions{Worktree: w, CloneGate: gate, CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { return nil }})
	}
	results := make(chan error, 2)
	for _, o := range options {
		go func() {
			result, e := SyncDoctorBranch(t.Context(), o)
			if e == nil && !result.Rechecked {
				e = errors.New("missing recheck")
			}
			results <- e
		}()
	}
	for range options {
		select {
		case e := <-results:
			if e != nil {
				t.Fatal(e)
			}
		case <-time.After(gateTimeout):
			t.Fatal("shared clone repair deadlocked")
		}
	}
	acquisitions, overlaps, deadlined := gate.report()
	if acquisitions[worktrees[0].ClonePath()] != 4 || len(acquisitions) != 1 || overlaps != 0 || deadlined != 0 {
		t.Fatalf("gate %v overlaps %d deadlines %d", acquisitions, overlaps, deadlined)
	}
}
