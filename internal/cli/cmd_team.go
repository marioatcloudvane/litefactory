package cli

// The team commands inspect and materialize litefactory's three-tier agent
// team index (embedded default < static-path override < registered-repo
// override, US-2/US-4). `team list` and `team show` are read-only: they
// resolve exactly one tier via internal/teamsource and print what it
// contains. `team add` renders every agent resolved for a named team and
// installs the result into a repo via internal/team's write engine. `team
// status` is also read-only: it classifies every file tracked by --repo's
// provenance lockfile into one of five drift states by comparing installed
// bytes against a fresh render of each agent's current source, using
// internal/team.Classify; it writes nothing to disk and exits non-zero when
// any tracked file is not in sync. `team update` reuses that same
// per-record classification and writes back only what's safe automatically
// (upstream updated, missing); a hand-edited or conflicting file is left
// untouched and reported unless --force is passed. It writes exclusively
// through internal/team's existing write engine (internal/team.Add) — it
// never introduces a second write path. By default it acts on --repo alone;
// passing --all-worktrees additionally discovers every worktree `git
// worktree list` confirms belongs to the same repository — no persisted
// schema relationship, discovery is live via internal/git's existing
// Clone.WorktreeList — and applies the same per-worktree algorithm to each
// independently, so a hand-edit or a failure in one worktree never blocks or
// force-overwrites another. `team manage <agent-key>
// set|unset|show` records, removes, or reports a per-repo override that
// points one agent at a local markdown file instead of its index-resolved
// source; the override is read at the same call site every render goes
// through (internal/teamsource's readAgentSource), so `team add`, `team
// status`, and `team update` honor it automatically with no changes to
// internal/team's drift classification.

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
	return rt.cfg.Settings.TeamConfig
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

// teamStatusRecordJSON is one lockfile-tracked artifact in `team status`'s
// report: which agent/harness/destination it is, which of the five
// [team.Status] states it classifies as, and — only for Locally modified and
// Conflict (AC-2) — a note that a hand-edit there is at risk of being
// discarded by a future `team update`.
type teamStatusRecordJSON struct {
	AgentKey string `json:"agent_key"`
	Harness  string `json:"harness"`
	DestPath string `json:"dest_path"`
	Status   string `json:"status"`
	Note     string `json:"note,omitempty"`
}

// teamStatusJSON is `team status --json`'s top-level shape. InSync is true
// only when every tracked record classified as [team.StatusInSync] (AC-1) —
// it is what runTeamStatus's non-zero exit condition is derived from.
type teamStatusJSON struct {
	Repo    string                 `json:"repo"`
	InSync  bool                   `json:"in_sync"`
	Records []teamStatusRecordJSON `json:"records"`
}

// teamStatusNote returns the hand-edit-risk note team status attaches to a
// Locally modified or Conflict record (AC-2), and an empty string for every
// other state.
func teamStatusNote(s team.Status) string {
	switch s {
	case team.StatusLocallyModified, team.StatusConflict:
		return "hand-edited since install; a future team update could discard this change"
	default:
		return ""
	}
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

// writeTeamStatusJSON encodes v to out as indented JSON.
func writeTeamStatusJSON(out io.Writer, v teamStatusJSON) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode team status json: %w", err)
	}
	return nil
}

// writeTeamStatusTable prints v as the human-readable form of `team status`.
func writeTeamStatusTable(out io.Writer, v teamStatusJSON) {
	fmt.Fprintf(out, "repo: %s\n\n", v.Repo)

	if len(v.Records) == 0 {
		fmt.Fprintln(out, "no files tracked by this repo's team lockfile")
		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "STATUS\tAGENT\tHARNESS\tDEST\tNOTE\n")
	for _, r := range v.Records {
		note := r.Note
		if note == "" {
			note = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Status, r.AgentKey, r.Harness, r.DestPath, note)
	}
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()

	if v.InSync {
		fmt.Fprintln(out, "\nall tracked files are in sync")
	}
}

// teamUpdateRecordJSON is one lockfile record's outcome in `team update`'s
// report: what it classified as, and what update did about it.
type teamUpdateRecordJSON struct {
	AgentKey string `json:"agent_key"`
	Harness  string `json:"harness"`
	DestPath string `json:"dest_path"`
	Status   string `json:"status"`
	Action   string `json:"action"`
	Note     string `json:"note,omitempty"`
}

