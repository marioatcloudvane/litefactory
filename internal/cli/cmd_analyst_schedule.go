package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/analystschedule"
	"github.com/cloudvane/litefactory/internal/store"
)

type analystScheduleFlags struct {
	config, key, reason, scheduleAfter string
	version, after, occurrence         int64
	limit                              int
	asJSON                             bool
}

func newAnalystScheduleCommand(action string) *Command {
	name := "standards schedule " + action
	c := &Command{Name: name, Usage: name + " [--json]", Short: "Manage opt-in recurring standards analysis and retained activity", Long: analystScheduleUsage}
	switch action {
	case "show", "change", "enable", "disable", "reanalyze", "activity", "activity show", "notices", "notices show":
		c.Args = ArgSpec{Names: []string{"id"}, Min: 1}
		c.Usage = name + " <id> [--json]"
	}
	c.New = func(fs *flag.FlagSet) Handler {
		o := analystScheduleFlags{}
		fs.BoolVar(&o.asJSON, "json", false, "emit retained JSON")
		switch action {
		case "create", "change":
			fs.StringVar(&o.config, "config", "", "configuration JSON file; - reads stdin")
		}
		if action == "create" {
			fs.StringVar(&o.key, "key", "", "required idempotent creation identity")
		}
		switch action {
		case "show", "change", "enable", "disable", "reanalyze":
			fs.Int64Var(&o.version, "version", 0, "required expected schedule version")
		}
		if action == "reanalyze" {
			fs.Int64Var(&o.occurrence, "occurrence", 0, "prior occurrence to deliberately reanalyze")
			fs.StringVar(&o.reason, "reason", "", "required operator reason")
		}
		switch action {
		case "list":
			fs.StringVar(&o.scheduleAfter, "after", "", "exclusive stable schedule ID cursor")
			fs.IntVar(&o.limit, "limit", 20, "page size, maximum 100")
		case "activity", "notices":
			fs.Int64Var(&o.after, "after", 0, "exclusive stable row ID cursor")
			fs.IntVar(&o.limit, "limit", 20, "page size, maximum 100")
		}
		return func(ctx context.Context, env *Env, args []string) error {
			if o.after < 0 || ((action == "list" || action == "activity" || action == "notices") && (o.limit < 1 || o.limit > 100)) {
				return Usagef(c, "invalid page; after must be nonnegative and limit 1..100")
			}
			return runAnalystSchedule(ctx, env, action, args, o)
		}
	}
	return c
}

