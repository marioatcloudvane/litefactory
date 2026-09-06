package cli

// The team commands inspect and materialize litefactory's three-tier agent
// team index (embedded default < static-path override < registered-repo
// override, US-2/US-4). `team list` and `team show` are read-only: they
// resolve exactly one tier via internal/teamsource and print what it
// contains. `team add` renders every agent resolved for a named team and
// installs the result into a repo via internal/team's write engine. `team
// status`, `team update` and `team manage` are declared here — so their
// flags and --help text are accurate — but their behaviour lands in a later
// story; invoking them today returns [ErrNotImplemented].

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/team"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

// teamHarnesses are the harness directory names T-26 detects and validates
// against. Order matters only for deterministic detection output.
var teamHarnesses = []string{"claude", "codex", "opencode"}

// teamHarnessFallback is what --harness resolves to when --repo has none of
// [teamHarnesses]'s directories present.
var teamHarnessFallback = []string{"claude", "codex"}

// teamRepoOptions are the --repo/--harness flags every team command shares
// (T-26): which repository to resolve the index and harness for, and which
// harness(es) to target.
type teamRepoOptions struct {
	repo    string
	harness string
}

func (o *teamRepoOptions) bind(fs *flag.FlagSet) {
	fs.StringVar(&o.repo, "repo", ".",
		"git repository to resolve the team index and harness for")
	fs.StringVar(&o.harness, "harness", "",
		"comma-separated harnesses to target (claude, codex, opencode); defaults to auto-detection")
}

// resolveTeamRepoPath resolves --repo to an absolute path and requires an
// exact .git entry there. There is no walk-up: this scaffold also serves
// future write commands, where guessing at a parent repository would be a
// mistake, not a convenience.
func resolveTeamRepoPath(repo string) (string, error) {
	abs, err := filepath.Abs(repo)
	if err != nil {
		return "", fmt.Errorf("resolve repo path %q: %w", repo, err)
	}
	gitPath := filepath.Join(abs, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", Preconditionf("no git repository at %s: %s not found", abs, gitPath)
		}
		return "", fmt.Errorf("stat %s: %w", gitPath, err)
	}
	return abs, nil
}

// isKnownHarness reports whether name is one of [teamHarnesses].
func isKnownHarness(name string) bool {
	for _, h := range teamHarnesses {
		if h == name {
			return true
		}
	}
	return false
}

// parseHarnessList validates a comma-separated --harness value. Every bad
// name is collected rather than reported fail-fast, so a typo in the second
// of three names does not hide the first.
func parseHarnessList(value string) ([]string, error) {
	var names []string
	var errs []error
	for _, part := range strings.Split(value, ",") {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		if !isKnownHarness(name) {
			errs = append(errs, fmt.Errorf("unknown harness %q (valid: claude, codex, opencode)", name))
			continue
		}
		names = append(names, name)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if len(names) == 0 {
		return nil, errors.New("--harness requires at least one harness name")
	}
	return names, nil
}

// teamHarnessResolution is the outcome of resolving --harness: which
// harnesses are in effect, and whether that came from an explicit flag,
// detecting a harness directory under --repo, or the documented fallback.
type teamHarnessResolution struct {
	names  []string
	source string
}

// resolveTeamHarness resolves --harness against repoPath (T-26). An explicit
// value is validated as a [UsageError] on a bad name; otherwise the harness
// directories already present at repoPath are detected, falling back to
// teamHarnessFallback when none are.
func resolveTeamHarness(cmd *Command, opts teamRepoOptions, repoPath string) (*teamHarnessResolution, error) {
	if opts.harness != "" {
		names, err := parseHarnessList(opts.harness)
		if err != nil {
			return nil, &UsageError{Cmd: cmd, Err: err}
		}
		return &teamHarnessResolution{names: names, source: "explicit"}, nil
	}

	var detected []string
	for _, h := range teamHarnesses {
		info, err := os.Stat(filepath.Join(repoPath, "."+h))
		if err == nil && info.IsDir() {
			detected = append(detected, h)
		}
	}
	if len(detected) > 0 {
		return &teamHarnessResolution{names: detected, source: "detected"}, nil
	}
	return &teamHarnessResolution{names: teamHarnessFallback, source: "fallback"}, nil
}

// teamRuntime is the configuration and database handle the team commands
// share. Unlike [repoRuntime], both fields may be nil: `team list`/`team
// show` are read-only inspection commands whose tier-1 (embedded) fallback
// must keep working even before `litefactory init` has run, so a missing
// config or database only rules out tiers 2 and 3 rather than failing the
// command outright.
type teamRuntime struct {
	cfg *config.Config
	db  *store.DB
}

// openTeamRuntime loads config.yaml and opens the database when both are
// present, degrading gracefully rather than failing when they are not: a
// genuinely corrupt configuration (a literal secret) is still a
// [PreconditionError], but an absent one just means tiers 2 and 3 do not
// apply this run.
func openTeamRuntime(ctx context.Context) (*teamRuntime, error) {
	paths, err := config.Locate()
	if err != nil {
		return nil, err
	}

	cfg, err := config.Load(paths)
	if err != nil {
		var notInitialized *config.NotInitializedError
		if errors.As(err, &notInitialized) {
			return &teamRuntime{}, nil
		}
		if errors.Is(err, config.ErrLiteralSecret) {
			return nil, Precondition(err)
		}
		return nil, err
	}
	rt := &teamRuntime{cfg: cfg}

	if _, err := os.Stat(paths.DBPath()); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return rt, nil
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
	rt.db = db
	return rt, nil
}

func (rt *teamRuntime) close() error {
	if rt.db == nil {
		return nil
	}
	return rt.db.Close()
}

// staticPathOverride is config.TeamConfig (tier 2), or "" when there is no
// loaded configuration at all.
func (rt *teamRuntime) staticPathOverride() string {
	if rt.cfg == nil {
		return ""
	}
	return rt.cfg.TeamConfig
}

// registeredRepoIdentifier looks up repoPath in the store and, when it is a
// registered repository, returns its slug as tier 3's identifier. No database
// means tier 3 is simply unavailable, not an error; a repository that is not
// registered is the same.
func (rt *teamRuntime) registeredRepoIdentifier(ctx context.Context, repoPath string) (string, error) {
	if rt.db == nil {
		return "", nil
	}
	repo, err := rt.db.GetRepoByPath(ctx, repoPath)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("look up registered repo at %s: %w", repoPath, err)
	}
	return repo.Slug(), nil
}

