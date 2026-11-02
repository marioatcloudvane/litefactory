// Package localrights owns local operator rights and reviewer administration.
package localrights

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/rights"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Repository is an opaque local-operator scope. It is not a remote authority.
type Repository struct {
	repo   store.Repo
	domain rights.Service
}

// OpenRepository resolves existing repository and configured owner authority.
// It performs no mutation and requires no running daemon or remote transport.
func OpenRepository(ctx context.Context, db *store.DB, owner, name string) (*Repository, error) {
	repo, err := db.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if errors.Is(err, store.ErrNotFound) {
		return nil, &contracts.LocalRightsUnavailable{Err: fmt.Errorf("repository %s/%s is not registered", owner, name)}
	}
	if err != nil {
		return nil, err
	}
	ids, err := turn.LoadIdentities(ctx, db, store.ForgeGitHub)
	if err != nil {
		return nil, &contracts.LocalRightsUnavailable{Err: fmt.Errorf("configured numeric owner identity required: %w", err)}
	}
	return &Repository{repo: repo, domain: rights.Service{Store: db, OwnerID: ids.OwnerUserID}}, nil
}

// SetCatalogue binds the existing host-composed catalogue source. It is called
// once during local composition, before the scope is given to a handler.
func (r *Repository) SetCatalogue(source rights.Source) { r.domain.Catalogue = source }

func (r *Repository) taskRef(task string) string {
	task = strings.TrimPrefix(task, "#")
	if task != "" {
		if _, err := strconv.Atoi(task); err == nil {
			return fmt.Sprintf("%s:%s/%s#%s", r.repo.Forge, r.repo.Owner, r.repo.Name, task)
		}
	}
	return task
}

// Entries returns the repository’s grantable workflow catalogue.
func (r *Repository) Entries(ctx context.Context) ([]contracts.RightsEntryView, error) {
	rows, err := r.domain.Entries(ctx, r.repo.ID)
	var out []contracts.RightsEntryView
	for _, row := range rows {
		out = append(out, contracts.RightsEntryView(row))
	}
	return out, err
}

// ListGrants returns grants in the existing local administration scope.
func (r *Repository) ListGrants(ctx context.Context, activeOnly bool) ([]contracts.RightsGrantView, error) {
	rows, err := r.domain.ListGrants(ctx, r.repo.ID, activeOnly)
	var out []contracts.RightsGrantView
	for _, row := range rows {
		out = append(out, contracts.RightsGrantView(row))
	}
	return out, err
}

// Grant validates and retains one local rights grant.
func (r *Repository) Grant(ctx context.Context, request contracts.GrantRightRequest) (contracts.RightsGrantView, error) {
	row, err := r.domain.Grant(ctx, rights.GrantInput{RepoID: r.repo.ID, SubjectID: request.SubjectID, SubjectLogin: request.SubjectLogin, Workflow: request.Workflow, Stage: request.Stage, Action: request.Action, TaskRef: r.taskRef(request.TaskRef), ExpiresAt: request.ExpiresAt})
	return contracts.RightsGrantView(row), err
}

// Revoke closes one retained rights grant without deleting its history.
func (r *Repository) Revoke(ctx context.Context, id int64) (contracts.RightsGrantView, error) {
	row, err := r.domain.Revoke(ctx, r.repo.ID, id)
	return contracts.RightsGrantView(row), err
}

// History returns append-only local rights and reviewer history.
func (r *Repository) History(ctx context.Context) ([]contracts.RightsHistoryView, error) {
	rows, err := r.domain.History(ctx, r.repo.ID)
	var out []contracts.RightsHistoryView
	for _, row := range rows {
		out = append(out, contracts.RightsHistoryView{Sequence: row.Sequence, At: row.At, ActorID: row.ActorID, Kind: row.Kind, RecordID: row.RecordID, Snapshot: row.Snapshot})
	}
	return out, err
}

// Assign assigns or replaces reviewers under the existing atomic rules.
func (r *Repository) Assign(ctx context.Context, request contracts.AssignReviewerRequest) (contracts.ReviewerView, error) {
	in := rights.AssignInput{RepoID: r.repo.ID, TaskRef: r.taskRef(request.TaskRef), Workflow: request.Workflow, Stage: request.Stage, ReviewerID: request.ReviewerID, ReviewerLogin: request.ReviewerLogin}
	var row store.ReviewerAssignment
	var err error
	if request.Replace {
		row, err = r.domain.Replace(ctx, in)
	} else {
		row, err = r.domain.Assign(ctx, in)
	}
	return contracts.ReviewerView(row), err
}

// Remove removes the selected local resource under its existing safety checks.
func (r *Repository) Remove(ctx context.Context, id int64) (contracts.ReviewerView, error) {
	row, err := r.domain.Remove(ctx, r.repo.ID, id)
	return contracts.ReviewerView(row), err
}

// ListReviewers returns reviewer assignments for the selected task scope.
func (r *Repository) ListReviewers(ctx context.Context, task string, openOnly bool) ([]contracts.ReviewerView, error) {
	rows, err := r.domain.ListReviewers(ctx, r.repo.ID, r.taskRef(task), openOnly)
	var out []contracts.ReviewerView
	for _, row := range rows {
		out = append(out, contracts.ReviewerView(row))
	}
	return out, err
}
