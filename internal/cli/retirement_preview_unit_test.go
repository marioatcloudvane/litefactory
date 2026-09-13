package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

type retirementPreviewInventory struct {
	owners []store.RetirementOwner
	err    error
}

func (i retirementPreviewInventory) RetirementBoundary(context.Context) (int64, error) {
	return 2, i.err
}
func (i retirementPreviewInventory) ListRetirementOwners(_ context.Context, after, through int64, limit int) ([]store.RetirementOwner, error) {
	var out []store.RetirementOwner
	for _, o := range i.owners {
		if o.Session.ID > after && o.Session.ID <= through {
			out = append(out, o)
		}
	}
	return out, nil
}

type retirementPreviewIssues struct{}

func (retirementPreviewIssues) GetIssue(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
	return github.Issue{ID: 1, Number: n, State: "closed", APIURL: "https://api.github.com/repos/owner/repo/issues/42"}, nil
}

func TestWriteRetirementPreview_AdvisoryExactResources(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	owner := store.RetirementOwner{Canonical: true, Repo: store.Repo{Forge: store.ForgeGitHub, Owner: "owner", Name: "repo", FeaturePrefix: "lf", WorktreeRoot: "/worktrees", BaseBranch: "main"}, Session: store.Session{ID: 1, TaskRef: "github:owner/repo#42", IssueNumber: 42, FeatureSlug: "feature", Branch: "lf/42-feature", CreatedAt: now.Add(-30 * 24 * time.Hour)}}
	inventory := retirementPreviewInventory{owners: []store.RetirementOwner{owner}}
	opts := daemon.RetirementOptions{Now: now, RetainDays: 14, Issues: retirementPreviewIssues{}, ObserveGit: func(context.Context, store.RetirementOwner) (git.RetirementObservation, error) {
		return git.RetirementObservation{WorktreeSafe: true, RemoteSHA: "remote", RemoteMerged: true, BaseSHA: "base"}, nil
	}}
	var first, second bytes.Buffer
	for _, buf := range []*bytes.Buffer{&first, &second} {
		if err := writeRetirementPreview(context.Background(), &Env{Stdout: buf}, inventory, opts); err != nil {
			t.Fatal(err)
		}
	}
	want := "Advisory retirement preview; nothing is deleted. Fresh checks are required before removal.\ngithub:owner/repo#42\tlocal_branch\t\"refs/heads/lf/42-feature\"\talready_absent\talready_absent\ngithub:owner/repo#42\tremote_branch\t\"refs/heads/lf/42-feature\"\twould_remove\tmerged_into_base\n"
	if first.String() != want {
		t.Fatalf("preview:\n%s\nwant:\n%s", first.String(), want)
	}
	if first.String() != second.String() {
		t.Error("identical observations produced nondeterministic output")
	}
	if inventory.owners[0].Session.Branch != owner.Session.Branch {
		t.Error("preview changed ownership")
	}
}

func TestWriteRetirementPreview_InventoryFailure(t *testing.T) {
	t.Parallel()
	failure := errors.New("inventory unavailable")
	var buf bytes.Buffer
	err := writeRetirementPreview(context.Background(), &Env{Stdout: &buf}, retirementPreviewInventory{err: failure}, daemon.RetirementOptions{})
	if !errors.Is(err, failure) {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(buf.String(), "would_remove") {
		t.Fatalf("failure reported a removal: %s", buf.String())
	}
}