// teamUpdateJSON is `team update --json`'s top-level shape.
type teamUpdateJSON struct {
	Repo    string                 `json:"repo"`
	Force   bool                   `json:"force"`
	Updated int                    `json:"updated"`
	Skipped int                    `json:"skipped"`
	Refused int                    `json:"refused"`
	Records []teamUpdateRecordJSON `json:"records"`
}

// teamUpdateAction* name the "action" column team update reports for one
// record, distinct from its Status: a record's Status is what team.Classify
// found; its action is what runTeamUpdate then decided to do about it.
const (
	// teamUpdateActionNone means the record was already in sync: nothing to
	// write.
	teamUpdateActionNone = "none"
	// teamUpdateActionUpdated means the record was Upstream updated or
	// Missing and was safely re-rendered and written: applying it discarded
	// nothing (AC-1).
	teamUpdateActionUpdated = "updated"
	// teamUpdateActionOverwritten means the record was Locally modified or
	// Conflict and --force was passed, so it was written anyway, discarding
	// a hand-edit (AC-2). Every such record is named individually in the
	// report — never only as an aggregate count.
	teamUpdateActionOverwritten = "overwritten"
	// teamUpdateActionRefused means the record was Locally modified or
	// Conflict and --force was not passed, so it was left untouched (AC-1).
	teamUpdateActionRefused = "refused"
	// teamUpdateActionError means the record could not be classified at all
	// (a render/classify failure), or classified as includable but its
	// fresh render no longer produces its DestPath — either way, nothing
	// was written for it.
	teamUpdateActionError = "error"
)

// teamUpdateNote returns the report note attached to a record given its
// classified status and the action taken, so the table and JSON forms agree
// on wording without duplicating the switch.
func teamUpdateNote(status team.Status, action string) string {
	switch action {
	case teamUpdateActionOverwritten:
		return fmt.Sprintf("was %s; overwritten because --force was passed", status.String())
	case teamUpdateActionRefused:
		return fmt.Sprintf("hand-edited since install (%s); left untouched — pass --force to overwrite", status.String())
	default:
		return ""
	}
}

// writeTeamUpdateJSON encodes v to out as indented JSON.
func writeTeamUpdateJSON(out io.Writer, v teamUpdateJSON) error {
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fmt.Errorf("encode team update json: %w", err)
	}
	return nil
}

