package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/messaging"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// ConversationAnalyses runs log analyses the owner confirmed in the
// conversation. Each runs in the background, one per project at a time, and
// its outcome is posted to the channel it was started from. Nothing here starts
// an analysis by itself, and an analysis cut short by a restart is not resumed.
type ConversationAnalyses struct {
	DB *store.DB
	// Start runs one analysis to its end through the shared log analysis service.
	Start func(ctx context.Context, q loganalysis.Request, dryRun bool) (store.LogAnalysis, error)
	// Report renders one saved analysis for the owner.
	Report func(ctx context.Context, id string) (string, error)
	Log    *slog.Logger

	mu      sync.Mutex
	ctx     context.Context
	wg      *sync.WaitGroup
	post    func(ctx context.Context, channel, key, text string) error
	running map[int64]bool
}

// serve binds analyses to the chat lifetime: shutdown cancels a running
// analysis, which the service then records as incomplete.
func (c *ConversationAnalyses) serve(ctx context.Context, wg *sync.WaitGroup, post func(context.Context, string, string, string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx, c.wg, c.post = ctx, wg, post
}

// begin starts one confirmed analysis and returns at once.
func (c *ConversationAnalyses) begin(r messaging.StandardsRequest) (messaging.StandardsResult, error) {
	args := r.Arguments
	repo, err := strconv.ParseInt(args["repository_id"], 10, 64)
	if err != nil || repo < 1 {
		return messaging.StandardsResult{}, errors.New("standards: a registered project is required")
	}
	q := loganalysis.Request{RepositoryIDs: []int64{repo}, StartKey: r.OperationKey}
	for name, bound := range map[string]*time.Time{"after": &q.After, "before": &q.Before} {
		t, e := time.Parse(time.RFC3339, args[name])
		if e != nil {
			return messaging.StandardsResult{}, fmt.Errorf("standards: exact %s time required", name)
		}
		*bound = t.UTC()
	}
	dryRun := args["dry_run"] == "true"
	if args["dry_run"] != "" && !dryRun && args["dry_run"] != "false" {
		return messaging.StandardsResult{}, errors.New("standards: dry_run must be true or false")
	}
	if _, err = loganalysis.NormalizeRequest(q); err != nil {
		return messaging.StandardsResult{}, fmt.Errorf("standards: %w", err)
	}
	project := args["project"]
	if project == "" {
		project = "project " + args["repository_id"]
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil || c.ctx.Err() != nil || c.post == nil || c.Start == nil {
		return messaging.StandardsResult{}, errors.New("standards: log analysis is unavailable")
	}
	if c.running[repo] {
		return messaging.StandardsResult{Text: "A log analysis of " + project + " is already running. Its result will be posted here; start another one after it finishes."}, nil
	}
	if c.running == nil {
		c.running = map[int64]bool{}
	}
	c.running[repo] = true
	c.wg.Add(1)
	go c.run(c.ctx, c.post, r.ChannelID, r.OperationKey, project, q, dryRun)
	text := fmt.Sprintf("Started a log analysis of %s for %s to %s. It takes a few minutes; the result will be posted here.", project, q.After.Format(time.RFC3339), q.Before.Format(time.RFC3339))
	if dryRun {
		text += " Dry run: nothing will be filed."
	}
	return messaging.StandardsResult{Text: text, References: map[string]string{"kind": "analysis", "project": project}}, nil
}

func (c *ConversationAnalyses) run(ctx context.Context, post func(context.Context, string, string, string) error, channel, key, project string, q loganalysis.Request, dryRun bool) {
	defer c.wg.Done()
	defer func() {
		c.mu.Lock()
		delete(c.running, q.RepositoryIDs[0])
		c.mu.Unlock()
	}()
	a, err := c.Start(ctx, q, dryRun)
	text := ""
	if a.ID != "" {
		key = "log-analysis:" + a.ID
		text, err = c.Report(context.WithoutCancel(ctx), a.ID)
		if err != nil {
			text = fmt.Sprintf("Log analysis %s of %s ended (%s), but its report could not be read: %v", a.ID, project, a.State, err)
		}
	} else {
		text = fmt.Sprintf("The log analysis of %s could not start: %v", project, err)
	}
	// The message is saved even during shutdown and delivered after restart.
	saved, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e := post(saved, channel, key, text); e != nil && c.Log != nil {
		c.Log.WarnContext(saved, "log analysis result could not be queued for the conversation", slog.String("analysis", a.ID), slog.Any("error", e))
	}
}

// list shows recent analyses with their state.
func (c *ConversationAnalyses) list(ctx context.Context) (string, error) {
	rows, err := c.DB.ListLogAnalyses(ctx, "", 20)
	if err != nil {
		return "", err
	}
	if len(rows) == 0 {
		return "No log analyses yet.", nil
	}
	var b strings.Builder
	for _, a := range rows {
		var scope loganalysis.Scope
		_ = json.Unmarshal(a.Scope, &scope)
		fmt.Fprintf(&b, "%s  %s  %s  started %s, repositories %v", a.ID, a.State, a.Disposition, a.CreatedAt.UTC().Format(time.RFC3339), scope.Request.RepositoryIDs)
		if !scope.Request.After.IsZero() {
			fmt.Fprintf(&b, ", window %s to %s", scope.Request.After.Format(time.RFC3339), scope.Request.Before.Format(time.RFC3339))
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// reportLength keeps a posted report to a few Discord messages; the whole
// report stays available through the CLI.
const reportLength = 12000

// renderConversationAnalysis is the owner's view of one saved analysis: the
// summary, the proposed standards as agents would read them and the findings
// grouped by who can fix them, each with what became of it.
func renderConversationAnalysis(view loganalysis.Inspection, categories []standards.Category) string {
	var b strings.Builder
	a := view.Invocation
	fmt.Fprintf(&b, "Log analysis %s: %s", a.ID, a.State)
	if a.Disposition != "" {
		fmt.Fprintf(&b, " (%s)", a.Disposition)
	}
	b.WriteString("\n")
	var report loganalysis.Report
	if len(view.Report) == 0 || json.Unmarshal(view.Report, &report) != nil {
		fmt.Fprintf(&b, "No report. %s\n", a.Reason)
		return b.String()
	}
	fmt.Fprintf(&b, "\nSummary\n%s\n", strings.TrimSpace(report.Summary))
	links := map[string]string{}
	for _, l := range view.Proposals {
		links[l.CandidateKey] = fmt.Sprintf(" (unapproved proposal %s version %d)", l.ProposalID, l.Version)
	}
	for i, p := range report.Proposals {
		fmt.Fprintf(&b, "\nProposed standard %d: %s%s\n", i+1, p.Operation.Kind, links[fmt.Sprintf("proposal-%d", i+1)])
		known := categories
		if p.NewCategory != nil {
			fmt.Fprintf(&b, "New category %s: %s. %s\n", p.NewCategory.ID, p.NewCategory.Name, p.NewCategory.Description)
			known = append([]standards.Category{*p.NewCategory}, categories...)
		}
		if p.Operation.Standard != nil {
			b.WriteString(standards.Render(known, []standards.Standard{*p.Operation.Standard}))
		}
		fmt.Fprintf(&b, "Why it prevents: %s\n", p.Justification.WhyItPrevents)
	}
	var scope loganalysis.Scope
	_ = json.Unmarshal(a.Scope, &scope)
	filings := map[string]store.LogAnalysisFiling{}
	for _, f := range view.Filings {
		filings[f.FindingKey] = f
	}
	for _, group := range []struct{ route, title string }{{"project", "Filed Bugs"}, {"upstream", "Upstream reports"}, {"team", "Suggested team changes"}} {
		heading := false
		for _, f := range report.Findings {
			if loganalysis.Route(scope, f) != group.route {
				continue
			}
			if !heading {
				heading = true
				fmt.Fprintf(&b, "\n%s", group.title)
				if scope.Request.DryRun && group.route != "team" {
					b.WriteString(" (dry run: nothing was filed)")
				}
				b.WriteString("\n")
			}
			filing, recorded := filings[f.Key]
			fmt.Fprintf(&b, "- %s [%s, %s]: %s\n", f.Title, f.Kind, f.Target, loganalysis.FindingStatus(a.ID, scope, f, filing, recorded))
		}
	}
	if len(report.Proposals) == 0 && report.NoChangeReason != "" {
		fmt.Fprintf(&b, "\nNo change: %s\n", report.NoChangeReason)
	}
	text := b.String()
	if len(text) > reportLength {
		text = strings.ToValidUTF8(text[:reportLength], "") + "\n[shortened]\n"
	}
	return text + "\nFull report: litefactory standards analyze logs show " + a.ID + "\n"
}
