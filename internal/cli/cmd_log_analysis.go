package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
)

func newLogAnalysisCommand(action string) *Command {
	name := "standards analysis " + action
	if action == "start" {
		name = "standards analyze logs"
	}
	c := &Command{Name: name, Usage: name + " [--json]", Short: "Start or inspect bounded execution-log analysis", Long: logAnalysisUsage}
	if action == "show" || action == "reread" || action == "recover" {
		c.Args = ArgSpec{Names: []string{"analysis-id"}, Min: 1}
		c.Usage = name + " <analysis-id> [--json]"
	}
	if action == "start" {
		c.Usage = name + " --repo forge:owner/name (--run ID | --after UTC --before UTC) [--json]"
	}
	if action == "reread" {
		c.Usage = name + " <analysis-id> --citation JSON"
	}
	c.New = func(fs *flag.FlagSet) Handler {
		var repos, runs, tasks logRepositoryFlags
		var after, before, key, cursor string
		var asJSON bool
		var limit, calls, bytes, seconds, records, rows, dataCalls, evidenceBytes int
		var evidenceRuns, reportBytes, proposals int
		var evidenceAfter int64
		var citation string
		fs.BoolVar(&asJSON, "json", false, "emit retained JSON")
		switch action {
		case "start":
			fs.Var(&repos, "repo", "registered forge:owner/name (repeatable)")
			fs.Var(&runs, "run", "stable run ID, at most five (repeatable)")
			fs.Var(&tasks, "task", "optional exact task filter (repeatable)")
			fs.StringVar(&after, "after", "", "closed UTC interval start (RFC3339 Z)")
			fs.StringVar(&before, "before", "", "closed UTC interval end (RFC3339 Z)")
			fs.StringVar(&key, "start-key", "", "idempotent caller identity; retry never relaunches")
			fs.IntVar(&evidenceRuns, "evidence-runs", 0, "lower evidence run ceiling (default 5)")
			fs.IntVar(&reportBytes, "report-bytes", 0, "lower submitted report byte ceiling (default 131072)")
			fs.IntVar(&proposals, "proposals", 0, "lower proposed standard ceiling (default 10)")
			fs.IntVar(&calls, "calls", 0, "lower whole-analysis call ceiling (default 40)")
			fs.IntVar(&bytes, "bytes", 0, "lower encoded outbound byte ceiling (default 262144)")
			fs.IntVar(&seconds, "seconds", 0, "lower wall time in seconds (default 600)")
			fs.IntVar(&records, "records", 0, "lower evidence record ceiling (default 100)")
			fs.IntVar(&rows, "rows", 0, "lower discovery row ceiling (default 50)")
			fs.IntVar(&dataCalls, "data-calls", 0, "lower evidence call ceiling (default 10)")
			fs.IntVar(&evidenceBytes, "evidence-bytes", 0, "lower encoded evidence byte ceiling (default 32768)")
		case "list":
			fs.StringVar(&cursor, "after", "", "stable analysis ID page cursor")
			fs.IntVar(&limit, "limit", 20, "page size, maximum 100")
		case "show":
			fs.Int64Var(&evidenceAfter, "after", 0, "evidence row cursor")
			fs.IntVar(&limit, "limit", 20, "evidence page size, maximum 100")
		case "reread":
			fs.StringVar(&citation, "citation", "", "exact retained citation JSON; separate bounded authorized read")
		}
		return func(ctx context.Context, env *Env, args []string) (err error) {
			rt, err := openRepoRuntime(ctx)
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, rt.close()) }()
			executable, err := os.Executable()
			if err != nil {
				return err
			}
			svc := &loganalysis.Service{DB: rt.db, RunsRoot: rt.paths.RunsDir(), Executable: executable, Redactor: redact.New(redact.Options{Logger: env.Logger, Environ: os.Environ()})}
			switch action {
			case "list":
				if limit < 1 || limit > 100 {
					return Usagef(nil, "limit must be 1..100")
				}
				list, e := rt.db.ListLogAnalyses(ctx, cursor, limit)
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, list)
				}
				for _, a := range list {
					if _, e = fmt.Fprintf(env.Stdout, "%s  %s  %s\n", a.ID, a.State, a.Disposition); e != nil {
						return e
					}
				}
				return nil
			case "show":
				if limit < 1 || limit > 100 || evidenceAfter < 0 {
					return Usagef(nil, "invalid evidence page")
				}
				view, e := svc.Inspect(ctx, args[0], evidenceAfter, limit)
				if e != nil {
					return e
				}
				if asJSON {
					return writeJSON(env, view)
				}
				if _, e = fmt.Fprintf(env.Stdout, "Analysis %s: %s / %s\nRequested scope: %s\nLimits: %s\nBook: revision %d digest %s (%s); selection %s; retrieval %s\nUsage: %d calls, %d encoded bytes\nReason: %s\n", view.Invocation.ID, view.Invocation.State, view.Invocation.Disposition, view.Invocation.Scope, view.Invocation.Limits, view.Standards.Status.Binding.RevisionID, view.Standards.Status.Binding.Digest, view.Standards.Status.Integrity, view.Standards.Status.SelectionStatus, view.Standards.Status.RetrievalStatus, view.Invocation.Calls, view.Invocation.OutboundBytes, view.Invocation.Reason); e != nil {
					return e
				}
				if selection := view.Standards.Status.Selection; selection != nil {
					if _, e = fmt.Fprintf(env.Stdout, "Selected standards: version %d digest %s; categories %v; standards %v\n", selection.Version, selection.Digest, selection.CategoryIDs, selection.StandardIDs); e != nil {
						return e
					}
				}
				for _, row := range view.Evidence {
					if _, e = fmt.Fprintf(env.Stdout, "Coverage %d: %s\n", row.ID, row.Metadata); e != nil {
						return e
					}
				}
				if len(view.Report) > 0 {
					if _, e = fmt.Fprintf(env.Stdout, "Findings: %s\n", view.Report); e != nil {
						return e
					}
				}
				for _, p := range view.Proposals {
					if _, e = fmt.Fprintf(env.Stdout, "Unapproved proposal %s version %d base %d change digest %s result digest %s (%s)\n", p.ProposalID, p.Version, p.BaseRevision, p.ChangeDigest, p.ResultDigest, p.CandidateKey); e != nil {
						return e
					}
				}
				_, e = fmt.Fprintf(env.Stdout, "Next evidence cursor: %d\n%s\n", view.EvidenceNext, logAnalysisUsage)
				return e
			case "recover":
				// Never relaunch a retained invocation. Only an expired retained deadline may be fenced.
				a, e := rt.db.GetLogAnalysis(ctx, args[0])
				if e != nil {
					return e
				}
				var limits loganalysis.Limits
				if e = json.Unmarshal(a.Limits, &limits); e != nil {
					return e
				}
				if a.State == "running" && time.Now().Before(a.CreatedAt.Add(time.Duration(limits.Seconds)*time.Second)) {
					return errors.New("analysis deadline has not expired; recovery cannot fence an active invocation")
				}
				if e = rt.db.InterruptLogAnalysis(ctx, a.ID, a.Generation, "incomplete", "orphaned invocation interrupted; no model relaunch or budget reset"); e != nil {
					return e
				}
				view, e := svc.Inspect(ctx, a.ID, 0, 20)
				if e != nil {
					return e
				}
				return writeJSON(env, view)
			case "reread":
				var c logevidence.Citation
				if e := json.Unmarshal([]byte(citation), &c); e != nil {
					return Usagef(nil, "--citation must be exact retained citation JSON")
				}
				a, e := rt.db.GetLogAnalysis(ctx, args[0])
				if e != nil {
					return e
				}
				var scope loganalysis.Scope
				if e = json.Unmarshal(a.Scope, &scope); e != nil {
					return e
				}
				if e = initializeLogCredentials(ctx, env, rt.db, rt.cfg, scope.Request.RepositoryIDs, svc.Redactor); e != nil {
					return e
				}
				out, e := svc.Reread(ctx, args[0], c)
				if e != nil {
					return e
				}
				return writeJSON(env, out)
			case "start":
				q := loganalysis.Request{StartKey: key, TaskRefs: tasks, Limits: loganalysis.Limits{ReportBytes: reportBytes, Proposals: proposals, Calls: calls, Bytes: bytes, Seconds: seconds, Evidence: logevidence.Limits{Runs: evidenceRuns, Records: records, Rows: rows, Calls: dataCalls, Bytes: evidenceBytes}}}
				for _, s := range repos {
					forge, slug, ok := strings.Cut(s, ":")
					owner, name, valid := strings.Cut(slug, "/")
					if !ok || !valid || strings.Contains(name, "/") {
						return Usagef(nil, "--repo requires registered forge:owner/name")
					}
					r, e := rt.db.GetRepoBySlug(ctx, forge, owner, name)
					if e != nil {
						return e
					}
					q.RepositoryIDs = append(q.RepositoryIDs, r.ID)
				}
				for _, s := range runs {
					id, e := strconv.ParseInt(s, 10, 64)
					if e != nil {
						return Usagef(nil, "--run requires a stable positive run ID")
					}
					q.RunIDs = append(q.RunIDs, id)
				}
				if after != "" {
					q.After, err = time.Parse(time.RFC3339Nano, after)
					if err != nil {
						return err
					}
				}
				if before != "" {
					q.Before, err = time.Parse(time.RFC3339Nano, before)
					if err != nil {
						return err
					}
				}
				if err = initializeLogCredentials(ctx, env, rt.db, rt.cfg, q.RepositoryIDs, svc.Redactor); err != nil {
					return err
				}
				svc.DisableMCP = !rt.cfg.Settings.MCPAutoStart
				svc.Runner = env.LogAnalyst
				svc.Model, _ = rt.cfg.Tiers.ResolveModel(config.Kind("claude"), config.TierFlagship)
				if svc.Runner == nil && rt.cfg.Agents.Claude != nil {
					runner, e := harness.New(harness.Options{Paths: rt.paths, Claude: *rt.cfg.Agents.Claude, Model: svc.Model, Environ: os.Environ(), Logger: env.Logger})
					if e == nil {
						svc.Runner = runner
					}
				}
				svc.Started = func(id string) error {
					_, e := fmt.Fprintf(env.Stderr, "Saved analysis %s; inspect with standards analysis show %s\n", id, id)
					return e
				}
				a, e := svc.Start(ctx, q)
				if a.ID != "" {
					if asJSON {
						err = writeJSON(env, a)
					} else {
						_, err = fmt.Fprintf(env.Stdout, "Analysis %s: %s / %s\n", a.ID, a.State, a.Disposition)
					}
				}
				return errors.Join(e, err)
			}
			return errors.New("unknown log analysis action")
		}
	}
	return c
}

