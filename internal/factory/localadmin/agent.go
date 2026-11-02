package localadmin

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/launchd"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

// AgentOptions are trusted host dependencies for local service lifecycle.
type AgentOptions struct {
	Level  string
	Runner launchd.Runner
	Logger *slog.Logger
}

// StartAgent validates configuration before changing the installed agent.
func StartAgent(ctx context.Context, options AgentOptions) (resultView contracts.AgentStartView, err error) {
	paths, err := config.Locate()
	if err != nil {
		return resultView, err
	}
	cfg, err := LoadConfig(paths)
	if err != nil {
		return resultView, err
	}
	program, err := AgentProgram(options.Level)
	if err != nil {
		return resultView, err
	}

	// PATH is materialised from the installing shell. This is one half of the
	// AC-2.2 fix: launchd would otherwise hand the daemon
	// /usr/bin:/bin:/usr/sbin:/sbin, which contains no Homebrew claude. The
	// other half is `daemon run` resolving its binaries at startup.
	//
	// LF_HOME is pinned to the home this command just read, so the daemon
	// cannot end up using a different one than the configuration it was
	// started from.
	environment := map[string]string{
		launchd.PathEnvVar: launchd.EffectivePATH(os.Getenv("PATH")),
		config.EnvHome:     paths.Home(),
	}
	credential, err := resolveDaemonCredential(cfg, paths, environment)
	if err != nil {
		return resultView, err
	}

	// launchd creates the redirect files but not the directory that holds
	// them, and a failed redirect kills the job before the daemon runs.
	//nolint:gosec // 0700 in practice: the process umask is 0o077 (see cmd/litefactory/main.go).
	if mkdirErr := os.MkdirAll(paths.LogsDir(), 0o777); mkdirErr != nil {
		return resultView, fmt.Errorf("create %s: %w", paths.LogsDir(), mkdirErr)
	}

	manager := &launchd.Manager{Runner: options.Runner, Logger: options.Logger}
	agent := manager.NewAgent(program, paths.LogsDir(), environment)
	result, err := manager.Install(ctx, agent)
	if err != nil {
		return resultView, err
	}

	return contracts.AgentStartView{Label: agent.Label, PlistPath: result.PlistPath, Domain: result.Domain, ProgramArguments: agent.ProgramArguments, Path: agent.Environment[launchd.PathEnvVar], Home: agent.Environment[config.EnvHome], StdoutPath: agent.StandardOutPath(), StderrPath: agent.StandardErrorPath(), ConfigPath: paths.ConfigPath(), Replaced: result.Replaced, Credential: credential}, nil
}

// StopAgent needs no configuration or database, even after broken startup.
func StopAgent(ctx context.Context, options AgentOptions) (contracts.AgentStopView, error) {
	manager := &launchd.Manager{Runner: options.Runner, Logger: options.Logger}
	unloaded, err := manager.Uninstall(ctx)
	if err != nil {
		return contracts.AgentStopView{}, err
	}
	path, err := manager.PlistPath()
	if err != nil {
		return contracts.AgentStopView{}, err
	}
	return contracts.AgentStopView{Unloaded: unloaded, Target: manager.ServiceTarget(), Domain: manager.Domain(), PlistPath: path}, nil
}

// AgentStatus reports the existing launchd process observation.
func AgentStatus(ctx context.Context, options AgentOptions) (contracts.AgentStatusView, error) {
	manager := &launchd.Manager{Runner: options.Runner, Logger: options.Logger}
	status, err := manager.Status(ctx)
	return contracts.AgentStatusView(status), err
}

// LastTickTime reads and releases the saved poll cursor without holding a
// database connection while the status consumer renders output.
func LastTickTime(ctx context.Context, paths config.Paths) (_ time.Time, err error) {
	if _, statErr := os.Stat(paths.DBPath()); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return time.Time{}, &contracts.LocalAdminUnavailable{Err: fmt.Errorf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())}
		}
		return time.Time{}, fmt.Errorf("read %s: %w", paths.DBPath(), statErr)
	}

	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return time.Time{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	cursor, err := db.GetPollCursor(ctx)
	if err != nil {
		return time.Time{}, err
	}
	return cursor.LastTickAt, nil
}

func resolveDaemonCredential(cfg *config.Config, paths config.Paths, environment map[string]string) (contracts.AgentCredentialView, error) {
	ref, configured := secrets.ReferenceFrom(
		cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv)
	if !configured {
		return contracts.AgentCredentialView{}, nil
	}
	if ref.Source != secrets.SourceEnv {
		return contracts.AgentCredentialView{Description: ref.String()}, nil
	}

	value, found := os.LookupEnv(ref.EnvVar)
	if !found || strings.TrimSpace(value) == "" {
		// Refusing here is not the same as refusing token_env. The variable is
		// the configured reference and it resolves to nothing, so installing
		// the agent would produce a daemon that cannot authenticate and no
		// terminal to say so in (S-5).
		return contracts.AgentCredentialView{}, &contracts.LocalAdminUnavailable{Err: fmt.Errorf(
			"github.token_env names $%s, but that variable is not set in this shell; a launchd agent inherits no shell environment, so `daemon start` has to copy the value into the plist\n"+
				"  export it and re-run, or point %s at github.token_keychain instead",
			ref.EnvVar, paths.ConfigPath())}
	}
	environment[ref.EnvVar] = value
	return contracts.AgentCredentialView{Description: ref.String(), EnvName: ref.EnvVar}, nil
}

// LoadConfig preserves the existing local setup error distinction.
func LoadConfig(paths config.Paths) (*config.Config, error) {
	cfg, err := config.Load(paths)
	if err != nil {
		var notInitialized *config.NotInitializedError
		if errors.As(err, &notInitialized) || errors.Is(err, config.ErrLiteralSecret) {
			return nil, &contracts.LocalAdminUnavailable{Err: err}
		}
		return nil, err
	}
	return cfg, nil
}

// AgentProgram is the command the LaunchAgent execs: this binary, followed by
// `daemon run`, followed by `--level <keyword>` when level is set (US-6,
// AC-6.3).
//
// The path is resolved through its symlinks because os.Executable may hand
// back the link the process was started through. A plist pointing at a
// Homebrew shim that is later re-pointed would silently start a different
// binary at the next login, which is not a failure anyone would think to look
// for.
//
// level is appended untouched: Agent.Validate's checkPlistValue already
// scans every ProgramArguments element for non-UTF-8 and control characters
// before it reaches the plist XML, and it was validated against
// logging.ParseLevel by the caller, so no second sanitisation pass belongs
// here (D-5).
func AgentProgram(level string) ([]string, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve this binary's path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, fmt.Errorf("resolve the symlinks of %s: %w", exe, err)
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve %s to an absolute path: %w", resolved, err)
	}
	program := []string{abs, "daemon", "run"}
	if level != "" {
		program = append(program, "--level", level)
	}
	return program, nil
}