// writeTeamUpdateTable prints v as the human-readable form of `team update`.
// AC-2 requires that any overwritten file be named explicitly rather than
// only counted, so every record is always listed, not just the changed
// ones.
func writeTeamUpdateTable(out io.Writer, v teamUpdateJSON) {
	fmt.Fprintf(out, "repo: %s\n\n", v.Repo)

	if len(v.Records) == 0 {
		fmt.Fprintln(out, "no files tracked by this repo's team lockfile")
		return
	}

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "ACTION\tSTATUS\tAGENT\tHARNESS\tDEST\tNOTE\n")
	for _, r := range v.Records {
		note := r.Note
		if note == "" {
			note = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", r.Action, r.Status, r.AgentKey, r.Harness, r.DestPath, note)
	}
	// A failed write of already-reported output is not actionable.
	_ = tw.Flush()

	fmt.Fprintf(out, "\nupdated: %d\n", v.Updated)
	fmt.Fprintf(out, "skipped (unchanged): %d\n", v.Skipped)
	if v.Refused > 0 {
		fmt.Fprintf(out, "refused (hand-edited; pass --force to overwrite): %d\n", v.Refused)
	}
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

// runTeamStatus implements `team status`: it classifies every file tracked
// by --repo's provenance lockfile into one of team.Classify's five drift
// states, by comparing installed bytes against a fresh render of each
// record's agent's current source (internal/teamsource.RenderCurrent) and
// the lockfile's own recorded hashes. It writes nothing to disk.
//
// A repo with no lockfile yet has nothing tracked, so AC-1's "every tracked
// artifact is classified" holds vacuously and this returns nil rather than
// an error — team.LoadLockfile already reports that case as an empty
// Lockfile alongside team.ErrNoLockfile, so that one sentinel is tolerated
// here and nothing else is.
//
// The returned error is a plain error, never a [UsageError] or
// [PreconditionError]: drift is a normal outcome to report, not a usage
// mistake or an unready machine, so the exit code it produces (via
// [ExitCode]) is the plain-error exit rather than either of those two.
func runTeamStatus(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, asJSON bool) (err error) {
	repoPath, err := resolveTeamRepoPath(opts.repo)
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

	lf, err := team.LoadLockfile(repoPath)
	if err != nil && !errors.Is(err, team.ErrNoLockfile) {
		return err
	}

	records := make([]teamStatusRecordJSON, 0, len(lf.Records))
	inSync := true
	drifted := 0
	// recordErrs collects per-record failures that couldn't be classified
	// into any of the five states (a render or classify error). Each such
	// failure is isolated to its own report entry below rather than
	// aborting the loop, matching the errors.Join aggregation convention
	// internal/teamsource/render.go's RenderTeam already uses in this same
	// package's call graph — a bad record must not hide every other
	// record's status (AC-1, AC-3).
	var recordErrs []error
	for _, rec := range lf.Records {
		freshSourceHash, freshFiles, renderErr := teamsource.RenderCurrent(repoPath, resolved.FS, rec.Harness, rec.AgentKey, rec.SourcePath)
		if renderErr != nil {
			wrapped := fmt.Errorf("re-render %s (%s): %w", rec.AgentKey, rec.Harness, renderErr)
			recordErrs = append(recordErrs, wrapped)
			inSync = false
			drifted++
			records = append(records, teamStatusRecordJSON{
				AgentKey: rec.AgentKey,
				Harness:  rec.Harness,
				DestPath: rec.DestPath,
				Status:   "error",
				Note:     wrapped.Error(),
			})
			continue
		}

		// A source that no longer produces rec.DestPath at all is outside the
		// five-state table's scope; freshBody stays nil so classifyBytes falls
		// through to whichever of Locally modified or Conflict its SourceHash
		// comparison implies, rather than erroring the whole report out.
		var freshBody []byte
		for _, f := range freshFiles {
			if f.Path == rec.DestPath {
				freshBody = f.Bytes
				break
			}
		}

		freshTeams := team.DecodeTeams(rec.Teams)
		status, classifyErr := team.Classify(repoPath, rec, freshSourceHash, freshTeams, freshBody)
		if classifyErr != nil {
			wrapped := fmt.Errorf("classify %s (%s, %s): %w", rec.AgentKey, rec.Harness, rec.DestPath, classifyErr)
			recordErrs = append(recordErrs, wrapped)
			inSync = false
			drifted++
			records = append(records, teamStatusRecordJSON{
				AgentKey: rec.AgentKey,
				Harness:  rec.Harness,
				DestPath: rec.DestPath,
				Status:   "error",
				Note:     wrapped.Error(),
			})
			continue
		}

		if status != team.StatusInSync {
			inSync = false
			drifted++
		}
		records = append(records, teamStatusRecordJSON{
			AgentKey: rec.AgentKey,
			Harness:  rec.Harness,
			DestPath: rec.DestPath,
			Status:   status.String(),
			Note:     teamStatusNote(status),
		})
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].DestPath != records[j].DestPath {
			return records[i].DestPath < records[j].DestPath
		}
		if records[i].AgentKey != records[j].AgentKey {
			return records[i].AgentKey < records[j].AgentKey
		}
		return records[i].Harness < records[j].Harness
	})

	out := teamStatusJSON{
		Repo:    repoPath,
		InSync:  inSync,
		Records: records,
	}

	if asJSON {
		if err := writeTeamStatusJSON(env.Stdout, out); err != nil {
			return err
		}
	} else {
		writeTeamStatusTable(env.Stdout, out)
	}

	if !inSync {
		return errors.Join(append(recordErrs, fmt.Errorf("%d of %d tracked file(s) are not in sync", drifted, len(records)))...)
	}
	return nil
}

// lockRecordKey mirrors internal/team's own LockRecord.key() composition
// (Harness, AgentKey, DestPath, \x00-joined), for reconciling a team.Add
// result against records this loop already decided to include without
// reaching into internal/team's unexported key().
func lockRecordKey(harness, agentKey, destPath string) string {
	return harness + "\x00" + agentKey + "\x00" + destPath
}