// teamSourceError translates a [teamsource.Resolve] failure into this
// package's exit-code contract: a configured override with nothing at it is
// a precondition (US-2 AC-3), never a usage or generic failure.
func teamSourceError(err error) error {
	var staticMissing *teamsource.StaticPathMissingError
	var repoMissing *teamsource.RegisteredRepoIndexMissingError
	if errors.As(err, &staticMissing) || errors.As(err, &repoMissing) {
		return Precondition(err)
	}
	return err
}

// resolveTeamIndex resolves the single team index source in effect for
// repoPath, in the tier precedence teamsource.Resolve implements.
func resolveTeamIndex(ctx context.Context, rt *teamRuntime, repoPath string) (*teamsource.Resolved, error) {
	identifier, err := rt.registeredRepoIdentifier(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	resolved, err := teamsource.Resolve(ctx, rt.db, rt.staticPathOverride(), identifier)
	if err != nil {
		return nil, teamSourceError(err)
	}
	return resolved, nil
}

// findTeam returns the entry named name from idx, or an error naming the
// team that was not found.
func findTeam(idx *team.Index, name string) (*team.TeamEntry, error) {
	for i := range idx.Teams {
		if idx.Teams[i].Name == name {
			return &idx.Teams[i], nil
		}
	}
	return nil, fmt.Errorf("unknown team %q", name)
}

// teamRosterEntry is one agent resolved for a team: either the team's own, or
// a shared agent the team does not override.
type teamRosterEntry struct {
	Key    string
	Agent  team.AgentEntry
	Shared bool
}

// teamRoster merges entry's own agents with idx-wide shared agents: a shared
// agent applies to every team except one that names the same key itself, in
// which case the team's own entry wins. internal/team defines no such merge
// itself (only the write engine's lockfile has adjacent logic, out of scope
// here), so this presentation-level combination lives in this package.
// Both halves are sorted by key so output never depends on map order.
func teamRoster(entry team.TeamEntry, shared map[string]team.AgentEntry) []teamRosterEntry {
	roster := make([]teamRosterEntry, 0, len(entry.Agents)+len(shared))
	seen := make(map[string]bool, len(entry.Agents))

	ownKeys := make([]string, 0, len(entry.Agents))
	for k := range entry.Agents {
		ownKeys = append(ownKeys, k)
	}
	sort.Strings(ownKeys)
	for _, k := range ownKeys {
		roster = append(roster, teamRosterEntry{Key: k, Agent: entry.Agents[k]})
		seen[k] = true
	}

	sharedKeys := make([]string, 0, len(shared))
	for k := range shared {
		sharedKeys = append(sharedKeys, k)
	}
	sort.Strings(sharedKeys)
	for _, k := range sharedKeys {
		if seen[k] {
			continue
		}
		roster = append(roster, teamRosterEntry{Key: k, Agent: shared[k], Shared: true})
	}
	return roster
}

// teamJSONAgent is one agent in `team list --json`'s output.
type teamJSONAgent struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Dest   string `json:"dest"`
	Shared bool   `json:"shared"`
}

