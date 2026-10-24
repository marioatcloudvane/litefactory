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
	post    func(ctx context.Context, channel, key, operation, text string) error
	running map[int64]bool
	// authors holds the author sessions drafting in the background, with
	// the owner's answers that arrived meanwhile. One draft per session.
	authors map[string][]messaging.StandardsRequest
}

// available reports whether background work can run now.
func (c *ConversationAnalyses) available() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx != nil && c.ctx.Err() == nil && c.post != nil
}

// claimAuthor marks session id as drafting; false when it already is.
func (c *ConversationAnalyses) claimAuthor(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, busy := c.authors[id]; busy {
		return false
	}
	if c.authors == nil {
		c.authors = map[string][]messaging.StandardsRequest{}
	}
	c.authors[id] = nil
	return true
}

// authorRunning reports whether session id is drafting in the background.
func (c *ConversationAnalyses) authorRunning(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, busy := c.authors[id]
	return busy
}

// queueAnswer keeps an answer for the session's next draft while one is
// being made; false when the session is not drafting.
func (c *ConversationAnalyses) queueAnswer(r messaging.StandardsRequest) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	queued, busy := c.authors[r.Arguments["id"]]
	if busy {
		c.authors[r.Arguments["id"]] = append(queued, r)
	}
	return busy
}

// releaseAuthor ends session id's draft and returns the answers that came in.
func (c *ConversationAnalyses) releaseAuthor(id string) []messaging.StandardsRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	queued := c.authors[id]
	delete(c.authors, id)
	return queued
}

// serve binds analyses to the chat lifetime: shutdown cancels a running
// analysis, which the service then records as incomplete.
func (c *ConversationAnalyses) serve(ctx context.Context, wg *sync.WaitGroup, post func(ctx context.Context, channel, key, operation, text string) error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ctx, c.wg, c.post = ctx, wg, post
}

// later runs work in the background for the chat lifetime and posts the text
// it returns, if any, to channel, after the confirmation of operation. It reports false
// when nothing can run in the background, so the caller works in the
// foreground instead.
func (c *ConversationAnalyses) later(ctx context.Context, channel, key, operation string, work func(context.Context) string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil || c.ctx.Err() != nil || c.post == nil {
		return false
	}
	run, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(c.ctx, cancel)
	post := c.post
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer stop()
		defer cancel()
		text := work(run)
		if text == "" {
			return
		}
		// The message is saved even during shutdown and delivered after restart.
		saved, done := context.WithTimeout(context.WithoutCancel(run), 10*time.Second)
		defer done()
		if e := post(saved, channel, key, operation, text); e != nil && c.Log != nil {
			c.Log.WarnContext(saved, "background result could not be queued for the conversation", slog.String("key", key), slog.Any("error", e))
		}
	}()
	return true
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
	project = project[strings.LastIndexByte(project, '/')+1:]
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ctx == nil || c.ctx.Err() != nil || c.post == nil || c.Start == nil {
		return messaging.StandardsResult{}, errors.New("standards: log analysis is unavailable")
	}
	if c.running[repo] {
		return messaging.StandardsResult{Text: "An analysis of " + project + " is already running; I'll post its result here. Start another one once it's done."}, nil
	}
	if c.running == nil {
		c.running = map[int64]bool{}
	}
	c.running[repo] = true
	c.wg.Add(1)
	go c.run(c.ctx, c.post, r.ChannelID, r.OperationKey, project, q, dryRun)
	text := fmt.Sprintf("Started the analysis of %s for %s. It takes about 5 minutes; I'll post what I find here.", project, messaging.PlainWindow(args["after"], args["before"], r.At))
	if dryRun {
		text += " It's a dry run, so nothing gets filed."
	}
	return messaging.StandardsResult{Text: text, References: map[string]string{"kind": "analysis", "project": project}}, nil
}