// teamUpdateWorktree runs `team update`'s per-worktree algorithm (US-11)
// against exactly one worktree: it classifies every file tracked by
// repoPath's own provenance lockfile exactly as runTeamStatus does — the
// same internal/team.Classify five-state table, fed by the same
// internal/teamsource.RenderCurrent re-render — and then writes back only
// what's safe to write back automatically, through internal/team's existing
// write engine (team.Add). It never introduces a second write path and it
// never discovers or touches any worktree other than repoPath: every
// operation here is scoped to the one lockfile team.LoadLockfile(repoPath)
// reads and the one repo team.Add(repoPath, ...) writes into.
//
// This is the unit --all-worktrees (US-15) repeats once per git-confirmed
// sibling: runTeamUpdateAllWorktrees calls it independently for repoPath and
// for every sibling runTeamUpdate discovers, so a failure classifying or
// writing one worktree's files never touches another worktree's own
// .litefactory bookkeeping.
//
// Per record:
//   - In sync: nothing to do.
//   - Upstream updated / Missing: safe to update — applying it discards
//     nothing, so it is always re-rendered and written (AC-1).
//   - Locally modified / Conflict: refused by default and reported by name
//     and classification; only overwritten when force is true, in which
//     case it is still named explicitly as overwritten rather than folded
//     into an aggregate count (AC-2).
//
// As with team status, a repo with no lockfile yet has nothing tracked, so
// this is a no-op rather than an error.
//
// Every per-record failure — a render/classify error, or a record decided
// includable whose fresh render no longer produces its DestPath at all — is
// isolated to its own report entry rather than aborting the batch, matching
// the isolate-and-continue, errors.Join-at-the-end convention runTeamStatus
// and internal/teamsource/render.go's RenderTeam already use: one bad record
// must never hide or block every other record's update.
//
// A nil *teamUpdateJSON return means repoPath's own team index or lockfile
// could not even be resolved: nothing was classified, there is nothing to
// print for this worktree, and the returned error explains why. A non-nil
// report paired with a non-nil error means classification/writing went
// ahead and produced something worth printing, but at least one record was
// refused or could not be classified/written. Either way the returned error
// is a plain error, never a [UsageError] or [PreconditionError], for the
// same reason runTeamStatus's is: a refused file is a normal, expected
// outcome to report, not a usage mistake or an unready machine.
func teamUpdateWorktree(ctx context.Context, rt *teamRuntime, repoPath string, force bool) (*teamUpdateJSON, error) {
	resolved, err := resolveTeamIndex(ctx, rt, repoPath)
	if err != nil {
		return nil, err
	}

	lf, err := team.LoadLockfile(repoPath)
	if err != nil && !errors.Is(err, team.ErrNoLockfile) {
		return nil, err
	}

	// pending groups, per (Harness, AgentKey), the RenderedFile entries whose
	// owning record was individually decided includable. Grouped rather than
	// batched per-record so team.Add sees one AgentRender per agent — never
	// an agent's whole fresh Files set just because one of its records needs
	// updating, so a refused sibling file is never smuggled into the write
	// batch alongside an included one.
	type pendingAgent struct {
		teams      []string
		sourcePath string
		sourceHash string
		files      []team.RenderedFile
	}
	pending := map[string]*pendingAgent{}
	var pendingOrder []string

	// includedAt maps a record's lockRecordKey to its index in records, for
	// every record this loop decided to include in the write batch, so the
	// report can be corrected below once team.Add's actual outcome is known
	// rather than assumed correct ahead of it.
	includedAt := map[string]int{}

	records := make([]teamUpdateRecordJSON, 0, len(lf.Records))
	updated, skipped, refused := 0, 0, 0
	var recordErrs []error

	for _, rec := range lf.Records {
		freshSourceHash, freshFiles, renderErr := teamsource.RenderCurrent(repoPath, resolved.FS, rec.Harness, rec.AgentKey, rec.SourcePath)
		if renderErr != nil {
			wrapped := fmt.Errorf("re-render %s (%s): %w", rec.AgentKey, rec.Harness, renderErr)
			recordErrs = append(recordErrs, wrapped)
			records = append(records, teamUpdateRecordJSON{
				AgentKey: rec.AgentKey,
				Harness:  rec.Harness,
				DestPath: rec.DestPath,
				Status:   "error",
				Action:   teamUpdateActionError,
				Note:     wrapped.Error(),
			})
			continue
		}

		// Mirrors runTeamStatus's own fresh-body lookup exactly: for a
		// root-file target, freshBody is only this agent's marker section
		// body (Classify wraps it via buildMarkerSection itself), never the
		// whole shared root file.
		var freshBody []byte
		var freshFound bool
		for _, f := range freshFiles {
			if f.Path == rec.DestPath {
				freshBody = f.Bytes
				freshFound = true
				break
			}
		}

		freshTeams := team.DecodeTeams(rec.Teams)
		status, classifyErr := team.Classify(repoPath, rec, freshSourceHash, freshTeams, freshBody)
		if classifyErr != nil {
			wrapped := fmt.Errorf("classify %s (%s, %s): %w", rec.AgentKey, rec.Harness, rec.DestPath, classifyErr)
			recordErrs = append(recordErrs, wrapped)
			records = append(records, teamUpdateRecordJSON{
				AgentKey: rec.AgentKey,
				Harness:  rec.Harness,
				DestPath: rec.DestPath,
				Status:   "error",
				Action:   teamUpdateActionError,
				Note:     wrapped.Error(),
			})
			continue
		}

		action := teamUpdateActionNone
		include := false
		switch status {
		case team.StatusUpstreamUpdated, team.StatusMissing:
			action = teamUpdateActionUpdated
			include = true
		case team.StatusLocallyModified, team.StatusConflict:
			if force {
				action = teamUpdateActionOverwritten
				include = true
			} else {
				action = teamUpdateActionRefused
			}
		}

		if include && !freshFound {
			// A record decided includable whose fresh render no longer
			// produces its DestPath at all must never flow into a
			// RenderedFile{Bytes: nil} write — that would silently install
			// an empty file. Reported as its own isolated anomaly instead.
			wrapped := fmt.Errorf(
				"%s (%s, %s) classified as %s but its current source no longer produces this file; left untouched rather than writing empty content",
				rec.AgentKey, rec.Harness, rec.DestPath, status)
			recordErrs = append(recordErrs, wrapped)
			records = append(records, teamUpdateRecordJSON{
				AgentKey: rec.AgentKey,
				Harness:  rec.Harness,
				DestPath: rec.DestPath,
				Status:   status.String(),
				Action:   teamUpdateActionError,
				Note:     wrapped.Error(),
			})
			continue
		}

		switch action {
		case teamUpdateActionUpdated, teamUpdateActionOverwritten:
			updated++
		case teamUpdateActionRefused:
			refused++
		default:
			skipped++
		}

		idx := len(records)
		records = append(records, teamUpdateRecordJSON{
			AgentKey: rec.AgentKey,
			Harness:  rec.Harness,
			DestPath: rec.DestPath,
			Status:   status.String(),
			Action:   action,
			Note:     teamUpdateNote(status, action),
		})

		if !include {
			continue
		}

		includedAt[lockRecordKey(rec.Harness, rec.AgentKey, rec.DestPath)] = idx

		agentKey := rec.Harness + "\x00" + rec.AgentKey
		agent, ok := pending[agentKey]
		if !ok {
			agent = &pendingAgent{
				teams:      freshTeams,
				sourcePath: rec.SourcePath,
				sourceHash: freshSourceHash,
			}
			pending[agentKey] = agent
			pendingOrder = append(pendingOrder, agentKey)
		}
		agent.files = append(agent.files, team.RenderedFile{Path: rec.DestPath, Bytes: freshBody})
	}

	if len(pendingOrder) > 0 {
		renders := make([]team.AgentRender, 0, len(pendingOrder))
		for _, agentKey := range pendingOrder {
			agent := pending[agentKey]
			parts := strings.SplitN(agentKey, "\x00", 2)
			renders = append(renders, team.AgentRender{
				Harness:    parts[0],
				AgentKey:   parts[1],
				Teams:      agent.teams,
				SourcePath: agent.sourcePath,
				SourceHash: agent.sourceHash,
				Files:      agent.files,
			})
		}

		result, addErr := team.Add(repoPath, renders)
		if addErr != nil {
			wrapped := fmt.Errorf("write updated files: %w", addErr)
			recordErrs = append(recordErrs, wrapped)
			// team.Add's own doc comment: a failure partway through leaves
			// either nothing changed or a partial set of artifacts on disk,
			// with no way from here to tell which records landed. Every
			// record this loop decided to include is downgraded from its
			// optimistic "updated"/"overwritten" action to "error" rather
			// than reporting a write that may never have happened.
			for _, idx := range includedAt {
				records[idx].Action = teamUpdateActionError
				records[idx].Note = wrapped.Error()
				updated--
			}
		} else {
			// After internal/team/write.go's existence-aware dedup fix, a
			// record this loop decided to include should never come back in
			// AddResult.Skipped — StatusUpstreamUpdated/StatusMissing/forced
			// overwrite all imply the installed bytes differ from the fresh
			// render. If one still does, that is a genuine anomaly (the
			// classify-time view and the write-time view of the same file
			// disagreed, most likely a concurrent change), not a silent
			// success, so it is reported as an error instead of counted as
			// "updated".
			for _, skippedRec := range result.Skipped {
				key := lockRecordKey(skippedRec.Harness, skippedRec.AgentKey, skippedRec.DestPath)
				idx, ok := includedAt[key]
				if !ok {
					continue
				}
				wrapped := fmt.Errorf(
					"%s (%s, %s) was classified as changed but team.Add treated it as already up to date; left as-is — investigate a possible concurrent change",
					skippedRec.AgentKey, skippedRec.Harness, skippedRec.DestPath)
				recordErrs = append(recordErrs, wrapped)
				records[idx].Action = teamUpdateActionError
				records[idx].Note = wrapped.Error()
				updated--
			}
		}
	}

	sort.Slice(records, func(i, j int) bool {
		if records[i].DestPath != records[j].DestPath {
			return records[i].DestPath < records[j].DestPath
		}
		if records[i].AgentKey != records[j].AgentKey {
			return records[i].AgentKey < records[j].AgentKey
		}
		return records[i].Harness < records[j].Harness
	})

	out := teamUpdateJSON{
		Repo:    repoPath,
		Force:   force,
		Updated: updated,
		Skipped: skipped,
		Refused: refused,
		Records: records,
	}

	if len(recordErrs) > 0 || refused > 0 {
		summary := fmt.Errorf("%d updated, %d refused, %d error(s) out of %d tracked file(s)", updated, refused, len(recordErrs), len(records))
		return &out, errors.Join(append(recordErrs, summary)...)
	}
	return &out, nil
}

