package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// StoryUpgradeOptions supplies trusted provider and retained-routing composition.
type StoryUpgradeOptions struct {
	Logger    *slog.Logger
	Keychain  func(context.Context, string, string) (string, error)
	UserAgent string
	Selector  func(*store.DB, *projectconnection.Factory) turn.TicketConnectionSelector
}

// PreviewStoryUpgrade previews historical contract changes without migrating state.
func PreviewStoryUpgrade(ctx context.Context, ref naming.Ref, options StoryUpgradeOptions, present contracts.StoryUpgradePresentation) error {
	return storyUpgrade(ctx, ref, false, options, present)
}

// ApplyStoryUpgrade applies or resumes the retained revision-bound contract upgrade.
func ApplyStoryUpgrade(ctx context.Context, ref naming.Ref, options StoryUpgradeOptions, present contracts.StoryUpgradePresentation) error {
	return storyUpgrade(ctx, ref, true, options, present)
}
func maintenanceUnavailablef(format string, args ...any) error {
	return &contracts.LocalMaintenanceUnavailable{Err: fmt.Errorf(format, args...)}
}
func storyUpgrade(ctx context.Context, ref naming.Ref, apply bool, options StoryUpgradeOptions, present contracts.StoryUpgradePresentation) (err error) {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.DBPath()); err != nil {
		return maintenanceUnavailablef("existing litefactory database required: %v", err)
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	// Preview deliberately does not migrate: even schema changes are deferred
	// until explicit apply or the daemon's normal startup migration.
	if apply {
		if err := db.Migrate(ctx); err != nil {
			return err
		}
	}
	ids, err := turn.LoadIdentities(ctx, db, store.ForgeGitHub)
	if err != nil {
		return maintenanceUnavailablef("configured numeric owner and bot identities required: %v", err)
	}
	ctx, ticketWorkflow, sharedRedactor, err := storyContractTicketWorkflow(ctx, options, db, *cfg, ref, ids.BotUserID)
	if err != nil {
		return err
	}
	renderer, err := report.New(report.Options{Redactor: sharedRedactor, Limit: planbrief.MaxStoryBodyBytes})
	if err != nil {
		return err
	}
	scope, retained, err := turn.RetainedPlanContentScope(ctx, db, ref.String())
	if err != nil {
		return err
	}
	if !retained {
		scope = planbrief.TaskOwnerScope()
	} // This command upgrades explicit historical contracts.
	o := turn.StoryContractUpgradeOptions{DB: db, Forge: ticketWorkflow, Renderer: renderer, Parent: ref, OwnerID: ids.OwnerUserID, Scope: scope, LockDir: filepath.Dir(paths.DBPath())}
	p, err := turn.PreviewStoryContractUpgrade(ctx, o)
	if err != nil {
		return err
	}
	view := contracts.StoryUpgradeView{Parent: p.Parent, SourceGeneration: p.SourceGeneration, TargetGeneration: p.TargetGeneration, FromContract: p.FromContract, ToContract: p.ToContract, State: p.State, Conflicts: append([]string(nil), p.Conflicts...)}
	for _, item := range p.Items {
		view.Items = append(view.Items, contracts.StoryUpgradeItemView{Story: item.Story, IssueNumber: item.IssueNumber, ObservedRevision: item.ObservedRevision, ExpectedRevision: item.ExpectedRevision, DesiredRevision: item.DesiredRevision})
	}
	if err := present.Preview(view); err != nil {
		return err
	}
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("upgrade remains pending: %s", strings.Join(p.Conflicts, "; "))
	}
	if !apply {
		return present.PreviewOnly()
	}
	if err := turn.ApplyStoryContractUpgrade(ctx, o, p); err != nil {
		return err
	}
	return present.Applied()
}

