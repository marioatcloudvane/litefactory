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
	"github.com/cloudvane/litefactory/internal/factory/localanalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
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
	ids, err := localanalysis.EvidenceRepositories(ctx, db, selections)
	if err != nil {
		return err
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
	return localanalysis.InitializeCredentials(ctx, env.Keychain, db, cfg, ids, r)
}