// runTeamUpdate implements the `team update` CLI command.
//
// Without --all-worktrees it resolves --repo, runs [teamUpdateWorktree]
// against it, and prints exactly what it always has: the dispatch here
// changes nothing about that path's calls, output shape, or returned error
// (AC-3). With --all-worktrees it additionally discovers every worktree git
// confirms belongs to the same repository (T-41, [discoverSiblingWorktrees])
// and applies [teamUpdateWorktree] to repoPath and to each sibling
// independently (US-15, [runTeamUpdateAllWorktrees]) — a failure in one
// worktree is reported against that worktree specifically and never blocks
// the others (AC-4).
func runTeamUpdate(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, force, allWorktrees, asJSON bool) (err error) {
	repoPath, err := resolveTeamRepoPath(opts.repo)
	if err != nil {
		return err
	}

	rt, err := openTeamRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	if allWorktrees {
		return runTeamUpdateAllWorktrees(ctx, env, rt, repoPath, force, asJSON)
	}

	out, updateErr := teamUpdateWorktree(ctx, rt, repoPath, force)
	if out == nil {
		return updateErr
	}
	if asJSON {
		if writeErr := writeTeamUpdateJSON(env.Stdout, *out); writeErr != nil {
			return writeErr
		}
	} else {
		writeTeamUpdateTable(env.Stdout, *out)
	}
	return updateErr
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

// newTeamStatusCommand declares "litefactory team status". Unlike list/show/
// add, status does not resolve or select a harness to render prospectively —
// every lockfile record already names its own harness — so it binds --repo
// directly rather than the shared teamRepoOptions.bind, which would also
// expose a --harness flag with no meaning here.
func newTeamStatusCommand() *Command {
	cmd := &Command{
		Name:  "team status",
		Usage: "team status [--repo path] [--json]",
		Short: "Report drift between installed agent files and their current source",
		Long: "Classify every file tracked by --repo's provenance lockfile into one of\n" +
			"five states — in sync, locally modified, upstream updated, conflict, or\n" +
			"missing — by comparing its installed bytes against a fresh render of its\n" +
			"agent's current source and the lockfile's recorded hashes. Locally\n" +
			"modified and conflicting files are flagged as at risk of being\n" +
			"discarded by a future `team update`. Nothing is written to disk. Exits\n" +
			"non-zero when any tracked file is not in sync.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		fs.StringVar(&opts.repo, "repo", ".",
			"git repository whose provenance lockfile is checked for drift")
		asJSON := fs.Bool("json", false, "emit JSON instead of a table")
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamStatus(ctx, env, cmd, opts, *asJSON)
		}
	}
	return cmd
}

