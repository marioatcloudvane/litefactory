package turn

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/doctor"
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
		// The intervening writer is another actor: it does not inherit the
		// advisor's budget deadline.
		release, e := gate.Acquire(t.Context(), w.ClonePath())
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

// conflictedDoctorSync prepares a conflicting task branch and its approved inputs.
func conflictedDoctorSync(t *testing.T, advisor ConflictAdvisor) (*gittest.Repo, *git.Worktree, DoctorSyncOptions) {
	t.Helper()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 559, "doctor conflict budget")
	conflictBaseAndBranch(t, repo, w.Path())
	head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	return repo, w, DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, Advisor: advisor, OperationID: 1, Task: "github:fixture/repo#559", Owner: "owner", Model: "test", RecordBackup: func(context.Context, string) error { return nil }}
}

func TestDoctorSyncAdvisorGetsBudgetInsideRepairDeadline(t *testing.T) {
	outer, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	outerDeadline, _ := outer.Deadline()
	var remaining time.Duration
	advisor := doctorSyncAdvisorFunc(func(ctx context.Context, r agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		deadline, ok := ctx.Deadline()
		if !ok || !deadline.Before(outerDeadline) {
			t.Errorf("advisor deadline %v not before repair deadline %v", deadline, outerDeadline)
		}
		remaining = time.Until(deadline)
		return (&doctorSyncAdvisor{}).ProposeConflictResolution(ctx, r)
	})
	repo, w, o := conflictedDoctorSync(t, advisor)
	result, e := SyncDoctorBranch(outer, o)
	if e != nil || !result.Rechecked {
		t.Fatalf("sync %+v %v", result, e)
	}
	if remaining <= 8*time.Minute || remaining > doctorSyncAdvisorBudget {
		t.Fatalf("advisor budget %v", remaining)
	}
	local, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil || local != result.Head || local == o.BeforeHead {
		t.Fatalf("local %s result %s %v", local, result.Head, e)
	}
	if remote := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remote != local {
		t.Fatalf("remote %s local %s", remote, local)
	}
}

func TestDoctorSyncAdvisorOverBudgetChangesNothing(t *testing.T) {
	saved := doctorSyncAdvisorBudget
	doctorSyncAdvisorBudget = 50 * time.Millisecond
	t.Cleanup(func() { doctorSyncAdvisorBudget = saved })
	advisor := doctorSyncAdvisorFunc(func(ctx context.Context, _ agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		<-ctx.Done()
		return agent.ConflictResolution{}, ctx.Err()
	})
	repo, w, o := conflictedDoctorSync(t, advisor)
	remoteBranches := repo.GitIn(repo.Origin, "branch", "--list", w.Branch())
	result, e := SyncDoctorBranch(t.Context(), o)
	if !errors.Is(e, context.DeadlineExceeded) || result.Rechecked {
		t.Fatalf("over-budget advisor %+v %v", result, e)
	}
	local, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil || local != o.BeforeHead {
		t.Fatalf("head changed %s %v", local, e)
	}
	if changes, e := w.Status(t.Context()); e != nil || len(changes) != 0 {
		t.Fatalf("worktree changed %v %v", changes, e)
	}
	if after := repo.GitIn(repo.Origin, "branch", "--list", w.Branch()); after != remoteBranches {
		t.Fatalf("origin branch changed %q -> %q", remoteBranches, after)
	}
}

