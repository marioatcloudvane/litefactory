package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/secrets"
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
		repo, err := rt.db.GetRepoBySlug(ctx, string(naming.ForgeGitHub), owner, name)
		if errors.Is(err, store.ErrNotFound) {
			return Preconditionf("%s is not a registered repository; add it with `litefactory repo add <path>`", args[0])
		}
		if err != nil {
			return err
		}
		targets = []store.Repo{repo}
	}

	reg, setupFailure := newWebhookRegistrar(ctx, env, rt)
	failed := 0
	for _, repo := range targets {
		res := ensureWebhook(ctx, reg, setupFailure, rt, repo)
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

// newWebhookRegistrar builds the registrar for the configured account. It does
// nothing when webhook.public_url is empty, so no keychain item is read for a
// setup without webhooks. A non-empty failure explains why none could be built.
func newWebhookRegistrar(ctx context.Context, env *Env, rt *repoRuntime) (*webhookreg.Registrar, string) {
	publicURL := rt.cfg.Webhook.PublicURL
	if publicURL == "" {
		return &webhookreg.Registrar{}, ""
	}
	ref, configured := secrets.ReferenceFrom(
		rt.cfg.Providers.GitHub.TokenKeychain.Service, rt.cfg.Providers.GitHub.TokenKeychain.Account, rt.cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return nil, "no bot token is configured; set github.token_keychain or github.token_env"
	}
	token, err := secrets.Resolver{Keychain: env.Keychain}.Resolve(ctx, ref)
	if err != nil {
		return nil, err.Error()
	}
	client, err := github.New(github.Options{
		APIBase:   rt.cfg.Providers.GitHub.APIBase,
		Token:     token,
		Logger:    env.Logger,
		UserAgent: initUserAgent(env),
	})
	if err != nil {
		return nil, err.Error()
	}
	return &webhookreg.Registrar{
		Forge: client,
		Keychain: webhookreg.SystemKeychain{
			Resolver: secrets.Resolver{Keychain: env.Keychain},
			Writer:   secrets.Writer{Keychain: env.KeychainWrite},
		},
		Store:     rt.db,
		PublicURL: publicURL,
		Now:       func() time.Time { return time.Now().UTC() },
	}, ""
}

// ensureWebhook runs one registration, or reports why it could not start.
func ensureWebhook(ctx context.Context, reg *webhookreg.Registrar, setupFailure string, rt *repoRuntime, repo store.Repo) webhookreg.Result {
	if rt.cfg.Webhook.PublicURL == "" {
		return webhookreg.Result{Outcome: webhookreg.Skipped, Reason: "webhook.public_url is not set in config.yaml; set it to register a webhook (the repository keeps working on polling)"}
	}
	if reg == nil {
		return webhookreg.Result{Outcome: webhookreg.Failed, Reason: setupFailure}
	}
	return reg.Ensure(ctx, repo)
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
