package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/repos"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// repoUnset is printed in place of a column a repository has no value for.
const repoUnset = "-"

// repoNeverFetched is what `repo list` prints for a repository that has never
// been fetched (AC-1.9). The zero time is a real state — a repository is
// registered long before the daemon first fetches it — and printing a zero
// timestamp instead would look like a bug in the daemon.
const repoNeverFetched = "never"

// repoAddOptions are `repo add`'s flags. Each one overrides one step of a
// documented resolution order; leaving one unset is how the next source gets
// its turn (AC-1.3, AC-1.6).
type repoAddOptions struct {
	base      string
	worktrees string
	prefix    string
	team      string
}

func (o *repoAddOptions) bind(fs *flag.FlagSet) {
	fs.StringVar(&o.team, "team", "", "installed external team to associate with this repository")
	fs.StringVar(&o.base, "base", "",
		"branch to cut task branches from; overrides default_base_branch and the remote's HEAD")
	fs.StringVar(&o.worktrees, "worktrees", "",
		"directory to create this repository's worktrees under; overrides worktree_root and the sibling default")
	fs.StringVar(&o.prefix, "prefix", "",
		"first segment of every task branch; overrides feature_prefix")
}

// newRepoAddCommand declares "litefactory repo add <path>".
func newRepoAddCommand() *Command {
	cmd := &Command{
		Name:  "repo add",
		Usage: "repo add <path> [--team id] [--base branch] [--worktrees dir] [--prefix name]",
		Short: "Register a local clone with litefactory",
		Long: "Register the git clone at <path>. The owner, name and default branch are\n" +
			"read from the clone itself; nothing is fetched over the network. Running it\n" +
			"again on the same path updates the registration instead of duplicating it.\n\n" +
			"Use --team to select an installed central team. Existing repository markers\n" +
			"remain diagnostic only. No files are written into the clone.",
		Args: ArgSpec{Names: []string{"path"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts repoAddOptions
		opts.bind(fs)
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoAdd(ctx, env, cmd, opts, args[0])
		}
	}
	return cmd
}

// newRepoListCommand declares "litefactory repo list".
func newRepoListCommand() *Command {
	return &Command{
		Name:  "repo list",
		Usage: "repo list",
		Short: "List the registered repositories",
		Long: "Print every registered repository: its local path, its remote as\n" +
			"forge:owner/name, the branch task branches are cut from, the detected aiteam\n" +
			"team, where its worktrees are created, and when it was last fetched.",
		New: func(*flag.FlagSet) Handler {
			return func(ctx context.Context, env *Env, _ []string) error {
				return runRepoList(ctx, env)
			}
		},
	}
}

// runRepoAdd registers the clone at path.
//
// The work splits cleanly in two: internal/repos decides what the registration
// *is* — reading the clone, and only reading it — and this function persists
// that decision and reports it. No git command is issued from here, which is
// what keeps the S-6 allowlist a single chokepoint.
func runRepoAdd(ctx context.Context, env *Env, cmd *Command, opts repoAddOptions, path string) (err error) {
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	// Resolving git here rather than inside internal/repos means a missing or
	// broken git is reported once, with the PATH it searched, before any of the
	// checks below run and blame the repository for it.
	binary, err := git.Resolve(ctx)
	if err != nil {
		return Precondition(err)
	}
	clone, err := binary.Open(ctx, path)
	if err != nil {
		check := repos.CheckGitRepository
		if errors.Is(err, fs.ErrNotExist) {
			check = repos.CheckPath
		}
		return repoRegistrationError(cmd, &repos.CheckError{Check: check, Path: path, Err: err})
	}
	previous, previousErr := rt.db.GetRepoByPath(ctx, clone.Path())
	if previousErr != nil && !errors.Is(previousErr, store.ErrNotFound) {
		return previousErr
	}
	if previousErr == nil {
		if opts.team == "" {
			opts.team = previous.ExternalTeam
		}
		if opts.base == "" {
			opts.base = previous.BaseBranch
		}
		if opts.worktrees == "" {
			opts.worktrees = previous.WorktreeRoot
		}
		if opts.prefix == "" {
			opts.prefix = previous.FeaturePrefix
		}
	}
	if opts.team != "" {
		for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
			if _, err := teamsource.Capture(ctx, rt.paths, opts.team, mode); err != nil {
				return Preconditionf("external team %q is not ready: %v; install it with team add first", opts.team, err)
			}
		}
	}

	registration, err := repos.Resolve(ctx, binary, path, repos.Options{
		ExternalTeam:  opts.team,
		BaseBranch:    opts.base,
		WorktreeRoot:  opts.worktrees,
		FeaturePrefix: opts.prefix,
	}, repos.Defaults{
		BaseBranch:    rt.cfg.Settings.DefaultBaseBranch,
		WorktreeRoot:  rt.cfg.Settings.WorktreeRoot,
		FeaturePrefix: rt.cfg.Settings.FeaturePrefix,
	})
	if err != nil {
		return repoRegistrationError(cmd, err)
	}

	// AC-PAR-4.8, and it runs before the upsert so that a refused registration
	// leaves no row behind. internal/repos decides; the rows come from here,
	// because that package makes no query of its own.
	existing, err := rt.db.ListRepos(ctx)
	if err != nil {
		return err
	}
	if err := repos.ValidateNewWorktreeRoot(registration, existing); err != nil {
		return repoRegistrationError(cmd, err)
	}

	// Whether the row already existed only changes the verb in the output, but
	// it is the difference between "I registered something" and "I changed
	// something you already had", and the user should not have to guess which
	// happened (AC-1.8).
	_, err = rt.db.GetRepoByPath(ctx, registration.Path)
	switch {
	case err == nil:
	case errors.Is(err, store.ErrNotFound):
	default:
		return err
	}
	existed := err == nil

	if _, err := rt.db.UpsertRepo(ctx, registration.StoreRepo(time.Now().UTC())); err != nil {
		return err
	}

	if registration.Aiteam.Team == "" && registration.ExternalTeam == "" {
		// AC-1.5: an undetectable team is a warning, never a failure. The
		// repository is registered either way, and nothing at M0 reads the
		// team, so refusing here would block real work over a null column.
		fmt.Fprintf(env.Stderr,
			"warning: %s: aiteam is installed but no team is declared; storing none.\n"+
				"  Declare one with an `<!-- aiteam:team=NAME -->` marker in CLAUDE.md or a .aiteam/team file.\n",
			registration.Path)
	}
	writeRegistration(env.Stdout, registration, existed)
	return nil
}

