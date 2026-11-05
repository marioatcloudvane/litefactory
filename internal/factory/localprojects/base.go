package localprojects

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/store"
)

func errBaseChangeViaAdd(current, requested string) error {
	return unavailable(fmt.Errorf("repo add cannot change the base branch of a registered repository (stored %q, requested %q); use `repo base <id|path> <branch>`", current, requested))
}

// ChangeBase moves a registered repository to another base branch. It refuses
// unless the branch exists on the remote and nothing is executing, and then
// updates the record and the code connection in one store transaction.
func (a *Administration) ChangeBase(ctx context.Context, reference, branch string) (contracts.RepositoryBaseChangeView, error) {
	var view contracts.RepositoryBaseChangeView
	repo, err := a.findRepository(ctx, reference)
	if err != nil {
		return view, err
	}
	if err := repos.ValidateBranchName(branch); err != nil {
		return view, unavailable(err)
	}
	view.OldBase, view.NewBase, view.Repository = repo.BaseBranch, branch, repositoryView(repo)

	code, codeErr := a.db.CurrentConnection(ctx, repo.ID, store.CodeConnection)
	if codeErr != nil && !errors.Is(codeErr, store.ErrNotFound) {
		return view, fmt.Errorf("read code connection of %s: %w", repo.Slug(), codeErr)
	}
	if codeErr == nil && repo.BaseBranch == branch && code.BaseBranch == branch {
		view.NoChange = true
		return view, nil
	}

	binary, err := git.Resolve(ctx)
	if err != nil {
		return view, unavailable(err)
	}
	clone, err := binary.Open(ctx, repo.Path)
	if err != nil {
		return view, unavailable(fmt.Errorf("open clone %s: %w", repo.Path, err))
	}
	exists, err := clone.RemoteBranchExists(ctx, branch)
	if err != nil {
		return view, unavailable(fmt.Errorf("the remote of %s cannot be reached, nothing changed: %w", repo.Slug(), err))
	}
	if !exists {
		return view, unavailable(fmt.Errorf("branch %q is not on the remote of %s; nothing changed", branch, repo.Slug()))
	}
	if errors.Is(codeErr, store.ErrNotFound) {
		if err := projectconnection.SeedRegistration(ctx, a.db, repo, *a.cfg); err != nil {
			return view, fmt.Errorf("create code connection of %s: %w", repo.Slug(), err)
		}
	}

	change, err := a.db.ChangeRepoBase(ctx, repo.ID, branch)
	var open *store.OpenWorkError
	if errors.As(err, &open) {
		parts := make([]string, len(open.Items))
		for i, item := range open.Items {
			parts[i] = item.Kind + " on " + item.Issue
		}
		return view, unavailable(fmt.Errorf("work is executing, nothing changed; wait for it to finish: %s", strings.Join(parts, ", ")))
	}
	if err != nil {
		return view, err
	}
	view.NoChange = change.NoChange
	view.Repository = repositoryView(change.Repo)
	view.OldBase = change.OldRecordBase
	view.KeptByStartedIssue = change.PinnedSessions
	if change.RecordWritten && change.OldConnectionBase != "" && change.OldConnectionBase == branch {
		view.Stale = append(view.Stale, "record")
	}
	if change.ConnectionWritten && change.OldRecordBase == branch {
		view.Stale = append(view.Stale, "connection")
	}
	return view, nil
}

func (a *Administration) findRepository(ctx context.Context, reference string) (store.Repo, error) {
	notFound := func() error {
		return unavailable(fmt.Errorf("no registered repository %q; see `repo list`", reference))
	}
	if id, err := strconv.ParseInt(reference, 10, 64); err == nil {
		repo, err := a.db.GetRepo(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return store.Repo{}, notFound()
		}
		return repo, err
	}
	path := filepath.Clean(reference)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	repo, err := a.db.GetRepoByPath(ctx, path)
	if errors.Is(err, store.ErrNotFound) {
		return store.Repo{}, notFound()
	}
	return repo, err
}
