// Package meterview provides read-only, transport-neutral meter reports.
package meterview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// Reader limits reports to existing catalogue and usage reads.
type Reader interface {
	ListRepos(context.Context) ([]store.Repo, error)
	NewMeterSnapshot(context.Context, store.MeterFilter, store.MeterSnapshotOptions) (*store.MeterSnapshot, error)
	ListMeterPrices(context.Context) (store.MeterCatalog, error)
}

// View holds the same summary, groups and optional rows used by the CLI.
type View struct {
	Summary         report.MeterSummary `json:"summary"`
	Issues          []IssueSummary      `json:"issues,omitempty"`
	Groups          []Group             `json:"groups"`
	Details         []store.MeterRow    `json:"details,omitempty"`
	AsOf            time.Time           `json:"-"`
	RefusalAttempts int                 `json:"-"`
}

// Build reads and closes one bounded snapshot, preserving its stable row order.
func Build(ctx context.Context, db Reader, f store.MeterFilter, details bool) (v View, err error) {
	f.IncludeStories = true
	snapshot, err := db.NewMeterSnapshot(ctx, f, store.MeterSnapshotOptions{})
	if err != nil {
		return v, classify(err)
	}
	defer func() { err = errors.Join(err, snapshot.Close(context.WithoutCancel(ctx))) }()
	v, err = buildFromPages(func() ([]store.MeterRow, error) { return snapshot.Next(ctx, 256) }, details)
	return v, errors.Join(err, ctx.Err())
}

// BuildFromRows aggregates already captured exclusive measurements and coverage
// with the same exact arithmetic as the regular meter report.
func BuildFromRows(source []store.MeterRow, details bool) (v View, err error) {
	return buildFromPages(rowsOnce(source), details)
}

