package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/identity"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// initBotUnknown is printed in the bot row when no bot identity could be
// resolved. It is a normal state on a machine that has just been initialised
// (AC-0.5), not an error.
const initBotUnknown = "not configured"

// The two warnings `init` can end on. Both name what is still missing and how
// to supply it: an operator who is told only that something is wrong has to
// guess, and the guess is usually "run it again".
const (
	initNoTokenWarning = `warning: no bot token is configured, so litefactory does not know which account it posts as.
  Store one in the keychain and reference it in %s:
      security add-generic-password -s litefactory -a github-bot -w
      github:
        token_keychain:
          service: "litefactory"
          account: "github-bot"
  Then run ` + "`litefactory init`" + ` again. The daemon refuses to start until this resolves.
`

	initTokenFailedWarning = `warning: the bot identity could not be resolved: %v
  Everything else was written; the bot identity is the only thing missing.
  Fix the reference in %s and run ` + "`litefactory init`" + ` again. The daemon refuses to start until this resolves.
`
)

// newInitCommand declares "litefactory init".
func newInitCommand() *Command {
	return &Command{
		Name:  "init",
		Usage: "init",
		Short: "Create $LF_HOME, the config file and the database, and derive identities",
		Long: "Bootstrap litefactory on this machine: create $LF_HOME, write a default\n" +
			"config.yaml, create the database, and derive the owner and bot identities\n" +
			"from the git tooling already logged in here.\n\n" +
			"The owner comes from `gh`, which must be logged in to github.com. The bot\n" +
			"comes from the GitHub API, under the token config.yaml points at; without\n" +
			"one, init warns and everything else still happens.\n\n" +
			"Running it again is safe: it refreshes both identities, creates no duplicate\n" +
			"rows, and never overwrites an existing config.yaml.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runInit(ctx, env)
			}
		},
	}
}

// runInit bootstraps this machine (AC-0.1 … AC-0.7).
//
// The order is deliberate and the identity writes come last. Both identities
// are resolved and compared before either is stored, so the bot == owner
// refusal in step 5 leaves the identities table exactly as it found it — an
// `init` that stored the owner and then refused would leave a half-configured
// machine behind (AC-0.6).
func runInit(ctx context.Context, env *Env) (err error) {
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	if err = paths.EnsureHome(); err != nil {
		return err
	}

	// 1. config.yaml, never overwritten: it is the operator's file the moment
	// it exists, and re-running init must not silently discard their edits.
	writtenConfig, err := config.WriteDefault(paths)
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return initConfigError(err)
	}

	// 2. the database. Migrate is idempotent, so this is both "create it" and
	// "bring an older one up to date".
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err = db.Migrate(ctx); err != nil {
		return err
	}

	// 3. the owner, from gh. Fatal: an owner is the one thing litefactory
	// cannot invent, and without it the daemon has nobody to take orders from.
	owner, err := identity.ResolveOwner(ctx, identity.Options{})
	if err != nil {
		return initIdentityError(ctx, err)
	}

	// 4. the bot, from the API. Never fatal here — see resolveBotIdentity.
	bot, hasBot := resolveBotIdentity(ctx, env, paths, cfg)

	// 5. the self-instruction loop check, on numeric ids (D-I). Handles are
	// renameable labels; the id is the account. If the daemon posted as the
	// owner, every comment it wrote would come back as a valid instruction and
	// the first run would never end. Nothing has been written to the identities
	// table at this point, so refusing here stores neither of them.
	if hasBot && bot.ID == owner.UserID {
		return Preconditionf(
			"the configured bot token belongs to the same account as the owner: %s (id %d).\n"+
				"  litefactory would read its own comments as instructions and loop forever on the first run.\n"+
				"  Point github.token_keychain or github.token_env at a separate machine or bot account.\n"+
				"  Neither identity was stored",
			owner.Login, owner.UserID)
	}

	// 6. store both. Upserts on (forge, role), so a re-run refreshes the
	// handles and ids in place and creates no duplicate row (AC-0.7).
	if _, err = db.UpsertIdentity(ctx, store.Identity{
		Forge:  store.ForgeGitHub,
		Role:   store.RoleOwner,
		Handle: owner.Login,
		UserID: owner.UserID,
		Source: store.SourceGH,
	}); err != nil {
		return err
	}
	if hasBot {
		if _, err = db.UpsertIdentity(ctx, store.Identity{
			Forge:  store.ForgeGitHub,
			Role:   store.RoleBot,
			Handle: bot.Login,
			UserID: bot.ID,
			Source: store.SourceForgeAPI,
		}); err != nil {
			return err
		}
	}

	writeInitSummary(env.Stdout, paths, writtenConfig, owner, bot, hasBot)
	return nil
}