// teamJSONTeam is one team in `team list --json`'s output.
type teamJSONTeam struct {
	Name   string          `json:"name"`
	Agents []teamJSONAgent `json:"agents"`
}

// teamListJSON is `team list --json`'s top-level shape (AC-1): every
// resolved team and agent, and which index source produced them.
type teamListJSON struct {
	Repo          string         `json:"repo"`
	IndexSource   string         `json:"index_source"`
	Harnesses     []string       `json:"harnesses"`
	HarnessSource string         `json:"harness_source"`
	Teams         []teamJSONTeam `json:"teams"`
}

// teamShowAgentJSON is one agent in `team show --json`'s output. IndexSource
// is repeated per agent (AC-2 asks for it "per agent") even though it is the
// same value for every agent in one invocation: exactly one source is ever in
// effect at a time, so there is nothing to break out per field.
type teamShowAgentJSON struct {
	Key         string `json:"key"`
	Source      string `json:"source"`
	Dest        string `json:"dest"`
	Shared      bool   `json:"shared"`
	IndexSource string `json:"index_source"`
}

// teamShowJSON is `team show <team> --json`'s top-level shape (AC-2).
type teamShowJSON struct {
	Repo          string              `json:"repo"`
	Team          string              `json:"team"`
	IndexSource   string              `json:"index_source"`
	Harnesses     []string            `json:"harnesses"`
	HarnessSource string              `json:"harness_source"`
	Agents        []teamShowAgentJSON `json:"agents"`
}

// writeTeamListJSON encodes v to out as indented JSON.
func writeTeamListJSON(out io.Writer, v teamListJSON) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode team list json: %w", err)
	}
	return nil
}

// writeTeamShowJSON encodes v to out as indented JSON.
func writeTeamShowJSON(out io.Writer, v teamShowJSON) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode team show json: %w", err)
	}
	return nil
}

// writeTeamListTable prints v as the human-readable form of `team list`.
func writeTeamListTable(out io.Writer, v teamListJSON) {
	fmt.Fprintf(out, "repo: %s\n", v.Repo)
	fmt.Fprintf(out, "index source: %s\n", v.IndexSource)
	fmt.Fprintf(out, "harness: %s (%s)\n\n", strings.Join(v.Harnesses, ", "), v.HarnessSource)

	if len(v.Teams) == 0 {
		fmt.Fprintln(out, "no teams in this index")
		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "TEAM\tAGENT\tSOURCE\tDEST\tSHARED\n")
	for _, t := range v.Teams {
		if len(t.Agents) == 0 {
			fmt.Fprintf(tw, "%s\t-\t-\t-\t-\n", t.Name)
			continue
		}
		for _, a := range t.Agents {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%t\n", t.Name, a.Key, a.Source, a.Dest, a.Shared)
		}
	}
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()
}

// writeTeamShowTable prints v as the human-readable form of `team show`.
func writeTeamShowTable(out io.Writer, v teamShowJSON) {
	fmt.Fprintf(out, "repo: %s\n", v.Repo)
	fmt.Fprintf(out, "team: %s\n", v.Team)
	fmt.Fprintf(out, "index source: %s\n", v.IndexSource)
	fmt.Fprintf(out, "harness: %s (%s)\n\n", strings.Join(v.Harnesses, ", "), v.HarnessSource)

	if len(v.Agents) == 0 {
		fmt.Fprintln(out, "no agents resolved for this team")
		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "AGENT\tSOURCE\tDEST\tSHARED\tINDEX SOURCE\n")
	for _, a := range v.Agents {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%t\t%s\n", a.Key, a.Source, a.Dest, a.Shared, a.IndexSource)
	}
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()
}