func readAnalystScheduleConfig(env *Env, path string, target any) error {
	if path == "" {
		return Usagef(nil, "--config is required; use - for stdin")
	}
	r := env.Stdin
	if path != "-" {
		f, err := os.Open(path) // #nosec G304 -- explicit local operator input.
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	if r == nil {
		return Usagef(nil, "stdin is unavailable")
	}
	const maximum = 64 << 10
	raw, err := io.ReadAll(io.LimitReader(r, maximum+1))
	if err != nil {
		return err
	}
	if len(raw) > maximum {
		return Usagef(nil, "schedule configuration exceeds 64 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err = dec.Decode(target); err != nil {
		return Usagef(nil, "schedule configuration: %v", err)
	}
	var extra any
	if err = dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Usagef(nil, "schedule configuration must contain exactly one JSON object")
	}
	return nil
}

func writeAnalystSchedule(env *Env, value any, asJSON bool) error {
	if asJSON {
		return writeJSON(env, value)
	}
	switch v := value.(type) {
	case store.AnalystSchedule:
		_, err := fmt.Fprintf(env.Stdout, "Schedule %s: %s (%s), version %d, generation %d, enabled %t\nRepository: %d; interval: %ds; anchor: %s; next due: %s; paused since: %s\nOperator: %s; reanalysis of: %d; reason: %s\nConfiguration: %s\n", v.ID, v.Name, v.Kind, v.Version, v.Generation, v.Enabled, v.RepositoryID, v.IntervalSeconds, v.Anchor.Format(time.RFC3339Nano), v.NextDue.Format(time.RFC3339Nano), v.PausedAt.Format(time.RFC3339Nano), v.Operator, v.ReanalysisOf, v.ReanalysisReason, v.Config)
		return err
	case []store.AnalystSchedule:
		for _, row := range v {
			if err := writeAnalystSchedule(env, row, false); err != nil {
				return err
			}
		}
		if len(v) > 0 {
			_, err := fmt.Fprintf(env.Stdout, "Next schedule cursor: %s\n", v[len(v)-1].ID)
			return err
		}
		return nil
	case store.AnalystOccurrence:
		_, err := fmt.Fprintf(env.Stdout, "Occurrence %d: %s / %s; schedule %s version %d generation %d\nNominal: %s through %s; missed slots: %d; cutoff: %s\nCreated: %s; launched: %s; finished: %s\nAnalysis: %s; more pending: %t; reason: %s\nSaved configuration: %s\nStandards: %s\nCoverage: %s\n", v.ID, v.State, v.Disposition, v.ScheduleID, v.Version, v.Generation, v.FirstDue.Format(time.RFC3339Nano), v.LastDue.Format(time.RFC3339Nano), v.MissedSlots, v.Cutoff.Format(time.RFC3339Nano), v.CreatedAt.Format(time.RFC3339Nano), v.LaunchedAt.Format(time.RFC3339Nano), v.FinishedAt.Format(time.RFC3339Nano), v.AnalysisID, v.MorePending, v.Reason, v.Config, v.Standards, v.Coverage)
		if err != nil {
			return err
		}
		for _, w := range v.Works {
			if _, err = fmt.Fprintf(env.Stdout, "Source %s run %d task %s issue %d: %s; offered %t cited %t covered %t; gap: %s\n", w.SourceKey, w.RunID, w.TaskRef, w.IssueNumber, w.State, w.Offered, w.Cited, w.Covered, w.Reason); err != nil {
				return err
			}
		}
		return nil
	case []store.AnalystOccurrence:
		for _, row := range v {
			if err := writeAnalystSchedule(env, row, false); err != nil {
				return err
			}
		}
		if len(v) > 0 {
			_, err := fmt.Fprintf(env.Stdout, "Next activity cursor: %d\n", v[len(v)-1].ID)
			return err
		}
		return nil
	case store.AnalystProposalNotice:
		_, err := fmt.Fprintf(env.Stdout, "Notice %d: %s proposal %s version %d base %d change digest %s result digest %s\nSchedule: %s config %d; occurrence: %d; analysis: %s; created: %s; proposal origin: %s; schema: %d\n", v.ID, v.OriginStatus, v.ProposalID, v.ProposalVersion, v.BaseRevision, v.ChangeDigest, v.ResultDigest, v.ScheduleID, v.Version, v.OccurrenceID, v.AnalysisID, v.CreatedAt.Format(time.RFC3339Nano), v.OriginAt.Format(time.RFC3339Nano), v.SchemaVersion)
		return err
	case []store.AnalystProposalNotice:
		for _, row := range v {
			if err := writeAnalystSchedule(env, row, false); err != nil {
				return err
			}
		}
		if len(v) > 0 {
			_, err := fmt.Fprintf(env.Stdout, "Next notice cursor: %d\n", v[len(v)-1].ID)
			return err
		}
		return nil
	}
	return fmt.Errorf("unsupported schedule output %T", value)
}

const analystScheduleUsage = `Create: standards schedule create --key ID --config FILE|-; creation is disabled and launches nothing.
Example configuration: {"name":"daily logs","kind":"logs","repository_id":1,"interval_seconds":86400,"run_limit":5}. Use kind bugs for Bug-run issue mapping; drift also requires explicit base and ref. Optional task_refs, modes, after (UTC RFC3339), paths/issues and lower log_limits/bug_limits/drift_limits restrict scope.
Enable, disable and change require --version VERSION. Enable explicitly opts in and makes the first pass due immediately.
Configuration uses a fixed UTC elapsed interval (default one day, minimum one hour). Recent runs begin seven days before first enable unless an explicit start is supplied; the saved lower bound does not slide.
While the daemon is stopped no work executes. Missed slots coalesce into one bounded pass; disabled periods remain paused. Newest finished runs have priority; older backlog and unresolved coverage remain visible.
Inspect a retained configuration: standards schedule show ID --version VERSION.
Inspect: standards schedule list; show ID; activity ID; activity show OCCURRENCE; notices ID; notices show NOTICE. Use --json and --after ROW --limit N for bounded pages.
Execution state and finding disposition are separate. Selected, offered, cited and covered evidence differ; partial work, missing evidence and exhausted limits remain gaps.
Reanalysis requires a prior --occurrence, --version and --reason and records a deliberate new generation. Ordinary recovery checks retained receipts first and never silently relaunches a started analysis or renews its budgets.
Each pass keeps the selected analyst's evidence, tool, standards and provider limits. Retained notices refer to exact original unapproved proposal versions; human approval and publication are separate.
Provider-free inspection needs no model. No external notices are delivered. References do not extend evidence retention; independently produced similar proposals are not universally deduplicated.
Provider execution and charging cannot be guaranteed exactly once. SQLite WAL synchronous=NORMAL can lose recent commits on power loss; disk loss and administrator edits are outside ordinary restart recovery.`

func runAnalystSchedule(ctx context.Context, env *Env, action string, args []string, o analystScheduleFlags) (err error) {
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	svc := &analystschedule.Service{DB: rt.db}
	var value any
	switch action {
	case "create", "change", "enable", "disable", "reanalyze":
		owner, e := rt.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
		if e != nil {
			return e
		}
		if owner.UserID <= 0 {
			return Preconditionf("configured numeric owner identity required")
		}
		operator := fmt.Sprintf("github:%d", owner.UserID)
		now := time.Now().UTC()
		if action != "create" && o.version <= 0 {
			return Usagef(nil, "--version must name the current positive schedule version")
		}
		switch action {
		case "create", "change":
			var config analystschedule.Config
			if e = readAnalystScheduleConfig(env, o.config, &config); e != nil {
				return e
			}
			if action == "create" {
				value, err = svc.Create(ctx, o.key, operator, config, now)
			} else {
				value, err = svc.Change(ctx, args[0], o.version, operator, config, now)
			}
		case "enable":
			value, err = svc.Enable(ctx, args[0], o.version, operator, now)
		case "disable":
			value, err = svc.Disable(ctx, args[0], o.version, operator, now)
		case "reanalyze":
			if o.occurrence <= 0 || o.reason == "" {
				return Usagef(nil, "--occurrence and --reason are required")
			}
			value, err = svc.Reanalyze(ctx, args[0], o.version, o.occurrence, o.reason, operator, now)
		}
	default:
		value, err = inspectAnalystSchedule(ctx, rt.db, action, args, o)
	}
	if err != nil {
		return err
	}
	return writeAnalystSchedule(env, value, o.asJSON)
}

func inspectAnalystSchedule(ctx context.Context, db *store.DB, action string, args []string, o analystScheduleFlags) (any, error) {
	switch action {
	case "list":
		return db.ListAnalystSchedules(ctx, o.scheduleAfter, o.limit)
	case "show":
		if o.version > 0 {
			return db.GetAnalystScheduleVersion(ctx, args[0], o.version)
		}
		return db.GetAnalystSchedule(ctx, args[0])
	case "activity":
		if _, err := db.GetAnalystSchedule(ctx, args[0]); err != nil {
			return nil, err
		}
		return db.ListAnalystOccurrences(ctx, args[0], o.after, o.limit)
	case "notices":
		if _, err := db.GetAnalystSchedule(ctx, args[0]); err != nil {
			return nil, err
		}
		return db.ListAnalystProposalNotices(ctx, args[0], o.after, o.limit)
	case "activity show", "notices show":
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || id <= 0 {
			return nil, Usagef(nil, "expected a positive retained row ID")
		}
		if action == "activity show" {
			return db.GetAnalystOccurrence(ctx, id)
		}
		return db.GetAnalystProposalNotice(ctx, id)
	}
	return nil, Usagef(nil, "unknown schedule action")
}
