package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func newMCPLogsCommand() *Command {
	return &Command{Name: "mcp logs", Usage: "mcp logs --repo forge:owner/name [--repo forge:owner/name]", Short: "Read bounded, redacted previous-run evidence over stdio", New: func(flags *flag.FlagSet) Handler {
		var repositories logRepositoryFlags
		flags.Var(&repositories, "repo", "registered repository grant (repeatable; required)")
		return func(ctx context.Context, env *Env, args []string) error {
			if len(args) != 0 || len(repositories) == 0 {
				return errors.New("mcp logs requires explicit --repo forge:owner/name selections")
			}
			return runMCPLogs(ctx, env, repositories)
		}
	}}
}

type logRepositoryFlags []string

func (v *logRepositoryFlags) String() string         { return strings.Join(*v, ",") }
func (v *logRepositoryFlags) Set(value string) error { *v = append(*v, value); return nil }

func runMCPLogs(ctx context.Context, env *Env, selections []string) (err error) {
	input, ok := env.Stdin.(io.ReadCloser)
	if !ok {
		return errors.New("mcp logs requires interruptible stdin")
	}
	output, ok := env.Stdout.(io.WriteCloser)
	if !ok {
		return errors.New("mcp logs requires interruptible stdout")
	}
	paths, err := config.Locate()
	if err != nil {
		return err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return err
	}
	info, err := os.Lstat(paths.DBPath())
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("mcp logs requires an existing initialized database")
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return errors.New("mcp logs database unavailable")
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var ids []int64
	for _, selection := range selections {
		forge, slug, ok := strings.Cut(selection, ":")
		owner, name, valid := strings.Cut(slug, "/")
		if !ok || !valid || owner == "" || name == "" || strings.Contains(name, "/") {
			return errors.New("mcp logs repository must be forge:owner/name")
		}
		repo, e := db.GetRepoBySlug(ctx, forge, owner, name)
		if e != nil {
			return errors.New("mcp logs repository is not registered")
		}
		ids = append(ids, repo.ID)
	}
	redactor := redact.New(redact.Options{Logger: env.Logger, Environ: os.Environ()})
	if err = initializeLogCredentials(ctx, env, db, cfg, ids, redactor); err != nil {
		return err
	}
	budget, err := logevidence.NewBudget(logevidence.Limits{})
	if err != nil {
		return err
	}
	service, err := logevidence.New(ctx, logevidence.Options{Store: db, RunsRoot: paths.RunsDir(), Scope: logevidence.Scope{RepositoryIDs: ids}, Redactor: redactor, Budget: budget})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, service.Close()) }()
	// Keep Unix socket paths short even when the system temp directory is long.
	directory, err := os.MkdirTemp("/tmp", "lf-log-reader-")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, os.RemoveAll(directory)) }()
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	broker, err := service.StartBroker(ctx, directory)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, broker.Close()) }()
	return (mcpadapter.Server{}).Run(ctx, input, output, submission.Client{DescriptorPath: broker.DescriptorPath()})
}

func initializeLogCredentials(ctx context.Context, env *Env, db *store.DB, cfg *config.Config, ids []int64, r *redact.Redactor) error {
	resolver := secrets.Resolver{Keychain: env.Keychain}
	register := func(ref secrets.Reference) error {
		token, err := resolver.Resolve(ctx, ref)
		if err != nil {
			return errors.New("mcp logs cannot initialize configured credential redaction")
		}
		r.AddToken(token)
		return nil
	}
	if ref, ok := secrets.ReferenceFrom(cfg.Providers.GitHub.TokenKeychain.Service, cfg.Providers.GitHub.TokenKeychain.Account, cfg.Providers.GitHub.TokenEnv); ok {
		if err := register(ref); err != nil {
			return err
		}
	}
	for _, id := range ids {
		versions, err := db.ListConnections(ctx, id)
		if err != nil {
			return errors.New("mcp logs credential registry unavailable")
		}
		for _, v := range versions {
			if ref, ok := secrets.ReferenceFrom(v.CredentialService, v.CredentialAccount, v.CredentialEnv); ok {
				if err := register(ref); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
