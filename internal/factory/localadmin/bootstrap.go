// Package localadmin owns stopped-daemon installation and configuration operations.
package localadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/identity"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// BootstrapOptions supplies trusted local host dependencies and presentation.
type BootstrapOptions struct {
	Logger    *slog.Logger
	Keychain  func(context.Context, string, string) (string, error)
	UserAgent string
	Warning   func(contracts.BootstrapWarning)
}

func (o BootstrapOptions) warning(warning contracts.BootstrapWarning) {
	if o.Warning != nil {
		o.Warning(warning)
	}
}

// Bootstrap initializes local files and existing durable identities. Both
// identities are resolved and compared before either identity is stored.
func Bootstrap(ctx context.Context, options BootstrapOptions) (result contracts.BootstrapResult, err error) {
	paths, err := config.Locate()
	if err != nil {
		return result, err
	}
	if err = paths.EnsureHome(); err != nil {
		return result, err
	}

	// 1. config.yaml, never overwritten: it is the operator's file the moment
	// it exists, and re-running init must not silently discard their edits.
	writtenConfig, err := config.WriteDefault(paths)
	if err != nil {
		return result, err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return result, &contracts.BootstrapFailure{Kind: contracts.BootstrapConfigFailure, Err: err}
	}

	// 2. the database. Migrate is idempotent, so this is both "create it" and
	// "bring an older one up to date".
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return result, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	if err = db.Migrate(ctx); err != nil {
		return result, err
	}

	// 3. the owner, from gh. Fatal: an owner is the one thing litefactory
	// cannot invent, and without it the daemon has nobody to take orders from.
	owner, err := identity.ResolveOwner(ctx, identity.Options{})
	if err != nil {
		return result, &contracts.BootstrapFailure{Kind: contracts.BootstrapOwnerFailure, Err: err, Interrupted: ctx.Err() != nil}
	}

	// 4. the bot, from the API. Never fatal here — see resolveBotIdentity.
	bot, hasBot := resolveBotIdentity(ctx, options, paths, cfg)

	// 5. the self-instruction loop check, on numeric ids (D-I). Handles are
	// renameable labels; the id is the account. If the daemon posted as the
	// owner, every comment it wrote would come back as a valid instruction and
	// the first run would never end. Nothing has been written to the identities
	// table at this point, so refusing here stores neither of them.
	if hasBot && bot.ID == owner.UserID {
		return result, &contracts.BootstrapFailure{Kind: contracts.BootstrapSameAccount, Login: owner.Login, UserID: owner.UserID}
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
		return result, err
	}
	if hasBot {
		if _, err = db.UpsertIdentity(ctx, store.Identity{
			Forge:  store.ForgeGitHub,
			Role:   store.RoleBot,
			Handle: bot.Login,
			UserID: bot.ID,
			Source: store.SourceForgeAPI,
		}); err != nil {
			return result, err
		}
	}

	result = contracts.BootstrapResult{Home: paths.Home(), ConfigPath: paths.ConfigPath(), DBPath: paths.DBPath(), WrittenConfig: writtenConfig, OwnerLogin: owner.Login, OwnerID: owner.UserID, BotLogin: bot.Login, BotID: bot.ID, HasBot: hasBot, Completed: true}
	return result, nil
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
func resolveBotIdentity(ctx context.Context, options BootstrapOptions, paths config.Paths, cfg *config.Config) (github.User, bool) {
	ref, configured := secrets.ReferenceFrom(
		cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if !configured {
		options.warning(contracts.BootstrapWarning{Kind: contracts.BootstrapNoToken, ConfigPath: paths.ConfigPath()})
		return github.User{}, false
	}

	// The token lives in this function and in the client it builds, and it is a
	// secrets.Token throughout: every formatting verb on it prints [redacted],
	// so the warnings below cannot leak it however they are phrased (AC-0.11).
	token, err := secrets.Resolver{Keychain: options.Keychain}.Resolve(ctx, ref)
	if err != nil {
		// A keychain item that is merely absent is the fresh-machine state, not
		// a misconfiguration, and it gets the warning that says how to fix it.
		// Every other failure — a locked keychain, an empty value, an
		// unreadable environment variable — keeps the generic wording, because
		// the remedy for those is genuinely "look at what you configured".
		if ref.Source == secrets.SourceKeychain && errors.Is(err, secrets.ErrNotFound) {
			options.warning(contracts.BootstrapWarning{Kind: contracts.BootstrapKeychainMissing, ConfigPath: paths.ConfigPath(), Service: ref.Service, Account: ref.Account})
		} else {
			options.warning(contracts.BootstrapWarning{Kind: contracts.BootstrapTokenFailed, Err: err, ConfigPath: paths.ConfigPath()})
		}
		return github.User{}, false
	}

	client, err := github.New(github.Options{
		APIBase:   cfg.Providers.GitHub.APIBase,
		Token:     token,
		Logger:    options.Logger,
		UserAgent: options.UserAgent,
	})
	if err != nil {
		options.warning(contracts.BootstrapWarning{Kind: contracts.BootstrapTokenFailed, Err: err, ConfigPath: paths.ConfigPath()})
		return github.User{}, false
	}

	user, err := client.AuthenticatedUser(ctx)
	if err != nil {
		options.warning(contracts.BootstrapWarning{Kind: contracts.BootstrapTokenFailed, Err: fmt.Errorf("%s: %w", ref, err), ConfigPath: paths.ConfigPath()})
		return github.User{}, false
	}
	return user, true
}
