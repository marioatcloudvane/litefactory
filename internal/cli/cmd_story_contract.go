package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func newStoryContractUpgradeCommand() *Command {
	return &Command{Name: "story-contract upgrade", Usage: "story-contract upgrade <parent-ref> [--preview | --apply]", Short: "Preview or explicitly upgrade unfinished legacy story contracts", Long: "Preview is read-only and the default. --apply retains an exact revision-bound unfinished cohort, refuses active/frozen scope or ambiguous conversion, and can resume partial publication. Completed children and historical acceptance remain unchanged. Successful upgrade still requires the next published and approved Plan.", Args: ArgSpec{Names: []string{"parent-ref"}, Min: 1}, New: func(fs *flag.FlagSet) Handler {
		var preview, apply bool
		fs.BoolVar(&preview, "preview", false, "show revisions and conflicts without mutation (default)")
		fs.BoolVar(&apply, "apply", false, "explicitly apply or resume the retained upgrade")
		return func(ctx context.Context, env *Env, args []string) error {
			if preview && apply {
				return Usagef(nil, "--preview and --apply are mutually exclusive")
			}
			return runStoryContractUpgrade(ctx, env, args[0], apply)
		}
	}}
}

func runStoryContractUpgrade(ctx context.Context, env *Env, parent string, apply bool) (err error) {
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return Usagef(nil, "parent-ref must identify a GitHub parent issue: %v", err)
	}
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	if _, err := os.Stat(paths.DBPath()); err != nil {
		return Preconditionf("existing litefactory database required: %v", err)
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
		return Preconditionf("configured numeric owner and bot identities required: %v", err)
	}
	_, err = db.GetRepoBySlug(ctx, store.ForgeGitHub, ref.Owner, ref.Repo)
	if err != nil {
		return fmt.Errorf("upgrade repository must be registered: %w", err)
	}
	tokenRef, configured := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return Preconditionf("configured bot credential required")
	}
	token, err := (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, tokenRef)
	if err != nil {
		return err
	}
	forge, err := github.New(github.Options{APIBase: cfg.Providers.GitHub.APIBase, Token: token, Logger: env.Logger, UserAgent: initUserAgent(env)})
	if err != nil {
		return err
	}
	bot, err := forge.AuthenticatedUser(ctx)
	if err != nil {
		return err
	}
	if bot.ID != ids.BotUserID {
		return Preconditionf("configured credential does not match the stored numeric bot identity")
	}
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{}), Limit: planbrief.MaxStoryBodyBytes})
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
	o := turn.StoryContractUpgradeOptions{DB: db, Forge: forge, Renderer: renderer, Parent: ref, OwnerID: ids.OwnerUserID, Scope: scope, LockDir: filepath.Dir(paths.DBPath())}
	p, err := turn.PreviewStoryContractUpgrade(ctx, o)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(env.Stdout, "%s: content/submission contract %d -> %d; generation %d -> %d; %s\n", p.Parent, p.FromContract, p.ToContract, p.SourceGeneration, p.TargetGeneration, p.State); err != nil {
		return err
	}
	for _, item := range p.Items {
		if _, err := fmt.Fprintf(env.Stdout, "%s #%d observed=%s expected=%s target=%s\n", item.Story, item.IssueNumber, item.ObservedRevision, item.ExpectedRevision, item.DesiredRevision); err != nil {
			return err
		}
	}
	for _, conflict := range p.Conflicts {
		if _, err := fmt.Fprintf(env.Stdout, "Conflict: %s\n", conflict); err != nil {
			return err
		}
	}
	if len(p.Conflicts) > 0 {
		return fmt.Errorf("upgrade remains pending: %s", strings.Join(p.Conflicts, "; "))
	}
	if !apply {
		_, err = fmt.Fprintln(env.Stdout, "Preview only. Use --apply to publish this revision-bound cohort. Completed stories retain their historical contract.")
		return err
	}
	if err := turn.ApplyStoryContractUpgrade(ctx, o, p); err != nil {
		return err
	}
	_, err = fmt.Fprintln(env.Stdout, "Upgrade recorded. The next published and approved Plan is required before engineering; workflow and historical acceptance are unchanged.")
	return err
}
