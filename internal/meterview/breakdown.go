package meterview

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
)

// BreakdownDimensions are the groupings one breakdown read accepts.
var BreakdownDimensions = []string{"mode", "agent", "model", "run"}

// breakdownRowLimit bounds rendered rows so one read stays small enough to
// hand on whole; the remainder is summed and named as left out.
const breakdownRowLimit = 60

// ParseBreakdown reads a comma or space separated subset of
// BreakdownDimensions. "all" selects mode, agent and model. The result keeps
// the canonical dimension order and holds no duplicates.
func ParseBreakdown(raw string) ([]string, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return nil, nil
	}
	selected := map[string]bool{}
	for _, field := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '+' || r == '/' }) {
		switch field {
		case "all":
			selected["mode"], selected["agent"], selected["model"] = true, true, true
		case "provider", "agents", "providers":
			selected["agent"] = true
		case "modes":
			selected["mode"] = true
		case "models":
			selected["model"] = true
		case "runs":
			selected["run"] = true
		default:
			if !slices.Contains(BreakdownDimensions, field) {
				return nil, invalid("breakdown accepts mode, agent, model and run")
			}
			selected[field] = true
		}
	}
	var out []string
	for _, d := range BreakdownDimensions {
		if selected[d] {
			out = append(out, d)
		}
	}
	return out, nil
}

// BreakdownRow is one group of the selected dimensions. Fields outside the
// selection stay empty.
type BreakdownRow struct {
	Mode, Agent string
	Model       *string
	Issue       int64
	Run         int
	Summary     report.MeterSummary
	Records     int
}

// Breakdown is a total with its rows grouped by the selected dimensions, over
// the same scope as the plain total (a task includes its stories).
type Breakdown struct {
	By       []string
	Total    report.MeterSummary
	Records  int
	Rows     []BreakdownRow
	Refusals int
}

// ReadBreakdown validates the request and groups one bounded snapshot.
func ReadBreakdown(ctx context.Context, db Reader, r Request, by []string) (Breakdown, error) {
	if len(by) == 0 {
		return Breakdown{}, invalid("breakdown needs at least one of mode, agent, model or run")
	}
	f, err := Validate(r, true)
	if err != nil {
		return Breakdown{}, err
	}
	f.RepoID, err = ResolveProject(ctx, db, r.Project)
	if err != nil {
		return Breakdown{}, err
	}
	return BuildBreakdown(ctx, db, f, by)
}

// BuildBreakdown reads and closes one bounded snapshot, grouping its rows.
func BuildBreakdown(ctx context.Context, db Reader, f store.MeterFilter, by []string) (b Breakdown, err error) {
	f.IncludeStories = true
	snapshot, err := db.NewMeterSnapshot(ctx, f, store.MeterSnapshotOptions{})
	if err != nil {
		return b, classify(err)
	}
	defer func() { err = errors.Join(err, snapshot.Close(context.WithoutCancel(ctx))) }()
	b = Breakdown{By: by, Total: report.MeterSummary{CollectionStatus: "unknown", PricingStatus: "unknown", HelperOverhead: "excluded"}}
	groups := map[string]*BreakdownRow{}
	refusals := map[string]bool{}
	for {
		page, e := snapshot.Next(ctx, 256)
		if e != nil {
			return b, classify(e)
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
			if e := b.Total.Add(reportValues(row)); e != nil {
				return b, e
			}
			b.Records++
			key := BreakdownRow{}
			var parts []string
			for _, d := range by {
				switch d {
				case "mode":
					key.Mode = row.Mode
					parts = append(parts, "m:"+row.Mode)
				case "agent":
					key.Agent = row.CodingAgent
					parts = append(parts, "a:"+row.CodingAgent)
				case "model":
					key.Model = row.ActualModel
					parts = append(parts, fmt.Sprintf("o:%t:%s", row.ActualModel != nil, Amount(row.ActualModel)))
				case "run":
					key.Issue, key.Run = row.Issue, row.Run
					parts = append(parts, fmt.Sprintf("r:%s#%d/%d", row.Project, row.Issue, row.Run))
				}
			}
			id := strings.Join(parts, "\x00")
			group := groups[id]
			if group == nil {
				key.Summary = report.MeterSummary{CollectionStatus: "unknown", PricingStatus: "unknown", HelperOverhead: "excluded"}
				group = &key
				groups[id] = group
			}
			if e := group.Summary.Add(reportValues(row)); e != nil {
				return b, e
			}
			group.Records++
		}
	}
	for _, g := range groups {
		b.Rows = append(b.Rows, *g)
	}
	slices.SortFunc(b.Rows, func(x, y BreakdownRow) int {
		return cmp.Or(cmp.Compare(x.Issue, y.Issue), cmp.Compare(x.Run, y.Run), cmp.Compare(x.Mode, y.Mode), cmp.Compare(x.Agent, y.Agent), cmp.Compare(Amount(x.Model), Amount(y.Model)))
	})
	b.Refusals = len(refusals)
	return b, ctx.Err()
}