func TestDoctorSyncDeadlineDuringPushKeepsPushedHead(t *testing.T) {
	repo, w, o := conflictedDoctorSync(t, &doctorSyncAdvisor{})
	marker := filepath.Join(t.TempDir(), "received")
	hook := "#!/bin/sh\ntouch '" + marker + "'\nsleep 2\n"
	if e := os.WriteFile(filepath.Join(repo.Origin, "hooks", "post-receive"), []byte(hook), 0o700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		for ctx.Err() == nil {
			if _, e := os.Stat(marker); e == nil {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	result, e := SyncDoctorBranch(ctx, o)
	if _, x := os.Stat(marker); x != nil {
		t.Fatalf("origin never received the push: %v (sync %v)", x, e)
	}
	local, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if x != nil {
		t.Fatal(x)
	}
	remote := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch())
	if local != remote || local == o.BeforeHead {
		t.Fatalf("local %s diverged from origin %s (sync %+v %v)", local, remote, result, e)
	}
	if e != nil || !result.Rechecked || result.Head != remote {
		t.Fatalf("sync after accepted push %+v %v", result, e)
	}
}

// behindDoctorSyncWorktree pushes a story commit to origin's task branch and
// resets the task worktree to its earlier head, as story merges pushed from
// scratch worktrees leave it. It returns the stale local and remote heads.
func behindDoctorSyncWorktree(t *testing.T, repo *gittest.Repo, w *git.Worktree) (string, string) {
	t.Helper()
	local, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	commitInSyncDir(t, repo, w.Path(), "story.txt", "story merge")
	repo.GitIn(w.Path(), "push", "--quiet", "origin", w.Branch())
	remote := repo.GitIn(w.Path(), "rev-parse", "refs/remotes/origin/"+w.Branch())
	repo.GitIn(w.Path(), "reset", "--quiet", "--hard", local)
	if remote == local {
		t.Fatal("remote task head did not advance")
	}
	return local, remote
}

func TestDoctorSyncFastForwardsBehindOnlyWorktree(t *testing.T) {
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 648, "doctor behind")
	_, remote := behindDoctorSyncWorktree(t, repo, w)
	advanceSyncBase(t, repo, 1)
	base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	result, e := SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: remote, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, RecordBackup: func(_ context.Context, h string) error {
		if h != remote {
			t.Fatal("wrong backup")
		}
		return nil
	}})
	if e != nil || !result.Rechecked {
		t.Fatalf("sync %+v %v", result, e)
	}
	if head := repo.GitIn(w.Path(), "rev-parse", "HEAD"); head != result.Head {
		t.Fatalf("worktree head %s differs from result %s", head, result.Head)
	}
	if remoteHead := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remoteHead != result.Head {
		t.Fatalf("remote head %s differs from result %s", remoteHead, result.Head)
	}
	for _, ancestor := range []string{remote, base} {
		repo.GitIn(w.Path(), "merge-base", "--is-ancestor", ancestor, result.Head)
	}
}

func TestDoctorSyncBehindRefusalsPreserveHeads(t *testing.T) {
	for _, kind := range []string{"dirty", "diverged"} {
		t.Run(kind, func(t *testing.T) {
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 648, "doctor behind refusal")
			_, remote := behindDoctorSyncWorktree(t, repo, w)
			switch kind {
			case "dirty":
				if e := os.WriteFile(filepath.Join(w.Path(), "retained"), []byte("keep"), 0600); e != nil {
					t.Fatal(e)
				}
			case "diverged":
				commitInSyncDir(t, repo, w.Path(), "local.txt", "local only")
			}
			local := repo.GitIn(w.Path(), "rev-parse", "HEAD")
			advanceSyncBase(t, repo, 1)
			base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			backups := 0
			_, e = SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: remote, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { backups++; return nil }})
			if e == nil || backups != 0 {
				t.Fatalf("expected refusal before backup: %v backups %d", e, backups)
			}
			if head := repo.GitIn(w.Path(), "rev-parse", "HEAD"); head != local {
				t.Fatalf("local head changed to %s", head)
			}
			if remoteHead := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remoteHead != remote {
				t.Fatalf("remote head changed to %s", remoteHead)
			}
		})
	}
}

