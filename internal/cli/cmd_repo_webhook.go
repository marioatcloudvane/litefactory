package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"io"
)

// repoWebhookAdvice is printed after a registration. Webhooks are what make a
// repository react quickly; polling is the fallback.
const repoWebhookAdvice = "  webhooks are recommended: they make litefactory react to issues and comments within seconds.\n" +
	"  A short poll interval (settings.poll_interval_minutes) is for setups without a webhook.\n"

// newRepoWebhookCommand declares "litefactory repo webhook <owner/name> | --all".
func newRepoWebhookCommand() *Command {
	cmd := &Command{
		Name:  "repo webhook",
		Usage: "repo webhook <owner/name> | --all",
		Short: "Register or repair the GitHub webhook of a repository",
		Long: "Make sure the repository has the GitHub webhook litefactory's listener expects:\n" +
			"the webhook.public_url address, JSON payloads, the issues, issue_comment and\n" +
			"sub_issues events, and a secret kept in the Keychain. A hook that is already\n" +
			"correct is left alone; running it again changes nothing. Other hooks on the\n" +
			"repository are never touched.\n\n" +
			"One line is printed per repository. The exit status is non-zero when any\n" +
			"requested repository could not be fixed.",
		Args: ArgSpec{Names: []string{"owner/name"}},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var all bool
		fs.BoolVar(&all, "all", false, "register or repair every registered repository")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoWebhook(ctx, env, cmd, all, args)
		}
	}
	return cmd
}

func runRepoWebhook(ctx context.Context, env *Env, cmd *Command, all bool, args []string) (err error) {
	if all == (len(args) > 0) {
		return Usagef(cmd, "give either one <owner/name> or --all")
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	shortcut := ""
	if len(args) > 0 {
		shortcut = args[0]
	}
	err = projectAdministration(rt, env).RepairWebhooks(ctx, all, shortcut, contracts.ProjectWebhookPresentation{
		Empty: func() {
			fmt.Fprintf(env.Stdout, "no repositories registered; add one with `litefactory repo add <path>`\n")
		},
		Result: func(repository contracts.RepositoryView, result contracts.ProjectWebhookView) {
			fmt.Fprintf(env.Stdout, "%s: %s\n", repository.Slug, result.Description)
			if result.Insecure && result.OK {
				fmt.Fprintf(env.Stderr, "warning: webhook.public_url is plain http, so %s payloads travel unencrypted; use https.\n", repository.Slug)
			}
		},
	})
	return projectCommandError(cmd, err)
}

// writeRepoAddWebhook prints what `repo add` did about the webhook. A problem
// is a warning with a next step; it never fails the registration. A skipped
// registration (no webhook.public_url) is the default state, so it is reported
// on the result line rather than as a warning.
func writeRepoAddWebhook(out, errOut io.Writer, repo contracts.RepositoryView, res contracts.ProjectWebhookView) {
	fmt.Fprintf(out, "  webhook\t%s\n", res.Description)
	fmt.Fprint(out, repoWebhookAdvice)
	if res.Failed {
		fmt.Fprintf(errOut, "warning: no webhook for %s: %s\n  Next step: fix the cause, then run `litefactory repo webhook %s`.\n",
			repo.Slug, res.Reason, repo.Slug)
	}
	if res.Insecure && res.OK {
		fmt.Fprintf(errOut, "warning: webhook.public_url is plain http, so %s payloads travel unencrypted; use https.\n", repo.Slug)
	}
}
