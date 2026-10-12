package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
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

	var targets []store.Repo
	if all {
		if targets, err = rt.db.ListRepos(ctx); err != nil {
			return err
		}
		if len(targets) == 0 {
			fmt.Fprintf(env.Stdout, "no repositories registered; add one with `litefactory repo add <path>`\n")
			return nil
		}
	} else {
		owner, name, ok := strings.Cut(args[0], "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return Usagef(cmd, "%q is not an owner/name", args[0])
		}
		repos, listErr := rt.db.ListRepos(ctx)
		if listErr != nil {
			return listErr
		}
		matches := 0
		for _, candidate := range repos {
			if candidate.Forge == string(naming.ForgeGitHub) && strings.EqualFold(candidate.Owner, owner) && strings.EqualFold(candidate.Name, name) {
				matches++
			}
		}
		if matches > 1 {
			return errors.New("repository shortcut is ambiguous; use project and subscription IDs")
		}
		repo, err := rt.db.GetRepoBySlug(ctx, string(naming.ForgeGitHub), owner, name)
		if errors.Is(err, store.ErrNotFound) {
			return Preconditionf("%s is not a registered repository; add it with `litefactory repo add <path>`", args[0])
		}
		if err != nil {
			return err
		}
		targets = []store.Repo{repo}
	}

	failed := 0
	for _, repo := range targets {
		res := ensureWebhook(ctx, env, rt, repo)
		fmt.Fprintf(env.Stdout, "%s: %s\n", repo.Slug(), res.Describe())
		if res.Insecure && res.OK() {
			fmt.Fprintf(env.Stderr, "warning: webhook.public_url is plain http, so %s payloads travel unencrypted; use https.\n", repo.Slug())
		}
		if res.Outcome == webhookreg.Failed || res.Outcome == webhookreg.Skipped {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d repositories could not be fixed", failed, len(targets))
	}
	return nil
}

// ensureWebhook runs one registration, or reports why it could not start.
func ensureWebhook(ctx context.Context, env *Env, rt *repoRuntime, repo store.Repo) webhookreg.Result {
	if rt.cfg.Webhook.PublicURL == "" {
		return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook.public_url is not set in config.yaml; set it to register a webhook (the repository keeps working on polling)"}
	}
	if err := projectconnection.SeedRegistration(ctx, rt.db, repo, *rt.cfg); err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection could not be loaded"}
	}
	version, err := rt.db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
	if err != nil || !version.Enabled {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection is unavailable or disabled"}
	}
	qualified, err := newProjectSubscriptions(ctx, env, rt, version)
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket connection authentication failed; inspect connection credentials"}
	}
	subscriptions, err := rt.db.ListTicketSubscriptions(ctx, repo.ID)
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "subscription intent could not be read"}
	}
	var owned []store.TicketSubscription
	for _, subscription := range subscriptions {
		if subscription.ConnectionVersionID == version.ID {
			owned = append(owned, subscription)
		}
	}
	if len(owned) > 1 {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "subscription selection is ambiguous; use project and subscription IDs"}
	}
	var saved store.TicketSubscription
	outcome := webhookreg.AlreadyCorrect
	if len(owned) == 1 {
		saved, err = qualified.Repair(ctx, owned[0].ID, owned[0].Revision)
	} else {
		containerID, verifyErr := verifySubscriptionContainer(ctx, qualified, version)
		if verifyErr != nil {
			return webhookreg.Result{Outcome: webhookreg.Failed, Reason: "ticket source repository could not be verified; inspect provider access"}
		}
		// Credential edits retain source disable intent. Ordinary compatibility
		// repair must not create another enabled hook for that immutable destination.
		for _, subscription := range subscriptions {
			if !subscription.DesiredEnabled && subscription.RepoID == repo.ID && subscription.Provider == version.Provider && subscription.Origin == version.Origin && subscription.Container == version.Destination && subscription.ContainerID == containerID {
				return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook is explicitly disabled for this ticket source; polling remains active"}
			}
		}
		saved, err = rt.db.ImportLegacyTicketSubscription(ctx, version, containerID)
		if errors.Is(err, store.ErrNotFound) {
			saved, err = qualified.Create(ctx, version, containerID)
			outcome = webhookreg.Created
		} else if err == nil {
			saved, err = qualified.Repair(ctx, saved.ID, saved.Revision)
			outcome = webhookreg.Updated
		}
	}
	if err != nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: describeSubscriptionCLIError(repo, err)}
	}
	if !saved.DesiredEnabled {
		return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook is explicitly disabled; polling remains active"}
	}
	hookID, _ := strconv.ParseInt(saved.RemoteHookID, 10, 64)
	return webhookreg.Result{Outcome: outcome, HookID: hookID, Insecure: strings.HasPrefix(saved.CallbackURL, "http://")}
}

// writeRepoAddWebhook prints what `repo add` did about the webhook. A problem
// is a warning with a next step; it never fails the registration. A skipped
// registration (no webhook.public_url) is the default state, so it is reported
// on the result line rather than as a warning.
func writeRepoAddWebhook(out, errOut io.Writer, repo store.Repo, res webhookreg.Result) {
	fmt.Fprintf(out, "  webhook\t%s\n", res.Describe())
	fmt.Fprint(out, repoWebhookAdvice)
	if res.Outcome == webhookreg.Failed {
		fmt.Fprintf(errOut, "warning: no webhook for %s: %s\n  Next step: fix the cause, then run `litefactory repo webhook %s`.\n",
			repo.Slug(), res.Reason, repo.Slug())
	}
	if res.Insecure && res.OK() {
		fmt.Fprintf(errOut, "warning: webhook.public_url is plain http, so %s payloads travel unencrypted; use https.\n", repo.Slug())
	}
}

func describeSubscriptionCLIError(repo store.Repo, err error) string {
	if strings.Contains(err.Error(), "webhook operation") {
		return fmt.Sprintf("the bot account cannot manage webhooks on %s; check admin rights or unresolved operation status. The repository keeps working on polling.", repo.Slug())
	}
	return err.Error()
}