// storyContractTicketWorkflow keeps every managed-child operation in the
// parent's retained execution context. Preview never imports or admits work.
func storyContractTicketWorkflow(ctx context.Context, options StoryUpgradeOptions, db *store.DB, cfg config.Config, ref naming.Ref, botID int64) (context.Context, *turn.TicketWorkflowBridge, *redact.Redactor, error) {
	sharedRedactor := redact.New(redact.Options{Logger: options.Logger})
	factory := projectconnection.NewFactory(projectconnection.Options{DB: db, Resolver: secrets.Resolver{Keychain: options.Keychain}, Logger: options.Logger, Redactor: sharedRedactor, ExpectedActorID: botID})
	parent, err := db.GetSessionByTaskRef(ctx, ref.String())
	if err == nil {
		if _, err := db.GetExecutionSnapshot(ctx, parent.ID); err != nil {
			return ctx, nil, nil, maintenanceUnavailablef("parent execution connection must be reconciled before upgrade: %v", err)
		}
		ctx = turn.ExecutionContext(ctx, parent.ID)
		selector := options.Selector(db, factory)
		adapter, actorID, err := selector(ctx, github.RepoFromRef(ref), ref.Issue, parent.ID)
		if err != nil {
			return ctx, nil, nil, err
		}
		workflow, err := turn.NewRoutedGitHubTicketWorkflow(db, adapter, actorID, selector)
		return ctx, workflow, sharedRedactor, err
	}
	if !errors.Is(err, store.ErrNotFound) {
		return ctx, nil, nil, err
	}
	// Historical upgrade-only cohorts can predate session admission. Their
	// legacy bridge is valid only without competing saved execution/settings.
	repo, err := db.GetRepoBySlug(ctx, store.ForgeGitHub, ref.Owner, ref.Repo)
	if err != nil {
		return ctx, nil, nil, fmt.Errorf("upgrade repository must be registered: %w", err)
	}
	sessions, err := db.ListRepoSessions(ctx, repo.ID)
	if err != nil {
		return ctx, nil, nil, err
	}
	if len(sessions) != 0 {
		return ctx, nil, nil, maintenanceUnavailablef("parent execution connection required when the project has saved work")
	}
	connections, err := db.ListConnections(ctx, repo.ID)
	if err != nil {
		return ctx, nil, nil, err
	}
	origin, err := ticket.CanonicalOrigin(cfg.Providers.GitHub.APIBase)
	if err != nil {
		return ctx, nil, nil, err
	}
	for _, v := range connections {
		if v.Provenance != "legacy" || !v.Enabled || v.Destination != repo.Slug() || v.Origin != origin || v.CredentialService != cfg.Providers.GitHub.TokenKeychain.Service || v.CredentialAccount != cfg.Providers.GitHub.TokenKeychain.Account || v.CredentialEnv != cfg.Providers.GitHub.TokenEnv {
			return ctx, nil, nil, maintenanceUnavailablef("parent execution connection required when project connections have changed")
		}
	}
	tokenRef, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return ctx, nil, nil, maintenanceUnavailablef("configured bot credential required")
	}
	token, err := (secrets.Resolver{Keychain: options.Keychain}).Resolve(ctx, tokenRef)
	if err != nil {
		return ctx, nil, nil, err
	}
	sharedRedactor.AddToken(token)
	forge, err := github.New(github.Options{APIBase: cfg.Providers.GitHub.APIBase, Token: token, Logger: options.Logger, UserAgent: options.UserAgent})
	if err != nil {
		return ctx, nil, nil, err
	}
	bot, err := forge.AuthenticatedUser(ctx)
	if err != nil {
		return ctx, nil, nil, err
	}
	if bot.ID != botID {
		return ctx, nil, nil, maintenanceUnavailablef("configured credential does not match the stored numeric bot identity")
	}
	adapter, err := github.NewTickets(forge)
	if err != nil {
		return ctx, nil, nil, err
	}
	workflow, err := turn.NewTicketWorkflow(ctx, db, adapter, botID)
	return ctx, workflow, sharedRedactor, err
}