// writeRegistration prints what was stored, and where each resolved value came
// from. Naming the source is the point: "base branch main" invites a shrug,
// "base branch main (remote HEAD)" tells the user which of the three sources
// answered and therefore which one to change.
func writeRegistration(out io.Writer, r *repos.Registration, existed bool) {
	verb := "registered"
	if existed {
		verb = "updated"
	}
	fmt.Fprintf(out, "%s %s\n", verb, r.Ref)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "  path\t%s\n", r.Path)
	fmt.Fprintf(tw, "  remote\t%s\n", r.RemoteURL)
	fmt.Fprintf(tw, "  base branch\t%s (%s)\n", r.BaseBranch, r.BaseBranchSource)
	fmt.Fprintf(tw, "  branch prefix\t%s\n", r.FeaturePrefix)
	fmt.Fprintf(tw, "  worktrees\t%s (%s)\n", r.WorktreeRoot, r.WorktreeRootSource)
	fmt.Fprintf(tw, "  aiteam team\t%s\n", repoOrUnset(r.Aiteam.Team))
	fmt.Fprintf(tw, "  external team\t%s\n", repoOrUnset(r.ExternalTeam))
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()
}

// runRepoList prints every registered repository (AC-1.9).
func runRepoList(ctx context.Context, env *Env) (err error) {
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	list, err := rt.db.ListRepos(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintf(env.Stdout, "no repositories registered; add one with `litefactory repo add <path>`\n")
		return nil
	}

	tw := tabwriter.NewWriter(env.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "PATH\tREMOTE\tBASE\tTEAM\tEXTERNAL TEAM\tWORKTREES\tLAST FETCH\n")
	for _, repo := range list {
		fmt.Fprintf(tw, "%s\t%s:%s\t%s\t%s\t%s\t%s\t%s\n",
			repo.Path, repo.Forge, repo.Slug(), repo.BaseBranch,
			repoOrUnset(repo.AiteamTeam), repoOrUnset(repo.ExternalTeam), repo.WorktreeRoot, repoFetchTime(repo.LastFetchAt))
	}
	return tw.Flush()
}

