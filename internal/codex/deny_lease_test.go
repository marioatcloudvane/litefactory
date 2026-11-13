package codex

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
)

func TestDenyLeaseSessionExclusion(t *testing.T) {
	r := &Runner{paths: config.NewPaths(filepath.Join(t.TempDir(), "new-factory-home"))}
	req := agent.Request{Invocation: agent.InvocationGraphNode, Session: agent.SessionRef{Ref: "factory", Resume: "native"}}
	release, err := r.acquireDenyLease(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, session := range []agent.SessionRef{{Ref: "factory"}, {Ref: "other", Resume: "native"}} {
		other := req
		other.Session = session
		ctx, cancel := context.WithTimeout(t.Context(), 70*time.Millisecond)
		_, err := r.acquireDenyLease(ctx, other)
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("same session acquired: %v", err)
		}
	}
	independent := req
	independent.Session = agent.SessionRef{Ref: "independent", Resume: "other-native"}
	otherRelease, err := r.acquireDenyLease(t.Context(), independent)
	if err != nil {
		t.Fatal(err)
	}
	otherRelease()
	release()
	corrected, err := r.acquireDenyLease(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	corrected()
}

func TestDenyLeaseCrossProcess(t *testing.T) {
	const marker = "LF_TEST_CODEX_DENY_LEASE_HOME"
	if home := os.Getenv(marker); home != "" {
		r := &Runner{paths: config.NewPaths(home)}
		ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
		defer cancel()
		release, err := r.acquireDenyLease(ctx, agent.Request{Invocation: agent.InvocationGraphNode, Session: agent.SessionRef{Ref: "shared"}})
		if release != nil {
			release()
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("child did not contend: %v", err)
		}
		return
	}
	home := t.TempDir()
	r := &Runner{paths: config.NewPaths(home)}
	release, err := r.acquireDenyLease(t.Context(), agent.Request{Invocation: agent.InvocationGraphNode, Session: agent.SessionRef{Ref: "shared"}})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestDenyLeaseCrossProcess$", "-test.timeout=5s")
	cmd.Env = append(os.Environ(), marker+"="+home)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child: %v\n%s", err, output)
	}
}

func TestDenyLeaseRefusesSymlinksAndReplacement(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		r := &Runner{paths: config.NewPaths(t.TempDir())}
		if err := os.Symlink(t.TempDir(), r.paths.CodexPolicyDir()); err != nil {
			t.Fatal(err)
		}
		if _, err := r.denyLeaseDirectory(); err == nil {
			t.Fatal("accepted policy symlink")
		}
	})
	t.Run("file", func(t *testing.T) {
		r := &Runner{paths: config.NewPaths(t.TempDir())}
		dir, err := r.denyLeaseDirectory()
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(t.TempDir(), "target")
		if err := os.WriteFile(target, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(dir, "test.lock")); err != nil {
			t.Fatal(err)
		}
		if f, err := acquireDenyLockFile(t.Context(), dir, "test.lock"); err == nil {
			f.Close()
			t.Fatal("accepted lock symlink")
		}
	})
	t.Run("dangling internal target", func(t *testing.T) {
		r := &Runner{paths: config.NewPaths(t.TempDir())}
		dir, err := r.denyLeaseDirectory()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("must-not-be-created", filepath.Join(dir, "test.lock")); err != nil {
			t.Fatal(err)
		}
		if f, err := acquireDenyLockFile(t.Context(), dir, "test.lock"); err == nil {
			f.Close()
			t.Fatal("accepted dangling lease symlink")
		}
		if _, err := os.Stat(filepath.Join(dir, "must-not-be-created")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("followed dangling lease symlink: %v", err)
		}
	})
	t.Run("replacement", func(t *testing.T) {
		r := &Runner{paths: config.NewPaths(t.TempDir())}
		dir, err := r.denyLeaseDirectory()
		if err != nil {
			t.Fatal(err)
		}
		holder, err := acquireDenyLockFile(t.Context(), dir, "test.lock")
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		done := make(chan error, 1)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		go func() {
			f, err := acquireDenyLockFile(ctx, dir, "test.lock")
			if f != nil {
				f.Close()
			}
			done <- err
		}()
		// Wait until the contender has had time to open the original held inode.
		time.Sleep(60 * time.Millisecond)
		if err := os.Rename(filepath.Join(dir, "test.lock"), filepath.Join(dir, "old.lock")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "test.lock"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := holder.Close(); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err == nil {
			t.Fatal("accepted replaced lock")
		}
	})
}

func TestDenyLeaseNonGraphUnchanged(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	r := &Runner{paths: config.NewPaths(home)}
	release, err := r.acquireDenyLease(t.Context(), agent.Request{})
	if err != nil {
		t.Fatal(err)
	}
	release()
	if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("non-graph lease created home: %v", err)
	}
}

func TestDenyLeaseConcurrentFirstCreation(t *testing.T) {
	r := &Runner{paths: config.NewPaths(t.TempDir())}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 16)
	for range 16 {
		go func() {
			<-start
			release, err := r.acquireNativeDenyLease(ctx, "simultaneously-created")
			if release != nil {
				release()
			}
			results <- err
		}()
	}
	close(start)
	for range 16 {
		if err := <-results; err != nil {
			t.Error(err)
		}
	}
}