// newTeamUpdateCommand declares "litefactory team update". Like team status,
// it does not resolve or select a harness to render prospectively — every
// lockfile record already names its own harness — so it binds --repo
// directly rather than the shared teamRepoOptions.bind, which would also
// expose a --harness flag with no meaning here.
func newTeamUpdateCommand() *Command {
	cmd := &Command{
		Name:  "team update",
		Usage: "team update [--repo path] [--force] [--all-worktrees] [--json]",
		Short: "Bring installed agent files back in line with their current source",
		Long: "Classify every file tracked by --repo's provenance lockfile exactly as\n" +
			"`team status` does, then write back whatever is safe to write\n" +
			"automatically: upstream-updated and missing files are re-rendered and\n" +
			"installed. Locally modified and conflicting files are left untouched\n" +
			"by default and reported by name and classification; pass --force to\n" +
			"overwrite them too — every file --force overwrites is named\n" +
			"explicitly in the output, never only counted. Writes go through\n" +
			"internal/team's existing write engine and update the lockfile record\n" +
			"for every file actually written. Acts only on --repo, and never\n" +
			"discovers or touches any other worktree of the same repository,\n" +
			"unless --all-worktrees is passed: then every worktree `git worktree\n" +
			"list` confirms belongs to the same repository is updated too, each\n" +
			"against its own .litefactory bookkeeping, independently — a hand-edit\n" +
			"or a failure in one worktree never blocks or force-overwrites another.",
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		fs.StringVar(&opts.repo, "repo", ".",
			"git repository whose provenance lockfile is brought up to date")
		force := fs.Bool("force", false, "overwrite locally modified or conflicting files (destructive)")
		allWorktrees := fs.Bool("all-worktrees", false,
			"also update every other worktree git confirms belongs to this repository")
		asJSON := fs.Bool("json", false, "emit JSON instead of a table")
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamUpdate(ctx, env, cmd, opts, *force, *allWorktrees, *asJSON)
		}
	}
	return cmd
}