// resolveBotIdentity resolves the account litefactory posts as, from the token
// config.yaml points at (AC-0.5).
//
// Nothing here is fatal, and that is a decision rather than an oversight. `init`
// is the bootstrap: on a fresh machine there is no token yet, and on a machine
// where the reference is wrong the useful outcome is still "$LF_HOME, the
// database and the owner exist, and here is precisely what is missing". The
// daemon is where a missing or unresolvable bot token refuses to start (S-5),
// and it says so in every warning below. Failing here instead would mean a
// transient API error left the machine without an owner identity either.
func resolveBotIdentity(ctx context.Context, env *Env, paths config.Paths, cfg *config.Config) (github.User, bool) {
	ref, configured := secrets.ReferenceFrom(
		cfg.GitHub.TokenKeychain.Service, cfg.GitHub.TokenKeychain.Account, cfg.GitHub.TokenEnv)
	if !configured {
		fmt.Fprintf(env.Stderr, initNoTokenWarning, paths.ConfigPath())
		return github.User{}, false
	}

	// The token lives in this function and in the client it builds, and it is a
	// secrets.Token throughout: every formatting verb on it prints [redacted],
	// so the warnings below cannot leak it however they are phrased (AC-0.11).
	token, err := secrets.Resolver{}.Resolve(ctx, ref)
	if err != nil {
		fmt.Fprintf(env.Stderr, initTokenFailedWarning, err, paths.ConfigPath())
		return github.User{}, false
	}

	client, err := github.New(github.Options{
		APIBase:   cfg.GitHub.APIBase,
		Token:     token,
		Logger:    env.Logger,
		UserAgent: initUserAgent(env),
	})
	if err != nil {
		fmt.Fprintf(env.Stderr, initTokenFailedWarning, err, paths.ConfigPath())
		return github.User{}, false
	}

	user, err := client.AuthenticatedUser(ctx)
	if err != nil {
		fmt.Fprintf(env.Stderr, initTokenFailedWarning,
			fmt.Errorf("%s: %w", ref, err), paths.ConfigPath())
		return github.User{}, false
	}
	return user, true
}

// initUserAgent identifies this build to GitHub. GitHub requires a User-Agent
// and a version in it is what makes a support conversation about one machine
// possible.
func initUserAgent(env *Env) string {
	if env.Version == "" {
		return github.DefaultUserAgent
	}
	return github.DefaultUserAgent + "/" + env.Version
}

// writeInitSummary reports what now exists on disk and who litefactory thinks
// it is. The owner row carries the `github: <handle>` echo of AC-0.3.
func writeInitSummary(
	out io.Writer, paths config.Paths, writtenConfig bool, owner identity.Owner, bot github.User, hasBot bool,
) {
	fmt.Fprintf(out, "initialized %s\n", paths.Home())

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  config\t%s (%s)\n", paths.ConfigPath(), initConfigState(writtenConfig))
	fmt.Fprintf(tw, "  database\t%s\n", paths.DBPath())
	fmt.Fprintf(tw, "  owner\tgithub: %s (id %d, via gh)\n", owner.Login, owner.UserID)
	if hasBot {
		fmt.Fprintf(tw, "  bot\tgithub: %s (id %d, via the GitHub API)\n", bot.Login, bot.ID)
	} else {
		fmt.Fprintf(tw, "  bot\t%s\n", initBotUnknown)
	}
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()
}

// initConfigState names what happened to config.yaml, because "written" and
// "left exactly as you had it" are the difference between a first run and a
// re-run, and the operator should not have to diff the file to find out which.
func initConfigState(written bool) string {
	if written {
		return "created"
	}
	return "kept, already present"
}

// initConfigError maps a configuration failure to an exit code, matching
// openRepoRuntime so that every command reports the same file the same way.
//
// A literal credential in config.yaml is exit 3 (AC-0.9); so is a config.yaml
// that vanished between being written and being read. Everything else — an
// unknown key (AC-0.10), a value out of range — is a plain failure: the file is
// wrong rather than the machine.
func initConfigError(err error) error {
	if errors.As(err, new(*config.NotInitializedError)) || errors.Is(err, config.ErrLiteralSecret) {
		return Precondition(err)
	}
	return err
}

// initIdentityError maps an owner-discovery failure to an exit code (AC-0.4).
//
// Every way gh can fail — absent, logged out, logged in to the wrong host,
// exiting non-zero, hanging until the deadline — is the machine not being ready
// rather than the command being wrong, so all of them are exit 3. The wrapped
// error carries the exact command and its stderr.
//
// The one exception is the operator interrupting us, which is not a
// precondition and is read from the context rather than guessed at from the
// error: a cancelled gh and a gh that timed out both surface as a context error
// on the way out.
func initIdentityError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	return Precondition(fmt.Errorf("derive the owner identity: %w", err))
}
