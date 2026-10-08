package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

func TestTeamBindingProcessHelper(t *testing.T) {
	clone := os.Getenv("LF_TEST_BINDING_CLONE")
	if clone == "" {
		return
	}
	paths, err := config.Locate()
	if err != nil {
		t.Fatal(err)
	}
	db, err := openTeamBindingDB(t.Context(), paths)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	repo, err := db.GetRepoByPath(t.Context(), clone)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: validateTeamPackage, InUse: teamRemovalGuard(paths)})
	if err != nil {
		t.Fatal(err)
	}
	err = manager.WithCatalogue(t.Context(), func(a *teampackage.CatalogueAccess) error {
		return a.WithSelected(t.Context(), "local", "custom", "1.0.0", func(entry teampackage.Installed, _ fs.FS) error {
			if _, err := fmt.Fprintln(os.Stdout, "ready"); err != nil {
				return err
			}
			var signal [1]byte
			if _, err := io.ReadFull(os.Stdin, signal[:]); err != nil {
				return err
			}
			_, err := db.ReplaceRepositoryTeamBinding(t.Context(), store.RepositoryTeamBinding{RepoID: repo.ID, Source: entry.Identity.Source, Slug: entry.Identity.Slug, Version: entry.Identity.Version, Kind: "local"})
			return err
		})
	})
	if err != nil {
		t.Fatal(err)
	}
}

func assertProcessLinkProtectsRemoval(t *testing.T, paths config.Paths, clone string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTeamBindingProcessHelper$")
	cmd.Env = append(os.Environ(), "LF_TEST_BINDING_CLONE="+clone)
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostics strings.Builder
	cmd.Stderr = &diagnostics
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = input.Close()
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan bool, 1)
	go func() { scanner := bufio.NewScanner(output); ready <- scanner.Scan() && scanner.Text() == "ready" }()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("link child failed before lock boundary")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("link child did not reach lock boundary")
	}
	manager, err := teampackage.New(teampackage.Options{Paths: paths, Validate: validateTeamPackage, InUse: teamRemovalGuard(paths)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	removed, err := manager.Remove(ctx, "local", "custom", "1.0.0")
	cancel()
	if removed || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("removal crossed link selection boundary: %v %v", removed, err)
	}
	if _, err := input.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("link child: %v %s", err, diagnostics.String())
	}
	if removed, err := manager.Remove(t.Context(), "local", "custom", "1.0.0"); removed || err == nil || !strings.Contains(err.Error(), clone) {
		t.Fatalf("binding survived removal: %v %v", removed, err)
	}
}