// newTeamManageCommand declares "litefactory team manage <agent-key>
// set|unset|show". It points one agent, in one repo, at a local markdown
// file instead of whatever the team index says, so an operator can
// patch/customize a single agent for one repo without forking the whole
// team definition.
//
// Unlike list/show/add/status/update, manage never resolves a team index or
// a harness: an override is per (repo, agent key) only, so it binds --repo
// directly rather than the shared teamRepoOptions.bind, which would also
// expose a --harness flag with no meaning here.
func newTeamManageCommand() *Command {
	cmd := &Command{
		Name:  "team manage",
		Usage: "team manage <agent-key> <action> [<source>] [--repo path]  (set source=<path> | unset | show)",
		Short: "Point one agent, in one repo, at a local markdown file",
		Long: "Override a single agent's rendered source for one repo without\n" +
			"forking the whole team definition.\n\n" +
			"`set source=<path>` records <path> — a repo-relative markdown file\n" +
			"inside --repo — as <agent-key>'s active override; every subsequent\n" +
			"`team add`/`team update`/`team status` against --repo reads it in\n" +
			"place of the team index's own source for that key alone. `unset`\n" +
			"removes the override, reverting to the index-resolved source. `show`\n" +
			"reports whether an override is active and, if so, its local path.\n\n" +
			"Never changes an agent's install destination, the lockfile, or the\n" +
			"team index itself.",
		Args: ArgSpec{Names: []string{"agent-key", "action", "source"}, Min: 2},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts teamRepoOptions
		fs.StringVar(&opts.repo, "repo", ".", "git repository the override applies to")
		return func(ctx context.Context, env *Env, args []string) error {
			var rest string
			if len(args) > 2 {
				rest = args[2]
			}
			return runTeamManage(ctx, env, cmd, opts, args[0], args[1], rest)
		}
	}
	return cmd
}

