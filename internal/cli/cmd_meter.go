package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

type meterOptions struct {
	mode, codingAgent, model string
	json, details            bool
	issue                    int64
	from, to                 string
}

func newMeterCommand() *Command {
	return &Command{Name: "meter", Usage: "meter [<repository> [<task-id>]] [--mode MODE] [--ca AGENT] [--model MODEL] [--json] [--details]", Short: "Show measured tokens, frozen estimates and missing coverage", Args: ArgSpec{Names: []string{"repository", "task-id"}}, New: func(fs *flag.FlagSet) Handler {
		var o meterOptions
		fs.StringVar(&o.mode, "mode", "", "filter research, plan, implement or spike")
		fs.StringVar(&o.codingAgent, "ca", "", "filter claude or codex")
		fs.StringVar(&o.model, "model", "", "filter actual model; unresolved attribution remains visible")
		fs.BoolVar(&o.json, "json", false, "emit JSON")
		fs.BoolVar(&o.details, "details", false, "include run, attempt and invocation rows")
		return func(ctx context.Context, env *Env, args []string) error { return runMeter(ctx, env, args, o, false) }
	}}
}
func newMeterExportCommand() *Command {
	return &Command{Name: "meter export", Usage: "meter export <project> [--coding_agent AGENT] [--issue NUMBER] [--mode MODE] [--time_from RFC3339] [--time_to RFC3339]", Short: "Export one registered GitHub project's measurements as CSV", Long: "CSV is written to stdout. Filters combine with AND; time bounds select entire attempts by start time, from inclusive and to exclusive. Unknown values are blank. Snapshot limits: 1,000,000 rows, 256 MiB encoded CSV, 30 seconds preparation. Daemon helper overhead excluded. See docs/meter.md for columns and reversible spreadsheet text escaping.", Args: ArgSpec{Names: []string{"project"}, Min: 1}, New: func(fs *flag.FlagSet) Handler {
		var o meterOptions
		fs.StringVar(&o.codingAgent, "coding_agent", "", "filter claude or codex")
		fs.Func("issue", "positive project-local issue number", func(raw string) error {
			n, e := strconv.ParseInt(raw, 10, 64)
			if e != nil || n <= 0 {
				return fmt.Errorf("issue must be positive")
			}
			o.issue = n
			return nil
		})
		fs.StringVar(&o.mode, "mode", "", "filter research, plan, implement or spike")
		fs.StringVar(&o.from, "time_from", "", "inclusive attempt start instant, RFC3339")
		fs.StringVar(&o.to, "time_to", "", "exclusive attempt start instant, RFC3339")
		return func(ctx context.Context, env *Env, args []string) error {

			return runMeter(ctx, env, args, o, true)
		}
	}}
}

func meterFilter(args []string, o meterOptions, export bool) (store.MeterFilter, string, error) {
	f := store.MeterFilter{Mode: o.mode, CodingAgent: o.codingAgent, Model: o.model, Issue: o.issue}
	if o.mode != "" && o.mode != "research" && o.mode != "plan" && o.mode != "implement" && o.mode != "spike" {
		return f, "", Usagef(nil, "invalid mode")
	}
	if o.codingAgent != "" && o.codingAgent != "claude" && o.codingAgent != "codex" {
		return f, "", Usagef(nil, "invalid coding agent")
	}
	if len(o.model) > 256 || strings.ContainsAny(o.model, "\x00\r\n\t") {
		return f, "", Usagef(nil, "invalid model")
	}
	if f.Issue < 0 {
		return f, "", Usagef(nil, "issue must be positive")
	}
	if export && len(args) != 1 || !export && len(args) > 2 {
		return f, "", Usagef(nil, "invalid metering scope")
	}
	project := ""
	if len(args) > 0 {
		project = args[0]
	}
	if export && !strings.HasPrefix(project, "github:") {
		return f, "", Usagef(nil, "export requires github:OWNER/REPO")
	}
	if len(args) == 2 {
		n, e := strconv.ParseInt(args[1], 10, 64)
		if e != nil || n <= 0 {
			return f, "", Usagef(nil, "task-id must be a positive issue number")
		}
		f.Issue = n
	}
	for _, bound := range []struct {
		raw  string
		dest **time.Time
	}{{o.from, &f.TimeFrom}, {o.to, &f.TimeTo}} {
		if bound.raw != "" {
			t, e := time.Parse(time.RFC3339Nano, bound.raw)
			if e != nil {
				return f, "", Usagef(nil, "time bounds must be timezone-aware RFC3339")
			}
			t = t.UTC()
			*bound.dest = &t
		}
	}
	if f.TimeFrom != nil && f.TimeTo != nil && !f.TimeFrom.Before(*f.TimeTo) {
		return f, "", Usagef(nil, "time_from must precede time_to")
	}
	return f, project, nil
}

