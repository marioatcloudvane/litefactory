package localprojects

import (
	"context"
	"errors"
	"io/fs"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/store"
)

// Administration owns local repository registration and subscription operations.
type Administration struct {
	db      *store.DB
	cfg     *config.Config
	options Options
}

// NewAdministration binds trusted local storage and provider dependencies.
func NewAdministration(db *store.DB, cfg *config.Config, options Options) *Administration {
	return &Administration{db: db, cfg: cfg, options: options}
}
func repositoryView(repo store.Repo) contracts.RepositoryView {
	return contracts.RepositoryView{ID: repo.ID, Path: repo.Path, Forge: repo.Forge, Slug: repo.Slug(), BaseBranch: repo.BaseBranch, WorktreeRoot: repo.WorktreeRoot, LastFetchAt: repo.LastFetchAt}
}
func registrationView(r *repos.Registration) contracts.ProjectRegistrationView {
	return contracts.ProjectRegistrationView{Path: r.Path, Ref: r.Ref.String(), RemoteURL: r.RemoteURL, BaseBranch: r.BaseBranch, BaseBranchSource: string(r.BaseBranchSource), FeaturePrefix: r.FeaturePrefix, WorktreeRoot: r.WorktreeRoot, WorktreeRootSource: string(r.WorktreeRootSource)}
}

// Register validates and stores a clone before attempting best-effort webhooks.
func (a *Administration) Register(ctx context.Context, request contracts.RegisterProjectRequest, present contracts.ProjectRegistrationPresentation) error {
	// Resolving git here rather than inside internal/repos means a missing or
	// broken git is reported once, with the PATH it searched, before any of the
	// checks below run and blame the repository for it.
	binary, err := git.Resolve(ctx)
	if err != nil {
		return unavailable(err)
	}
	clone, err := binary.Open(ctx, request.Path)
	if err != nil {
		check := repos.CheckGitRepository
		if errors.Is(err, fs.ErrNotExist) {
			check = repos.CheckPath
		}
		return &contracts.ProjectRegistrationFailure{Err: &repos.CheckError{Check: check, Path: request.Path, Err: err}}
	}
	explicitBase := request.BaseBranch != ""
	previous, previousErr := a.db.GetRepoByPath(ctx, clone.Path())
	if previousErr != nil && !errors.Is(previousErr, store.ErrNotFound) {
		return previousErr
	}
	if previousErr == nil {
		if explicitBase && request.BaseBranch != previous.BaseBranch {
			return errBaseChangeViaAdd(previous.BaseBranch, request.BaseBranch)
		}
		if request.BaseBranch == "" {
			request.BaseBranch = previous.BaseBranch
		}
		if request.WorktreeRoot == "" {
			request.WorktreeRoot = previous.WorktreeRoot
		}
		if request.FeaturePrefix == "" {
			request.FeaturePrefix = previous.FeaturePrefix
		}
	}

	registration, err := repos.Resolve(ctx, binary, request.Path, repos.Options{
		BaseBranch:    request.BaseBranch,
		WorktreeRoot:  request.WorktreeRoot,
		FeaturePrefix: request.FeaturePrefix,
	}, repos.Defaults{
		BaseBranch:    a.cfg.Settings.DefaultBaseBranch,
		WorktreeRoot:  a.cfg.Settings.WorktreeRoot,
		FeaturePrefix: a.cfg.Settings.FeaturePrefix,
	})
	if err != nil {
		return &contracts.ProjectRegistrationFailure{Err: err}
	}

	// AC-PAR-4.8, and it runs before the upsert so that a refused registration
	// leaves no row behind. internal/repos decides; the rows come from here,
	// because that package makes no query of its own.
	existing, err := a.db.ListRepos(ctx)
	if err != nil {
		return err
	}
	if err := repos.ValidateNewWorktreeRoot(registration, existing); err != nil {
		return &contracts.ProjectRegistrationFailure{Err: err}
	}

	// Whether the row already existed only changes the verb in the output, but
	// it is the difference between "I registered something" and "I changed
	// something you already had", and the user should not have to guess which
	// happened (AC-1.8).
	_, err = a.db.GetRepoByPath(ctx, registration.Path)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrNotFound):
	default:
		return err
	}
	existed := err == nil

	stored, err := a.db.RegisterRepo(ctx, registration.StoreRepo(), explicitBase)
	if err != nil {
		return registerRefusal(err, request.BaseBranch)
	}
	if stored.BaseBranch != registration.BaseBranch {
		// A base change committed after the earlier read; report what is
		// stored. The source label stays as resolved.
		registration.BaseBranch = stored.BaseBranch
	}

	if err := projectconnection.SeedRegistration(ctx, a.db, stored, *a.cfg); err != nil {
		return err
	}
	present.Registration(registrationView(registration), existed)

	// A webhook problem never undoes or fails the registration above: the repo
	// keeps working on polling.
	present.Webhook(repositoryView(stored), webhookView(a.ensureWebhook(ctx, stored)))
	return nil
}

// List returns the existing local administration inventory.
func (a *Administration) List(ctx context.Context) ([]contracts.RepositoryView, error) {
	rows, err := a.db.ListRepos(ctx)
	var out []contracts.RepositoryView
	for _, row := range rows {
		out = append(out, repositoryView(row))
	}
	return out, err
}

// registerRefusal maps a guarded registration write error. A base-change
// refusal names the base the store found at write time, which can differ from
// the earlier read when a `repo base` committed in between.
func registerRefusal(err error, requested string) error {
	var refusal *store.BaseChangeViaAddError
	if errors.As(err, &refusal) {
		return errBaseChangeViaAdd(refusal.Stored, requested)
	}
	return err
}