// runTeamList implements `team list` (AC-1): every resolved team and agent,
// and which index source is currently in effect. It writes nothing to disk.
func runTeamList(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, asJSON bool) (err error) {
	repoPath, err := resolveTeamRepoPath(opts.repo)
	if err != nil {
		return err
	}
	harness, err := resolveTeamHarness(cmd, opts, repoPath)
	if err != nil {
		return err
	}

	rt, err := openTeamRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	resolved, err := resolveTeamIndex(ctx, rt, repoPath)
	if err != nil {
		return err
	}

	teams := make([]teamJSONTeam, 0, len(resolved.Index.Teams))
	for _, t := range resolved.Index.Teams {
		roster := teamRoster(t, resolved.Index.Shared)
		agents := make([]teamJSONAgent, 0, len(roster))
		for _, r := range roster {
			agents = append(agents, teamJSONAgent{
				Key: r.Key, Source: r.Agent.Source, Dest: r.Agent.Dest, Shared: r.Shared,
			})
		}
		teams = append(teams, teamJSONTeam{Name: t.Name, Agents: agents})
	}

	out := teamListJSON{
		Repo:          repoPath,
		IndexSource:   resolved.Source.String(),
		Harnesses:     harness.names,
		HarnessSource: harness.source,
		Teams:         teams,
	}

	if asJSON {
		return writeTeamListJSON(env.Stdout, out)
	}
	writeTeamListTable(env.Stdout, out)
	return nil
}

// runTeamShow implements `team show <team>` (AC-2): every agent resolved for
// teamName, and which single index source resolved it.
func runTeamShow(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, teamName string, asJSON bool) (err error) {
	repoPath, err := resolveTeamRepoPath(opts.repo)
	if err != nil {
		return err
	}
	harness, err := resolveTeamHarness(cmd, opts, repoPath)
	if err != nil {
		return err
	}

	rt, err := openTeamRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	resolved, err := resolveTeamIndex(ctx, rt, repoPath)
	if err != nil {
		return err
	}

	t, err := findTeam(resolved.Index, teamName)
	if err != nil {
		return &UsageError{Cmd: cmd, Err: err}
	}

	roster := teamRoster(*t, resolved.Index.Shared)
	agents := make([]teamShowAgentJSON, 0, len(roster))
	for _, r := range roster {
		agents = append(agents, teamShowAgentJSON{
			Key: r.Key, Source: r.Agent.Source, Dest: r.Agent.Dest,
			Shared: r.Shared, IndexSource: resolved.Source.String(),
		})
	}

	out := teamShowJSON{
		Repo:          repoPath,
		Team:          t.Name,
		IndexSource:   resolved.Source.String(),
		Harnesses:     harness.names,
		HarnessSource: harness.source,
		Agents:        agents,
	}

	if asJSON {
		return writeTeamShowJSON(env.Stdout, out)
	}
	writeTeamShowTable(env.Stdout, out)
	return nil
}

// runTeamAdd implements `team add <team>`: it renders every agent resolved
// for teamName — its own agents plus the index's shared agents, via the same
// teamRoster merge list/show use — for every harness resolveTeamHarness
// selects, and installs the result into repoPath via team.Add.
//
// team.Add's own upsert semantics never refuse an already-managed repo (its
// doc comment: "Concurrent Add calls against the same root serialize..." —
// nowhere does it check for an existing lockfile); the refusal this command
// wants — "use team update instead" — lives here, via team.HasLockfile,
// exactly as that function's doc comment describes it should be used.
func runTeamAdd(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, teamName string) (err error) {
	repoPath, err := resolveTeamRepoPath(opts.repo)
	if err != nil {
		return err
	}
	harness, err := resolveTeamHarness(cmd, opts, repoPath)
	if err != nil {
		return err
	}

	hasLock, err := team.HasLockfile(repoPath)
	if err != nil {
		return fmt.Errorf("check existing lockfile: %w", err)
	}
	if hasLock {
		return fmt.Errorf("repo %s already has a team lockfile; run `litefactory team update` instead of `team add`", repoPath)
	}

	rt, err := openTeamRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	resolved, err := resolveTeamIndex(ctx, rt, repoPath)
	if err != nil {
		return err
	}

	t, err := findTeam(resolved.Index, teamName)
	if err != nil {
		return &UsageError{Cmd: cmd, Err: err}
	}
	roster := teamRoster(*t, resolved.Index.Shared)
	sourceRoster := make([]teamsource.RosterEntry, len(roster))
	for i, entry := range roster {
		sourceRoster[i] = teamsource.RosterEntry{Key: entry.Key, Agent: entry.Agent}
	}

	renders, err := teamsource.RenderTeam(repoPath, resolved.FS, teamName, sourceRoster, harness.names)
	if err != nil {
		return err
	}

	result, err := team.Add(repoPath, renders)
	if err != nil {
		return err
	}

	fmt.Fprintf(env.Stdout, "repo: %s\n", repoPath)
	fmt.Fprintf(env.Stdout, "team: %s\n", t.Name)
	fmt.Fprintf(env.Stdout, "index source: %s\n", resolved.Source.String())
	fmt.Fprintf(env.Stdout, "harness: %s (%s)\n\n", strings.Join(harness.names, ", "), harness.source)
	fmt.Fprintf(env.Stdout, "written: %d\n", len(result.Written))
	fmt.Fprintf(env.Stdout, "skipped (unchanged): %d\n", len(result.Skipped))
	return nil
}