const logAnalysisUsage = `Start: standards analyze logs --repo forge:owner/name (--run ID [--run ID] | --after UTC --before UTC).
Inspect: standards analysis list; standards analysis show ID [--json] [--after ROW --limit N].
Reread: standards analysis reread ID --citation JSON. This is a separate bounded owner-authorized pass.
Recovery: standards analysis recover ID after its deadline; accepted results remain unchanged. A start-key retry only returns the original invocation.
Defaults: five evidence runs, 100 records, 50 discovery rows, ten data calls, 32 KiB encoded evidence; one model invocation, ten minutes, 40 broker calls, 256 KiB encoded outbound data, 128 KiB report, ten proposals. Limits may only be lowered.
Requested, discovered, offered and cited evidence differ; unread runs, unknown interval remainder, omissions and exhausted limits are coverage gaps. EOF describes retained source only.
Retained references do not extend log retention. Rereads may report missing or changed sources. Known-secret redaction cannot remove every unknown secret or personal detail.
Observations and proposals do not establish root cause; repeated messages need not be independent incidents. Proposals require separate human review, approval and publication.
Execution state differs from finding disposition: proposals, no warranted change, insufficient evidence or not evaluated. Provider failure is an execution failure.
SQLite WAL synchronous=NORMAL may lose recent commits on power failure. A new explicit analysis may charge again and yield different findings.`
