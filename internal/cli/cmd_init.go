package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localadmin"
	"github.com/cloudvane/litefactory/internal/github"
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
      providers:
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

	// initKeychainMissingWarning covers the case the generated config.yaml
	// creates: the reference is present and correct, and the keychain item it
	// names simply does not exist yet.
	//
	// Before the generated file named a reference (US-8/AC-8.3), a fresh
	// machine ended on [initNoTokenWarning], which carries the
	// `security add-generic-password` line. Now it ends here instead, and
	// [initTokenFailedWarning]'s "fix the reference" is advice for a reference
	// that is not broken — it would send a new operator to edit a file that is
	// already right. The service and account are the configured names rather
	// than fixed text, so the command can be pasted as printed; they are
	// identifiers, never the credential.
	initKeychainMissingWarning = `warning: no bot token is stored yet, so litefactory does not know which account it posts as.
  The reference in %s is correct; the keychain item it names does not exist yet.
  Store the token, then run ` + "`litefactory init`" + ` again:
      security add-generic-password -s %s -a %s -w
  The daemon refuses to start until this resolves.
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
func runInit(ctx context.Context, env *Env) error {
	result, err := localadmin.Bootstrap(ctx, localadmin.BootstrapOptions{
		Logger: env.Logger, Keychain: env.Keychain, UserAgent: initUserAgent(env),
		Warning: func(warning contracts.BootstrapWarning) {
			switch warning.Kind {
			case contracts.BootstrapNoToken:
				fmt.Fprintf(env.Stderr, initNoTokenWarning, warning.ConfigPath)
			case contracts.BootstrapKeychainMissing:
				fmt.Fprintf(env.Stderr, initKeychainMissingWarning, warning.ConfigPath, warning.Service, warning.Account)
			case contracts.BootstrapTokenFailed:
				fmt.Fprintf(env.Stderr, initTokenFailedWarning, warning.Err, warning.ConfigPath)
			}
		},
	})
	if result.Completed {
		writeInitSummary(env.Stdout, result)
	}
	return initBootstrapError(err)
}

// initBootstrapError maps each owned failure before joining a possible close
// failure, preserving existing exit status and error ordering.
func initBootstrapError(err error) error {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		mapped := make([]error, 0, len(children))
		for _, child := range children {
			mapped = append(mapped, initBootstrapError(child))
		}
		return errors.Join(mapped...)
	}
	//nolint:errorlint // Map this exact child only; errors.As could absorb a separately ordered wrapped failure.
	failure, ok := err.(*contracts.BootstrapFailure)
	if !ok {
		return err
	}
	switch failure.Kind {
	case contracts.BootstrapConfigFailure:
		return initConfigError(failure.Err)
	case contracts.BootstrapOwnerFailure:
		if failure.Interrupted {
			return failure.Err
		}
		return Precondition(fmt.Errorf("derive the owner identity: %w", failure.Err))
	case contracts.BootstrapSameAccount:
		return Preconditionf(
			"the configured bot token belongs to the same account as the owner: %s (id %d).\n"+
				"  litefactory would read its own comments as instructions and loop forever on the first run.\n"+
				"  Point providers.github.token_keychain or providers.github.token_env at a separate machine or bot account.\n"+
				"  Neither identity was stored",
			failure.Login, failure.UserID)
	}
	return err
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
	out io.Writer, result contracts.BootstrapResult,
) {
	fmt.Fprintf(out, "initialized %s\n", result.Home)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  config\t%s (%s)\n", result.ConfigPath, initConfigState(result.WrittenConfig))
	fmt.Fprintf(tw, "  database\t%s\n", result.DBPath)
	fmt.Fprintf(tw, "  owner\tgithub: %s (id %d, via gh)\n", result.OwnerLogin, result.OwnerID)
	if result.HasBot {
		fmt.Fprintf(tw, "  bot\tgithub: %s (id %d, via the GitHub API)\n", result.BotLogin, result.BotID)
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