// TestDoctorSyncBaseMovedAfterApproval covers origin/<base> changing between
// proposal and apply: a forward move still merges the pinned BaseHead, while
// a rewrite that drops the approved BaseHead is refused unchanged.
func TestDoctorSyncBaseMovedAfterApproval(t *testing.T) {
	for _, rewritten := range []bool{false, true} {
		t.Run(map[bool]string{false: "advanced", true: "rewritten"}[rewritten], func(t *testing.T) {
			repo := gittest.New(t)
			w := newSyncWorktree(t, repo, 685, "doctor base moved")
			before, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			advanceSyncBase(t, repo, 1)
			approved, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
			if e != nil {
				t.Fatal(e)
			}
			if rewritten {
				repo.Git("reset", "--quiet", "--hard", before)
				commitInSyncDir(t, repo, repo.Dir, "rewritten.txt", "rewritten base")
				repo.Git("push", "--quiet", "--force", "origin", gittest.DefaultBranch)
			} else {
				commitInSyncDir(t, repo, repo.Dir, "later.txt", "later base")
				repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
			}
			repo.Git("fetch", "--quiet", "origin")
			current, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
			if e != nil || current == approved {
				t.Fatalf("base did not move: %s %v", current, e)
			}
			result, e := SyncDoctorBranch(t.Context(), DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: before, Base: "origin/" + gittest.DefaultBranch, BaseHead: approved, Author: syncTestAuthor, RecordBackup: func(context.Context, string) error { return nil }})
			after, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if x != nil {
				t.Fatal(x)
			}
			if rewritten {
				if !errors.Is(e, doctor.ErrRepairBaseRewritten) || result.Rechecked || after != before {
					t.Fatalf("rewritten base accepted %+v %v head %s", result, e, after)
				}
				return
			}
			if e != nil || !result.Rechecked || result.Head != after || result.BaseHead != approved {
				t.Fatalf("forward base refused %+v %v", result, e)
			}
			if ok, x := w.IsAncestor(t.Context(), approved, after); x != nil || !ok {
				t.Fatalf("merged head lacks approved base %v %v", ok, x)
			}
			if ok, x := w.IsAncestor(t.Context(), current, after); x != nil || ok {
				t.Fatalf("merge retargeted to newer base %v %v", ok, x)
			}
			if remoteHead := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remoteHead != after {
				t.Fatalf("remote head %s differs from rechecked head %s", remoteHead, after)
			}
		})
	}
}