func resolveMeterProject(ctx context.Context, db *store.DB, selector string) (int64, error) {
	if selector == "" {
		return 0, nil
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return 0, err
	}
	var matched int64
	for _, r := range repos {
		if selector == r.Path || selector == r.Slug() || selector == r.Forge+":"+r.Slug() {
			if matched != 0 {
				return 0, Usagef(nil, "repository selector is ambiguous")
			}
			matched = r.ID
		}
	}
	if matched == 0 {
		return 0, Usagef(nil, "unknown registered repository")
	}
	return matched, nil
}

func runMeter(ctx context.Context, env *Env, args []string, o meterOptions, export bool) (err error) {
	f, project, err := meterFilter(args, o, export)
	if err != nil {
		return err
	}
	if export {
		ref, e := naming.ParseRef(project + "#1")
		if e != nil || ref.Forge != "github" {
			return Usagef(nil, "export requires github:OWNER/REPO")
		}
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	f.RepoID, err = resolveMeterProject(ctx, rt.db, project)
	if err != nil {
		return err
	}
	return writeMeter(ctx, env.Stdout, rt.db, f, o, export)
}

func writeMeter(ctx context.Context, out io.Writer, db *store.DB, f store.MeterFilter, o meterOptions, export bool) (err error) {
	options := store.MeterSnapshotOptions{}
	if export {
		options.HeaderBytes = len(meterCSVHeaderLine)
		options.RowBytes = func(r store.MeterRow) (int, error) { b, e := meterCSVBytes(r); return len(b), e }
	}
	snapshot, err := db.NewMeterSnapshot(ctx, f, options)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, snapshot.Close(ctx)) }()
	if export {
		return writeMeterCSV(ctx, out, snapshot)
	}
	summary := report.MeterSummary{CollectionStatus: "unknown", PricingStatus: "unknown", HelperOverhead: "excluded"}
	rows := []store.MeterRow{}
	groups := map[string]*meterGroup{}
	for {
		page, e := snapshot.Next(ctx, 256)
		if e != nil {
			return e
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if row.CollectionStatus != "complete" || row.PricingStatus != "priced" {
				row.EstimatedUSD = nil
			}
			if e := summary.Add(meterReportValues(row)); e != nil {
				return e
			}
			keyBytes, _ := json.Marshal([]string{row.Project, strconv.FormatInt(row.Issue, 10), row.Mode, row.CodingAgent, strconv.FormatBool(row.ActualModel != nil), meterAmount(row.ActualModel)})
			key := string(keyBytes)
			group := groups[key]
			if group == nil {
				group = &meterGroup{Project: row.Project, Issue: row.Issue, Mode: row.Mode, CodingAgent: row.CodingAgent, Model: row.ActualModel}
				groups[key] = group
			}
			if e := group.Summary.Add(meterReportValues(row)); e != nil {
				return e
			}
			if o.details {
				rows = append(rows, row)
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ordered := make([]meterGroup, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, *groups[key])
	}
	if o.json {
		return json.NewEncoder(out).Encode(struct {
			Summary report.MeterSummary `json:"summary"`
			Groups  []meterGroup        `json:"groups"`
			Details []store.MeterRow    `json:"details,omitempty"`
		}{summary, ordered, rows})
	}
	if _, err = fmt.Fprintf(out, "Collection: %s; pricing: %s; daemon helper overhead excluded\nLocal estimated USD: %s; observed priced subtotal USD: %s\nPrice revisions: %v\n", summary.CollectionStatus, summary.PricingStatus, meterAmount(summary.EstimatedUSD), meterAmount(summary.PricedSubtotalUSD), summary.PriceRevisions); err != nil {
		return err
	}
	fields := []struct {
		name         string
		total, known *int64
	}{{"input_uncached", summary.Tokens.InputUncached, summary.KnownTokens.InputUncached}, {"cache_read", summary.Tokens.CacheRead, summary.KnownTokens.CacheRead}, {"cache_write", summary.Tokens.CacheWrite, summary.KnownTokens.CacheWrite}, {"cache_write_5m (subset)", summary.Tokens.CacheWrite5m, summary.KnownTokens.CacheWrite5m}, {"cache_write_1h (subset)", summary.Tokens.CacheWrite1h, summary.KnownTokens.CacheWrite1h}, {"output", summary.Tokens.Output, summary.KnownTokens.Output}, {"reasoning (subset)", summary.Tokens.Reasoning, summary.KnownTokens.Reasoning}}
	for _, field := range fields {
		if _, err = fmt.Fprintf(out, "%s: %s; known subtotal: %s\n", field.name, meterCount(field.total), meterCount(field.known)); err != nil {
			return err
		}
	}
	for _, g := range ordered {
		if _, err = fmt.Fprintf(out, "%s #%d %s %s model %s: input %s output %s; collection %s; pricing %s\n", g.Project, g.Issue, g.Mode, g.CodingAgent, meterAmount(g.Model), meterCount(g.Summary.Tokens.InputUncached), meterCount(g.Summary.Tokens.Output), g.Summary.CollectionStatus, g.Summary.PricingStatus); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if _, err = fmt.Fprintf(out, "%s #%d run %d attempt %s invocation %s parent %s model %s (%s): collection %s / pricing %s; price revision %d; local estimated USD %s; observed priced subtotal USD %s; input_uncached %s cache_read %s cache_write %s cache_write_5m_subset %s cache_write_1h_subset %s output %s reasoning_subset %s; diagnostics %s\n",
			r.Project, r.Issue, r.Run, r.AttemptID, r.InvocationID, r.ParentInvocationID, meterAmount(r.ActualModel), r.ModelProvenance, r.CollectionStatus, r.PricingStatus, r.PriceRevisionID, meterAmount(r.EstimatedUSD), meterAmount(r.PricedSubtotalUSD), meterCount(r.Tokens.InputUncached), meterCount(r.Tokens.CacheRead), meterCount(r.Tokens.CacheWrite), meterCount(r.Tokens.CacheWrite5m), meterCount(r.Tokens.CacheWrite1h), meterCount(r.Tokens.Output), meterCount(r.Tokens.Reasoning), strings.Join(r.Diagnostics, ";")); err != nil {
			return err
		}
	}
	return ctx.Err()
}
func meterReportValues(r store.MeterRow) report.MeterValues {
	return report.MeterValues{Tokens: agent.TokenUsage{InputUncached: r.Tokens.InputUncached, CacheRead: r.Tokens.CacheRead, CacheWrite: r.Tokens.CacheWrite, CacheWrite5m: r.Tokens.CacheWrite5m, CacheWrite1h: r.Tokens.CacheWrite1h, Output: r.Tokens.Output, Reasoning: r.Tokens.Reasoning}, EstimatedUSD: r.EstimatedUSD, PricedSubtotalUSD: r.PricedSubtotalUSD, PriceRevisionID: r.PriceRevisionID, CollectionStatus: r.CollectionStatus, PricingStatus: r.PricingStatus, Diagnostics: r.Diagnostics}
}
func meterAmount(v *string) string {
	if v == nil {
		return "unknown"
	}
	return *v
}
func meterCount(v *int64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatInt(*v, 10)
}

type meterGroup struct {
	Project     string              `json:"project"`
	Issue       int64               `json:"issue"`
	Mode        string              `json:"mode"`
	CodingAgent string              `json:"coding_agent"`
	Model       *string             `json:"actual_model"`
	Summary     report.MeterSummary `json:"summary"`
}