// runTeamManage implements `team manage <agent-key> set|unset|show`. It
// never touches the team index, a lockfile, or a rendered artifact — it only
// records or reports which local markdown file, if any, currently
// supersedes agentKey's index-resolved source for repoPath at render time
// (internal/teamsource.readAgentSource).
func runTeamManage(ctx context.Context, env *Env, cmd *Command, opts teamRepoOptions, agentKey, action, sourceArg string) error {
	if !team.KeyPattern.MatchString(agentKey) {
		return &UsageError{Cmd: cmd, Err: &team.KeyError{Path: "agent-key", Key: agentKey}}
	}
	repoPath, err := resolveTeamRepoPath(opts.repo)
	if err != nil {
		return err
	}

	switch action {
	case "set":
		path, ok := strings.CutPrefix(sourceArg, "source=")
		if !ok || path == "" {
			return Usagef(cmd, "team manage %s set requires source=<path>, got %q", agentKey, sourceArg)
		}
		if err := team.SetOverride(repoPath, agentKey, path); err != nil {
			return teamManageError(cmd, err)
		}
		fmt.Fprintf(env.Stdout, "repo: %s\n", repoPath)
		fmt.Fprintf(env.Stdout, "agent: %s\n", agentKey)
		fmt.Fprintf(env.Stdout, "override set: %s\n", path)
		return nil

	case "unset":
		if sourceArg != "" {
			return Usagef(cmd, "team manage %s unset takes no source argument, got %q", agentKey, sourceArg)
		}
		if err := team.UnsetOverride(repoPath, agentKey); err != nil {
			return teamManageError(cmd, err)
		}
		fmt.Fprintf(env.Stdout, "repo: %s\n", repoPath)
		fmt.Fprintf(env.Stdout, "agent: %s\n", agentKey)
		fmt.Fprintln(env.Stdout, "override unset")
		return nil

	case "show":
		if sourceArg != "" {
			return Usagef(cmd, "team manage %s show takes no source argument, got %q", agentKey, sourceArg)
		}
		path, active, err := team.ActiveOverride(repoPath, agentKey)
		if err != nil {
			return teamManageError(cmd, err)
		}
		fmt.Fprintf(env.Stdout, "repo: %s\n", repoPath)
		fmt.Fprintf(env.Stdout, "agent: %s\n", agentKey)
		if active {
			fmt.Fprintln(env.Stdout, "override: active")
			fmt.Fprintf(env.Stdout, "path: %s\n", path)
		} else {
			fmt.Fprintln(env.Stdout, "override: none (index-resolved source in effect)")
		}
		return nil

	default:
		return Usagef(cmd, "team manage: unknown action %q; want set, unset, or show", action)
	}
}

// teamManageError translates internal/team's sentinel errors for
// SetOverride/UnsetOverride/ActiveOverride into this package's exit-code
// vocabulary: an invalid agent key is a usage mistake (ExitUsage); an
// override path that fails validateOverrideSource's checks (AC-4 — does not
// exist, is not repo-relative, is not a markdown file, ...) is a
// precondition the operator must fix before retrying (ExitPrecondition);
// anything else (an I/O failure, a corrupt overrides file, a refused
// symlink, ...) is left as a plain error (ExitFailure).
func teamManageError(cmd *Command, err error) error {
	var keyErr *team.KeyError
	if errors.As(err, &keyErr) {
		return &UsageError{Cmd: cmd, Err: err}
	}
	var pathErr *team.OverridePathError
	if errors.As(err, &pathErr) {
		return Precondition(err)
	}
	return err
}