// newTeamListCommand declares "litefactory team list".
func newTeamListCommand() *Command {
	cmd := &Command{
		Name:  "team list",
		Usage: "team list [--repo path] [--harness list] [--json]",
		Short: "List every resolved team and agent",
		Long: "Print every team and agent from the currently resolved team index — the\n" +
			"embedded default, a configured static-path override, or a registered\n" +
			"repository's own index, whichever is in effect for --repo — along with\n" +
			"which of those three sources produced it and which harnesses were\n" +
			"detected. Nothing is written to disk.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		asJSON := fs.Bool("json", false, "emit JSON instead of a table")
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamList(ctx, env, cmd, opts, *asJSON)
		}
	}
	return cmd
}

// newTeamShowCommand declares "litefactory team show <team>".
func newTeamShowCommand() *Command {
	cmd := &Command{
		Name:  "team show",
		Usage: "team show <team> [--repo path] [--harness list] [--json]",
		Short: "Show one team's resolved agents",
		Long: "Print every agent resolved for <team> — its own agents plus any\n" +
			"index-wide shared agent it does not itself override — and which single\n" +
			"index source (embedded, static-path override, or registered-repo\n" +
			"override) resolved them. Nothing is written to disk.",
		Args: ArgSpec{Names: []string{"team"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		asJSON := fs.Bool("json", false, "emit JSON instead of a table")
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamShow(ctx, env, cmd, opts, args[0], *asJSON)
		}
	}
	return cmd
}

// newTeamAddCommand declares "litefactory team add <team>".
func newTeamAddCommand() *Command {
	cmd := &Command{
		Name:  "team add",
		Usage: "team add <team> [--repo path] [--harness list]",
		Short: "Render and install one team's agents into a repo",
		Long: "Render every agent resolved for <team> — its own agents plus any\n" +
			"index-wide shared agent it does not itself override — for every\n" +
			"detected or explicitly requested harness, and install the result into\n" +
			"--repo via internal/team's write engine. Refuses to run against a repo\n" +
			"that already has a team lockfile from a previous add; run `team update`\n" +
			"against those instead.",
		Args: ArgSpec{Names: []string{"team"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamAdd(ctx, env, cmd, opts, args[0])
		}
	}
	return cmd
}

// newTeamStatusCommand declares "litefactory team status". Its flags parse
// and its --help is accurate; its behaviour is a follow-up story.
func newTeamStatusCommand() *Command {
	cmd := &Command{
		Name:  "team status",
		Usage: "team status [--repo path] [--harness list]",
		Short: "Not yet implemented",
		Long:  "team status is not implemented yet. This command currently only validates its flags.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		return notImplemented(cmd.Name)
	}
	return cmd
}

// newTeamUpdateCommand declares "litefactory team update". Its flags parse
// and its --help is accurate; its behaviour is a follow-up story.
func newTeamUpdateCommand() *Command {
	cmd := &Command{
		Name:  "team update",
		Usage: "team update [--repo path] [--harness list]",
		Short: "Not yet implemented",
		Long:  "team update is not implemented yet. This command currently only validates its flags.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		return notImplemented(cmd.Name)
	}
	return cmd
}

// newTeamManageCommand declares "litefactory team manage". Its flags parse
// and its --help is accurate; its behaviour is a follow-up story.
func newTeamManageCommand() *Command {
	cmd := &Command{
		Name:  "team manage",
		Usage: "team manage [--repo path] [--harness list]",
		Short: "Not yet implemented",
		Long:  "team manage is not implemented yet. This command currently only validates its flags.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		opts.bind(fs)
		return notImplemented(cmd.Name)
	}
	return cmd
}