func (c *ConversationAnalyses) run(ctx context.Context, post func(ctx context.Context, channel, key, operation, text string) error, channel, key, project string, q loganalysis.Request, dryRun bool) {
	defer c.wg.Done()
	defer func() {
		c.mu.Lock()
		delete(c.running, q.RepositoryIDs[0])
		c.mu.Unlock()
	}()
	operation := key
	a, err := c.Start(ctx, q, dryRun)
	var text string
	if a.ID != "" {
		key = "log-analysis:" + a.ID
		text, err = c.Report(context.WithoutCancel(ctx), a.ID)
		if err != nil {
			text = fmt.Sprintf("The analysis of %s has ended, but I couldn't read its report. Ask me for it in a moment.", project)
		}
	} else {
		text = fmt.Sprintf("I couldn't start the analysis of %s: %s. Nothing was filed. Say \"again\" and I'll set it up once more.", project, plainFailure(err.Error()))
	}
	// The message is saved even during shutdown and delivered after restart.
	saved, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if e := post(saved, channel, key, operation, text); e != nil && c.Log != nil {
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

// reportLength keeps a posted result to a few Discord messages; the full
// report is a lookup away in the conversation.
const reportLength = 6000

// renderConversationAnalysis tells the owner what one saved analysis found,
// in plain words: what was looked at, the proposed standards, what was filed
// or is already known, and what to do next. Analysis IDs, states and CLI
// commands stay in the full report.
func renderConversationAnalysis(view loganalysis.Inspection) string {
	a := view.Invocation
	var scope loganalysis.Scope
	_ = json.Unmarshal(a.Scope, &scope)
	subject := "the runs"
	if names := scopeNames(scope); names != "" {
		subject = names + "'s runs"
	}
	if !scope.Request.After.IsZero() {
		subject += " for " + messaging.PlainWindow(scope.Request.After.Format(time.RFC3339), scope.Request.Before.Format(time.RFC3339), a.CreatedAt)
	}
	var report loganalysis.Report
	if len(view.Report) == 0 || json.Unmarshal(view.Report, &report) != nil {
		return fmt.Sprintf("The analysis of %s didn't finish: %s. Nothing was filed. Say \"again\" and I'll set it up once more.", subject, plainFailure(a.Reason))
	}
	var b strings.Builder
	// The analyst's opening paragraph says what it looked at and what
	// matters; the rest of its summary is in the full report.
	summary, _, _ := strings.Cut(strings.TrimSpace(report.Summary), "\n\n")
	fmt.Fprintf(&b, "I went through %s. %s\n", subject, strings.TrimSpace(summary))
	if n := len(report.Proposals); n > 0 {
		noun := "a new standard"
		if n > 1 {
			noun = fmt.Sprintf("%d new standards", n)
		}
		fmt.Fprintf(&b, "\nI'm proposing %s; nothing changes until you approve:\n", noun)
		for _, p := range report.Proposals {
			if s := p.Operation.Standard; s != nil {
				fmt.Fprintf(&b, "- %s", strings.TrimSpace(s.Rule))
				if why := strings.TrimSpace(p.Justification.WhyItPrevents); why != "" {
					fmt.Fprintf(&b, " (%s)", strings.TrimSuffix(why, "."))
				}
				b.WriteString("\n")
			}
		}
	} else if reason := strings.TrimSpace(report.NoChangeReason); reason != "" && summary == "" {
		fmt.Fprintf(&b, "\nNo new standard this time: %s\n", reason)
	}
	filings := map[string]store.LogAnalysisFiling{}
	for _, f := range view.Filings {
		filings[f.FindingKey] = f
	}
	var filed, known, open, team []string
	for _, f := range report.Findings {
		filing, recorded := filings[f.Key]
		switch {
		case recorded && filing.State == store.FilingFiled:
			filed = append(filed, fmt.Sprintf("%s %s %s", issueNumber(filing.Reference), f.Title, filing.URL))
		case recorded && filing.State == store.FilingWouldFile:
			open = append(open, f.Title+" (dry run, not filed)")
		case recorded:
			open = append(open, f.Title+" (not filed)")
		case f.Existing != "":
			known = append(known, f.Title+" ("+issueNumber(f.Existing)+")")
		case loganalysis.Route(scope, f) == "team":
			team = append(team, f.Title)
		case f.File:
			open = append(open, f.Title+" (not filed yet)")
		default:
			open = append(open, f.Title)
		}
	}
	for _, list := range []struct {
		title string
		items []string
	}{{"I filed", filed}, {"Already known, so not filed again", known}, {"Suggested changes to your own team's roles", team}, {"Also noticed", open}} {
		if len(list.items) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n%s:\n", list.title)
		for _, item := range list.items {
			b.WriteString("- " + item + "\n")
		}
	}
	text := strings.TrimRight(b.String(), "\n")
	if len(text) > reportLength {
		text = strings.ToValidUTF8(text[:reportLength], "") + "…"
	}
	next := "Nothing waits for you here."
	if len(report.Proposals) > 0 {
		next = "Tell me which proposals to approve, or ask to see them first."
	}
	return text + "\n\n" + next + " Ask me for the full report if you want the details."
}

// scopeNames names the analysed projects.
func scopeNames(scope loganalysis.Scope) string {
	var names []string
	for _, r := range scope.Repositories {
		names = append(names, r.Name[strings.LastIndexByte(r.Name, '/')+1:])
	}
	return strings.Join(names, " and ")
}

// issueNumber shortens "owner/repo#12" to "#12".
func issueNumber(reference string) string {
	if i := strings.LastIndexByte(reference, '#'); i >= 0 {
		return reference[i:]
	}
	return reference
}

// plainFailure puts a recorded failure cause into plain words.
func plainFailure(reason string) string {
	lower := strings.ToLower(reason)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("login", "log in", "logged in", "auth", "401", "credential", "api key"):
		return "the AI provider didn't accept the analyst's login"
	case has("rate limit", "rate-limit", "429", "quota", "usage limit"):
		return "the AI provider's rate limit was hit"
	case has("interrupted", "wall-time", "deadline", "timeout", "timed out", "canceled", "cancelled"):
		return "it ran out of time or was interrupted by a restart"
	case has("terminal result", "without an accepted report"):
		return "the analyst stopped without handing back a report"
	case has("mcp is disabled"):
		return "the analyst's tools are switched off in the factory settings"
	case has("unavailable"):
		return "something it needs was unavailable"
	}
	return "it stopped before finishing"
}