// docsConflictedDoctorSync makes base and branch edit docs/stack-profiles.md
// differently, the plain markdown content conflict seen on #702.
func docsConflictedDoctorSync(t *testing.T, advisor ConflictAdvisor) (*gittest.Repo, *git.Worktree, DoctorSyncOptions) {
	t.Helper()
	repo := gittest.New(t)
	w := newSyncWorktree(t, repo, 702, "doctor docs conflict")
	const doc = "docs/stack-profiles.md"
	repo.WriteFile(doc, "# Stack profiles\n\nShared intro.\n")
	repo.Commit("add docs")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	repo.GitIn(w.Path(), "fetch", "--quiet", "origin")
	repo.GitIn(w.Path(), "merge", "--quiet", "--ff-only", "origin/"+gittest.DefaultBranch)
	repo.WriteFile(doc, "# Stack profiles\n\nBase rewrite of the intro.\n")
	repo.Commit("base edits docs")
	repo.Git("push", "--quiet", "origin", gittest.DefaultBranch)
	if e := os.WriteFile(filepath.Join(w.Path(), doc), []byte("# Stack profiles\n\nBranch rewrite of the intro.\n"), 0o600); e != nil {
		t.Fatal(e)
	}
	repo.GitIn(w.Path(), "commit", "--quiet", "--all", "--message", "branch edits docs")
	head, e := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	base, e := w.RevParse(t.Context(), "origin/"+gittest.DefaultBranch, git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	return repo, w, DoctorSyncOptions{Worktree: w, CloneGate: newTestGate(), CloneRoot: w.ClonePath(), Branch: w.Branch(), BeforeHead: head, Base: "origin/" + gittest.DefaultBranch, BaseHead: base, Author: syncTestAuthor, Advisor: advisor, OperationID: 1, Task: "github:fixture/repo#702", Owner: "owner", Model: "test", RecordBackup: func(context.Context, string) error { return nil }}
}

// resolvingAdvisor answers every region with text.
func resolvingAdvisor(text string, idSuffix string) ConflictAdvisor {
	return doctorSyncAdvisorFunc(func(_ context.Context, req agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		result := agent.ConflictResolution{Version: git.RegionProposalVersion}
		for _, f := range req.Files {
			out := agent.ConflictResolutionFile{Path: f.Path}
			for _, r := range f.Regions {
				resolved := text
				out.Regions = append(out.Regions, agent.ConflictRegionResolution{ID: r.ID + idSuffix, Resolution: &resolved})
			}
			result.Files = append(result.Files, out)
		}
		return result, nil
	})
}

// TestDoctorSyncDocsConflictResolvesOrNamesCategory covers #702: a markdown
// docs conflict resolves through the advisor, and each failure after the
// backup carries a fixed category with the conflicting repository paths.
func TestDoctorSyncDocsConflictResolvesOrNamesCategory(t *testing.T) {
	blocking := doctorSyncAdvisorFunc(func(ctx context.Context, _ agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		<-ctx.Done()
		return agent.ConflictResolution{}, ctx.Err()
	})
	failing := doctorSyncAdvisorFunc(func(context.Context, agent.ConflictResolutionRequest) (agent.ConflictResolution, error) {
		return agent.ConflictResolution{}, errors.New("advisor exited 1")
	})
	cases := []struct {
		name     string
		advisor  ConflictAdvisor
		push     bool
		category error
		paths    bool
	}{
		{name: "resolve", advisor: resolvingAdvisor("Merged rewrite of the intro.\n", "")},
		{name: "advisor failed", advisor: failing, category: doctor.ErrRepairAdvisorFailed, paths: true},
		{name: "advisor timed out", advisor: blocking, category: doctor.ErrRepairAdvisorTimedOut, paths: true},
		{name: "conflict marker", advisor: resolvingAdvisor("<<<<<<< ours\nkept\n", ""), category: doctor.ErrRepairConfirmationFailed, paths: true},
		{name: "wrong region", advisor: resolvingAdvisor("Merged.\n", "-unknown"), category: doctor.ErrRepairConfirmationFailed, paths: true},
		{name: "push rejected", advisor: resolvingAdvisor("Merged.\n", ""), push: true, category: doctor.ErrRepairPushRejected},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if errors.Is(c.category, doctor.ErrRepairAdvisorTimedOut) {
				saved := doctorSyncAdvisorBudget
				doctorSyncAdvisorBudget = 50 * time.Millisecond
				t.Cleanup(func() { doctorSyncAdvisorBudget = saved })
			}
			repo, w, o := docsConflictedDoctorSync(t, c.advisor)
			if c.push {
				repo.Git("remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "missing"))
			}
			result, e := SyncDoctorBranch(t.Context(), o)
			local, x := w.RevParse(t.Context(), "HEAD", git.RevParseOptions{})
			if x != nil {
				t.Fatal(x)
			}
			if c.category == nil {
				if e != nil || !result.Rechecked || local != result.Head {
					t.Fatalf("docs conflict not resolved %+v %v", result, e)
				}
				if ok, x := w.IsAncestor(t.Context(), o.BaseHead, local); x != nil || !ok {
					t.Fatalf("resolved head lacks base %v %v", ok, x)
				}
				if remote := repo.GitIn(repo.Origin, "rev-parse", "refs/heads/"+w.Branch()); remote != local {
					t.Fatalf("remote %s local %s", remote, local)
				}
				return
			}
			if !errors.Is(e, c.category) || result.Rechecked || local != o.BeforeHead {
				t.Fatalf("want %v, got %+v %v head %s", c.category, result, e, local)
			}
			var conflict *doctor.RepairConflictError
			if errors.As(e, &conflict) != c.paths || (c.paths && (!errors.Is(conflict.Category, c.category) || len(conflict.Paths) != 1 || conflict.Paths[0] != "docs/stack-profiles.md")) {
				t.Fatalf("conflict paths %+v (%v)", conflict, e)
			}
		})
	}
}
