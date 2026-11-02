package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/maintenance"
	"github.com/cloudvane/litefactory/internal/naming"
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

func runStoryContractUpgrade(ctx context.Context, env *Env, parent string, apply bool) error {
	ref, err := naming.ParseRef(parent)
	if err != nil {
		return Usagef(nil, "parent-ref must identify a GitHub parent issue: %v", err)
	}
	options := maintenance.StoryUpgradeOptions{Logger: env.Logger, Keychain: env.Keychain, UserAgent: initUserAgent(env), Selector: daemon.ProjectTicketSelector}
	present := contracts.StoryUpgradePresentation{
		Preview: func(p contracts.StoryUpgradeView) error {
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
			return nil
		},
		PreviewOnly: func() error {
			_, err := fmt.Fprintln(env.Stdout, "Preview only. Use --apply to publish this revision-bound cohort. Completed stories retain their historical contract.")
			return err
		},
		Applied: func() error {
			_, err := fmt.Fprintln(env.Stdout, "Upgrade recorded. The next published and approved Plan is required before engineering; workflow and historical acceptance are unchanged.")
			return err
		},
	}
	if apply {
		err = maintenance.ApplyStoryUpgrade(ctx, ref, options, present)
	} else {
		err = maintenance.PreviewStoryUpgrade(ctx, ref, options, present)
	}
	var unavailable *contracts.LocalMaintenanceUnavailable
	if errors.As(err, &unavailable) {
		return Precondition(err)
	}
	return err
}