// Label names the row by its selected dimensions in plain words.
func (r BreakdownRow) Label(by []string) string {
	var parts []string
	for _, d := range by {
		switch d {
		case "mode":
			mode := r.Mode
			if mode == "" {
				mode = "not recorded"
			}
			parts = append(parts, "mode "+mode)
		case "agent":
			agent := r.Agent
			if agent == "" {
				agent = "not recorded"
			}
			parts = append(parts, "agent "+agent)
		case "model":
			model := "not recorded"
			if r.Model != nil && *r.Model != "" {
				model = *r.Model
			}
			parts = append(parts, "model "+model)
		case "run":
			parts = append(parts, fmt.Sprintf("run %d of issue %d", r.Run, r.Issue))
		}
	}
	return strings.Join(parts, ", ")
}

// breakdownAmount describes a summary's dollars and whether any are known.
func breakdownAmount(s report.MeterSummary) (string, bool) {
	if s.CollectionStatus == "complete" && s.PricingStatus == "priced" && s.EstimatedUSD != nil {
		return "USD " + *s.EstimatedUSD, true
	}
	// A zero subtotal on an incomplete row is no lower bound worth naming.
	if s.PricedSubtotalUSD != nil && strings.Trim(*s.PricedSubtotalUSD, "0.") != "" {
		return "at least USD " + *s.PricedSubtotalUSD + " (some usage was not reported or not priced)", true
	}
	return "cost unavailable (usage not reported or not priced)", false
}

// TotalAmount describes the total's dollars and whether any are known.
func (b Breakdown) TotalAmount() (string, bool) { return breakdownAmount(b.Total) }

// Lines renders the total, one line per row and the unavailable rows once.
func (b Breakdown) Lines() []string {
	if b.Records == 0 {
		return []string{"No usage is recorded for this filter."}
	}
	total, _ := breakdownAmount(b.Total)
	lines := []string{"Breakdown by " + strings.Join(b.By, ", ") + ".", "Total: " + total + "."}
	var unavailable []string
	rows, leftCount := b.Rows, 0
	if len(rows) > breakdownRowLimit {
		rows, leftCount = rows[:breakdownRowLimit], len(rows)-breakdownRowLimit
	}
	for _, row := range rows {
		amount, ok := breakdownAmount(row.Summary)
		if !ok {
			unavailable = append(unavailable, row.Label(b.By))
			continue
		}
		lines = append(lines, row.Label(b.By)+": "+amount+".")
	}
	if len(unavailable) > 0 {
		lines = append(lines, "Cost unavailable (usage not reported or not priced) for: "+strings.Join(unavailable, "; ")+".")
	}
	if leftCount > 0 {
		lines = append(lines, fmt.Sprintf("%d further rows were left out; narrow the breakdown to see them.", leftCount))
	}
	if b.Refusals > 0 {
		lines = append(lines, fmt.Sprintf("%d attempts were provider-reported zero-cost refusals.", b.Refusals))
	}
	return lines
}
