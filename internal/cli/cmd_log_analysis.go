package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/logevidence"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/standards"
)

func newLogAnalysisCommand(action string) *Command {
	name := "standards analyze logs " + action
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
		var evidenceRuns, reportBytes, proposals, digests int
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
			fs.IntVar(&evidenceRuns, "evidence-runs", 0, "lower raw-read run ceiling (default 20)")
			fs.IntVar(&reportBytes, "report-bytes", 0, "lower submitted report byte ceiling (default 131072)")
			fs.IntVar(&proposals, "proposals", 0, "lower proposed standard ceiling (default 3)")
			fs.IntVar(&calls, "calls", 0, "lower whole-analysis call ceiling (default 120)")
			fs.IntVar(&bytes, "bytes", 0, "lower encoded outbound byte ceiling (default 1048576)")
			fs.IntVar(&seconds, "seconds", 0, "lower wall time in seconds (default 1800)")
			fs.IntVar(&records, "records", 0, "lower evidence record ceiling (default 100)")
			fs.IntVar(&rows, "rows", 0, "lower discovery row ceiling, runs and incidents (default 400)")
			fs.IntVar(&dataCalls, "data-calls", 0, "lower evidence call ceiling (default 80)")
			fs.IntVar(&evidenceBytes, "evidence-bytes", 0, "lower encoded evidence byte ceiling (default 786432)")
			fs.IntVar(&digests, "digests", 0, "lower condensed run view ceiling (default 40)")
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
				var categories []standards.Category
				if binding, be := rt.db.GetAnalystStandardsBinding(ctx, view.Invocation.ID); be == nil && len(binding.SnapshotBytes) > 0 {
					var snap standards.Snapshot
					if json.Unmarshal(binding.SnapshotBytes, &snap) == nil {
						categories = snap.Categories
					}
				}
				_, e = io.WriteString(env.Stdout, renderLogAnalysis(view, categories))
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
				q := loganalysis.Request{StartKey: key, TaskRefs: tasks, Limits: loganalysis.Limits{ReportBytes: reportBytes, Proposals: proposals, Calls: calls, Bytes: bytes, Seconds: seconds, Evidence: logevidence.Limits{Runs: evidenceRuns, Records: records, Rows: rows, Calls: dataCalls, Bytes: evidenceBytes, Digests: digests}}}
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
					_, e := fmt.Fprintf(env.Stderr, "Saved analysis %s; inspect with standards analyze logs show %s\n", id, id)
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
Inspect: standards analyze logs list; standards analyze logs show ID [--json] [--after ROW --limit N].
Reread: standards analyze logs reread ID --citation JSON. This is a separate bounded owner-authorized pass.
Recovery: standards analyze logs recover ID after its deadline; accepted results remain unchanged. A start-key retry only returns the original invocation.
The analyst decides what to read and what to conclude; show lists what it actually read next to its result. It proposes at most three standards, which stay unapproved until the owner reviews them.
Defaults: 400 discovery rows (runs and incidents), 40 condensed run views, raw reads from 20 runs and 100 records, 80 data calls, 768 KiB encoded evidence; one model invocation, 30 minutes, 120 broker calls, 1 MiB encoded outbound data, 128 KiB report. Limits may only be lowered.
Known-secret redaction cannot remove every unknown secret or personal detail. Retained references do not extend log retention.`

// renderLogAnalysis shows a report for the owner: what was read, the
// proposed standards as the agents would read them, and the other findings.
func renderLogAnalysis(view loganalysis.Inspection, categories []standards.Category) string {
	var b strings.Builder
	a := view.Invocation
	fmt.Fprintf(&b, "Analysis %s: %s / %s\nScope: %s\n", a.ID, a.State, a.Disposition, a.Scope)
	status := view.Standards.Status
	fmt.Fprintf(&b, "Book: revision %d (%s); selection %s; retrieval %s\n", status.Binding.RevisionID, status.Integrity, status.SelectionStatus, status.RetrievalStatus)
	read := view.Read
	fmt.Fprintf(&b, "Read: %d incidents, %d runs listed, digests of %d runs %v, raw excerpts from %d runs %v", read.IncidentsOffered, read.RunsListed, len(read.Digests), read.Digests, len(read.RawReads), read.RawReads)
	if read.Withheld > 0 {
		fmt.Fprintf(&b, "; %d deliveries withheld", read.Withheld)
	}
	if read.BudgetExhausted {
		b.WriteString("; budget exhausted")
	}
	fmt.Fprintf(&b, "\nUsage: %d calls, %d encoded bytes\n", a.Calls, a.OutboundBytes)
	var report loganalysis.Report
	if len(view.Report) == 0 || json.Unmarshal(view.Report, &report) != nil {
		fmt.Fprintf(&b, "No report. %s\n", a.Reason)
		return b.String()
	}
	fmt.Fprintf(&b, "\nSummary\n%s\n", strings.TrimSpace(report.Summary))
	links := map[string]string{}
	for _, l := range view.Proposals {
		links[l.CandidateKey] = fmt.Sprintf("unapproved proposal %s version %d", l.ProposalID, l.Version)
	}
	for i, p := range report.Proposals {
		link := links[fmt.Sprintf("proposal-%d", i+1)]
		fmt.Fprintf(&b, "\nProposal %d: %s, %s\n\n", i+1, p.Operation.Kind, link)
		known := categories
		if p.NewCategory != nil {
			fmt.Fprintf(&b, "New category %s: %s. %s\n\n", p.NewCategory.ID, p.NewCategory.Name, p.NewCategory.Description)
			known = append([]standards.Category{*p.NewCategory}, categories...)
		}
		if p.Operation.Standard != nil {
			b.WriteString(standards.Render(known, []standards.Standard{*p.Operation.Standard}))
		}
		j := p.Justification
		fmt.Fprintf(&b, "Incidents: %s\nTasks: %s\nCost: %s\nWhy it prevents: %s\nLoad after: %s\n", strings.Join(j.IncidentIDs, ", "), strings.Join(j.Tasks, ", "), j.Cost, j.WhyItPrevents, j.LoadAfter)
		writeCitations(&b, p.Citations)
	}
	if len(report.NotBook) > 0 {
		b.WriteString("\nNot for the book (Bug candidates for the owner)\n")
		for _, n := range report.NotBook {
			fmt.Fprintf(&b, "- %s, %s: %s\n  Incidents: %s\n", n.Kind, n.Target, n.Finding, strings.Join(n.IncidentIDs, ", "))
			writeCitations(&b, n.Citations)
		}
	}
	if len(report.Proposals) == 0 && report.NoChangeReason != "" {
		fmt.Fprintf(&b, "\nNo change: %s\n", report.NoChangeReason)
	}
	return b.String()
}

func writeCitations(b *strings.Builder, citations []logevidence.Citation) {
	if len(citations) == 0 {
		return
	}
	parts := make([]string, 0, len(citations))
	for _, c := range citations {
		parts = append(parts, fmt.Sprintf("run %d [%d:%d]", c.RunID, c.Start, c.End))
	}
	fmt.Fprintf(b, "  Citations: %s\n", strings.Join(parts, ", "))
}