func buildFromPages(next func() ([]store.MeterRow, error), details bool) (v View, err error) {
	summary := report.MeterSummary{CollectionStatus: "unknown", PricingStatus: "unknown", HelperOverhead: "excluded"}
	rows := []store.MeterRow{}
	groups := map[string]*Group{}
	issues := map[string]*IssueSummary{}
	refusals := map[string]bool{}
	for {
		page, err := next()
		if err != nil {
			return v, classify(err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			if row.ProviderReportedZeroCostRefusal {
				refusals[row.AttemptID] = true
			}
			if row.CollectionStatus != "complete" || row.PricingStatus != "priced" {
				row.EstimatedUSD = nil
			}
			if e := summary.Add(reportValues(row)); e != nil {
				return v, e
			}
			issueKey := fmt.Sprintf("%s#%d", row.Project, row.Issue)
			issue := issues[issueKey]
			if issue == nil {
				issue = &IssueSummary{Project: row.Project, Issue: row.Issue}
				issues[issueKey] = issue
			}
			if e := issue.Summary.Add(reportValues(row)); e != nil {
				return v, e
			}
			keyBytes, _ := json.Marshal([]string{row.Project, strconv.FormatInt(row.Issue, 10), row.Mode, row.CodingAgent, strconv.FormatBool(row.ActualModel != nil), Amount(row.ActualModel)})
			key := string(keyBytes)
			group := groups[key]
			if group == nil {
				group = &Group{Project: row.Project, Issue: row.Issue, Mode: row.Mode, CodingAgent: row.CodingAgent, Model: row.ActualModel}
				groups[key] = group
			}
			if e := group.Summary.Add(reportValues(row)); e != nil {
				return v, e
			}
			if details {
				rows = append(rows, row)
			}
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	ordered := make([]Group, 0, len(keys))
	for _, key := range keys {
		ordered = append(ordered, *groups[key])
	}
	issueKeys := make([]string, 0, len(issues))
	for key := range issues {
		issueKeys = append(issueKeys, key)
	}
	slices.Sort(issueKeys)
	for _, key := range issueKeys {
		v.Issues = append(v.Issues, *issues[key])
	}
	v = View{Issues: v.Issues, Summary: summary, Groups: ordered, Details: rows, AsOf: time.Now().UTC(), RefusalAttempts: len(refusals)}
	return v, nil
}

// WriteText preserves the existing CLI text format.
func (v View) WriteText(out io.Writer) (err error) {
	if _, err = fmt.Fprintf(out, "Collection: %s; pricing: %s; daemon helper overhead excluded\n%s\nPrice revisions: %v\n", v.Summary.CollectionStatus, v.Summary.PricingStatus, CostText(v.Summary.EstimatedUSD, v.Summary.PricedSubtotalUSD, v.Summary.CollectionStatus, v.Summary.PricingStatus), v.Summary.PriceRevisions); err != nil {
		return err
	}
	if len(v.Summary.Diagnostics) > 0 {
		if _, err = fmt.Fprintf(out, "Collection diagnostics: %s\n", strings.Join(v.Summary.Diagnostics, ";")); err != nil {
			return err
		}
	}
	if v.RefusalAttempts > 0 {
		if _, err = fmt.Fprintf(out, "Provider-reported zero-cost refusals: %d attempts; token-derived pricing remains unknown\n", v.RefusalAttempts); err != nil {
			return err
		}
	}
	fields := []struct {
		name         string
		total, known *int64
	}{{"input_uncached", v.Summary.Tokens.InputUncached, v.Summary.KnownTokens.InputUncached}, {"cache_read", v.Summary.Tokens.CacheRead, v.Summary.KnownTokens.CacheRead}, {"cache_write", v.Summary.Tokens.CacheWrite, v.Summary.KnownTokens.CacheWrite}, {"cache_write_5m (subset)", v.Summary.Tokens.CacheWrite5m, v.Summary.KnownTokens.CacheWrite5m}, {"cache_write_1h (subset)", v.Summary.Tokens.CacheWrite1h, v.Summary.KnownTokens.CacheWrite1h}, {"output", v.Summary.Tokens.Output, v.Summary.KnownTokens.Output}, {"reasoning (subset)", v.Summary.Tokens.Reasoning, v.Summary.KnownTokens.Reasoning}}
	for _, field := range fields {
		if _, err = fmt.Fprintf(out, "%s: %s; known subtotal: %s\n", field.name, Count(field.total), Count(field.known)); err != nil {
			return err
		}
	}
	for _, issue := range v.Issues {
		if _, err = fmt.Fprintf(out, "%s #%d: %s\n", issue.Project, issue.Issue, CostText(issue.Summary.EstimatedUSD, issue.Summary.PricedSubtotalUSD, issue.Summary.CollectionStatus, issue.Summary.PricingStatus)); err != nil {
			return err
		}
	}
	for _, g := range v.Groups {
		if _, err = fmt.Fprintf(out, "%s #%d %s %s model %s: recorded input %s output %s; collection %s; pricing %s\n", g.Project, g.Issue, g.Mode, g.CodingAgent, Amount(g.Model), Count(g.Summary.KnownTokens.InputUncached), Count(g.Summary.KnownTokens.Output), g.Summary.CollectionStatus, g.Summary.PricingStatus); err != nil {
			return err
		}
	}
	for _, r := range v.Details {
		if _, err = fmt.Fprintf(out, "%s #%d run %d attempt %s invocation %s parent %s model %s (%s): collection %s / pricing %s; price revision %d; %s; input_uncached %s cache_read %s cache_write %s cache_write_5m_subset %s cache_write_1h_subset %s output %s reasoning_subset %s; diagnostics %s\n",
			r.Project, r.Issue, r.Run, r.AttemptID, r.InvocationID, r.ParentInvocationID, Amount(r.ActualModel), r.ModelProvenance, r.CollectionStatus, r.PricingStatus, r.PriceRevisionID, RowCostText(r), Count(r.Tokens.InputUncached), Count(r.Tokens.CacheRead), Count(r.Tokens.CacheWrite), Count(r.Tokens.CacheWrite5m), Count(r.Tokens.CacheWrite1h), Count(r.Tokens.Output), Count(r.Tokens.Reasoning), strings.Join(r.Diagnostics, ";")); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(out, "  execution %s run_id %d graph_instance %s graph_attempt %s node %s outcome %s; provider %s version %s source %s; started %s finished %s\n", r.ExecutionPath, r.RunID, Count(r.GraphInstanceID), Count(r.GraphAttemptID), r.GraphNode, r.Outcome, r.SourceInvocationID, r.AgentVersion, r.SourceFormat, r.AttemptStartedAt, r.AttemptFinishedAt); err != nil {
			return err
		}
	}
	return nil
}

// Lines renders whole lines for transport pagination.
func (v View) Lines() []string {
	var b bytes.Buffer
	_ = v.WriteText(&b)
	return strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
}

// SummaryLines includes estimate labels and all coverage information.
func (v View) SummaryLines() []string {
	v.Issues = nil
	v.Groups = nil
	v.Details = nil
	return v.Lines()
}

// GroupLines renders the ordered project, task, mode, agent and model groups.
func (v View) GroupLines() []string {
	all := v
	all.Details = nil
	lines := all.Lines()
	return lines[len(v.SummaryLines()):]
}

// DetailLines renders invocation and execution rows.
func (v View) DetailLines() []string { return v.Lines()[len(v.SummaryLines())+len(v.GroupLines()):] }
func reportValues(r store.MeterRow) report.MeterValues {
	return report.MeterValues{Tokens: agent.TokenUsage{InputUncached: r.Tokens.InputUncached, CacheRead: r.Tokens.CacheRead, CacheWrite: r.Tokens.CacheWrite, CacheWrite5m: r.Tokens.CacheWrite5m, CacheWrite1h: r.Tokens.CacheWrite1h, Output: r.Tokens.Output, Reasoning: r.Tokens.Reasoning}, EstimatedUSD: r.EstimatedUSD, PricedSubtotalUSD: r.PricedSubtotalUSD, PriceRevisionID: r.PriceRevisionID, CollectionStatus: r.CollectionStatus, PricingStatus: r.PricingStatus, Diagnostics: r.Diagnostics}
}

// Amount keeps missing values distinct from zero.
func Amount(v *string) string {
	if v == nil {
		return "unknown"
	}
	return *v
}

// Count renders nullable token counts.
func Count(v *int64) string {
	if v == nil {
		return "unknown"
	}
	return strconv.FormatInt(*v, 10)
}

// IssueSummary combines all selected modes, agents and models for one issue.
type IssueSummary struct {
	Project string              `json:"project"`
	Issue   int64               `json:"issue"`
	Summary report.MeterSummary `json:"summary"`
}

// Group describes one stable aggregation key.
type Group struct {
	Project     string              `json:"project"`
	Issue       int64               `json:"issue"`
	Mode        string              `json:"mode"`
	CodingAgent string              `json:"coding_agent"`
	Model       *string             `json:"actual_model"`
	Summary     report.MeterSummary `json:"summary"`
}

// CostText describes stored values without converting unknown totals to zero.
func CostText(estimate, subtotal *string, collection, pricing string) string {
	if collection == "complete" && pricing == "priced" && estimate != nil {
		return "Complete local estimate USD: " + *estimate + " at recorded rates"
	}
	if subtotal != nil {
		return "Observed lower bound USD: " + *subtotal + " at recorded rates; total unknown"
	}
	return "Local estimated USD: unknown; observed priced subtotal USD: unknown"
}

// RowCostText also discloses provider-reported zero-cost refusals.
func RowCostText(row store.MeterRow) string {
	text := CostText(row.EstimatedUSD, row.PricedSubtotalUSD, row.CollectionStatus, row.PricingStatus)
	if row.ProviderReportedZeroCostRefusal {
		return "Provider-reported zero-cost refusal (USD 0); token-derived pricing remains unknown; " + text
	}
	return text
}

// rowsOnce adapts an immutable materialization to the streaming aggregators.
func rowsOnce(rows []store.MeterRow) func() ([]store.MeterRow, error) {
	return func() ([]store.MeterRow, error) { page := rows; rows = nil; return page, nil }
}
