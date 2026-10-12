package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func newTaskRetireCommand() *Command {
	return &Command{Name: "task retire", Usage: "task retire", Short: "Preview retirement of registered task resources", Long: "Show advisory retirement decisions for registered task worktrees and branches.\nFetches fresh remote refs and checks each issue directly. Nothing is deleted,\nownership is retained, and the automatic sweep schedule is unchanged.\nResults must be verified again before any later removal.", New: func(_ *flag.FlagSet) Handler { return runTaskRetire }}
}

func runTaskRetire(ctx context.Context, env *Env, _ []string) (err error) {
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	reference, configured := secrets.ReferenceFrom(rt.cfg.Providers.GitHub.TokenKeychain.Service, rt.cfg.Providers.GitHub.TokenKeychain.Account, rt.cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return Preconditionf("configured bot credential required")
	}
	token, err := (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, reference)
	if err != nil {
		return err
	}
	forge, err := github.New(github.Options{APIBase: rt.cfg.Providers.GitHub.APIBase, Token: token, Logger: env.Logger, UserAgent: initUserAgent(env)})
	if err != nil {
		return err
	}
	tickets, err := github.NewTickets(forge)
	if err != nil {
		return err
	}
	// Retirement only reads tickets; no bot mutation authority is needed.
	issueReader := &turn.TicketPlanBridge{DB: rt.db, Tickets: tickets}
	return writeRetirementPreview(ctx, env, rt.db, daemon.RetirementOptions{Now: time.Now().UTC(), RetainDays: rt.cfg.Settings.WorktreeRetainDays, Issues: issueReader, ObserveGit: daemon.ObserveRetirementGit})
}

// The bounded store interface keeps preview testable without starting a daemon.
type retirementInventory interface {
	RetirementBoundary(context.Context) (int64, error)
	ListRetirementOwners(context.Context, int64, int64, int) ([]store.RetirementOwner, error)
}

func writeRetirementPreview(ctx context.Context, env *Env, inventory retirementInventory, opts daemon.RetirementOptions) error {
	if _, err := fmt.Fprintln(env.Stdout, "Advisory retirement preview; nothing is deleted. Fresh checks are required before removal."); err != nil {
		return err
	}
	boundary, err := inventory.RetirementBoundary(ctx)
	if err != nil {
		return err
	}
	var cursor int64
	for {
		owners, err := inventory.ListRetirementOwners(ctx, cursor, boundary, 100)
		if err != nil {
			return err
		}
		if len(owners) == 0 {
			return nil
		}
		for _, owner := range owners {
			if err := ctx.Err(); err != nil {
				return err
			}
			result := daemon.EvaluateRetirement(ctx, opts, owner)
			if len(result.Decisions) == 0 {
				if _, err := fmt.Fprintf(env.Stdout, "%s\tkept\tno recorded resources\n", owner.Session.TaskRef); err != nil {
					return err
				}
			}
			for _, decision := range result.Decisions {
				if _, err := fmt.Fprintf(env.Stdout, "%s\t%s\t%q\t%s\t%s\n", owner.Session.TaskRef, decision.Kind, decision.Resource, decision.Action, decision.Reason); err != nil {
					return err
				}
			}
			// A rate limit must not cause a request for every remaining owner.
			var limited *github.RateLimitError
			if errors.As(result.Error, &limited) {
				return result.Error
			}
			cursor = owner.Session.ID
		}
	}
}