// repoFetchTime renders a last-fetch instant, or [repoNeverFetched] when there
// has not been one. UTC, because every other timestamp litefactory prints or
// stores is UTC and a list that mixed zones would be unreadable.
func repoFetchTime(t time.Time) string {
	if t.IsZero() {
		return repoNeverFetched
	}
	return t.UTC().Format(time.RFC3339)
}

// repoOrUnset renders an optional value, or [repoUnset] when it is empty.
func repoOrUnset(value string) string {
	if value == "" {
		return repoUnset
	}
	return value
}

// repoRuntime is the configuration and database handle the repo commands
// share.
type repoRuntime struct {
	paths config.Paths
	cfg   *config.Config
	db    *store.DB
}

// openRepoRuntime loads config.yaml and opens the database.
//
// Both must already exist: `litefactory init` owns creating them, and opening
// a database that is not there would silently manufacture an empty one with no
// identities in it — a machine that looks set up and cannot post a comment.
// Migrations still run, because they are idempotent and a binary newer than
// the database has to be able to say so.
func openRepoRuntime(ctx context.Context) (*repoRuntime, error) {
	paths, err := config.Locate()
	if err != nil {
		return nil, err
	}
	cfg, err := config.Load(paths)
	if err != nil {
		var notInitialized *config.NotInitializedError
		if errors.As(err, &notInitialized) || errors.Is(err, config.ErrLiteralSecret) {
			return nil, Precondition(err)
		}
		return nil, err
	}
	if _, err := os.Stat(paths.DBPath()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return nil, fmt.Errorf("read %s: %w", paths.DBPath(), err)
	}

	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return nil, err
	}
	if err := db.Migrate(ctx); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &repoRuntime{paths: paths, cfg: cfg, db: db}, nil
}

func (r *repoRuntime) close() error { return r.db.Close() }

// repoRegistrationError turns a registration failure into an exit code.
//
// This is the whole translation layer between internal/repos and the process:
// the domain package returns sentinels and typed errors and never imports this
// one, so the mapping from "which check failed" to 1, 2 or 3 lives here and
// only here.
//
// Everything that says "this repository or this machine is not in a state
// where I can register it" is a precondition (exit 3) — that is AC-1.1,
// AC-1.2 and AC-1.4, and AC-PAR-4.8, where what makes this repository
// unregisterable is another one already registered. A flag value git would not
// accept is the command line
// being wrong (exit 2). The error text itself is left alone: for a git failure
// it is a [*git.CommandError] carrying the exact command and its stderr, which
// several criteria require printed verbatim.
func repoRegistrationError(cmd *Command, err error) error {
	switch {
	case errors.Is(err, repos.ErrInvalidBaseBranch),
		errors.Is(err, naming.ErrInvalidFeaturePrefix):
		return &UsageError{Cmd: cmd, Err: err}
	case errors.Is(err, repos.ErrAiteamMissing),
		errors.Is(err, repos.ErrBaseBranchUnknown),
		errors.Is(err, repos.ErrWorktreeRootCollision),
		errors.Is(err, fs.ErrNotExist),
		errors.Is(err, git.ErrNotDirectory),
		errors.Is(err, git.ErrNotARepository),
		errors.Is(err, git.ErrBareRepository),
		errors.Is(err, git.ErrRemoteNotFound),
		errors.Is(err, git.ErrUnsupportedForge),
		errors.Is(err, git.ErrMalformedRemoteURL):
		return Precondition(err)
	default:
		return err
	}
}
