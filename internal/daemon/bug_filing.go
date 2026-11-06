package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstormtasks"
	"github.com/cloudvane/litefactory/internal/factory/featuredraft"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// bugLabel is the one existing repository label a filed bug carries.
const bugLabel = "bug"

// bugLabelAttempts bounds label writes and missing-label observations before
// the filing fails with its link and the remedy.
const bugLabelAttempts = 5

// bugLabelRetry is the wait between two label attempts.
var bugLabelRetry = 30 * time.Second

const (
	bugReasonRevokedNothing   = "authority was revoked; nothing was filed"
	bugReasonRestart          = "restart found the create begun; checking whether the issue was created"
	bugReasonUnclear          = "no clear answer from the create; checking whether the issue was created"
	bugReasonMismatch         = "the reply did not match the bug; checking whether the issue was created"
	bugReasonRateLimited      = "rate limited; retrying after the provider's wait"
	bugReasonRateLimitedDone  = "rate limited; retries used; nothing was filed"
	bugReasonUnconfirmed      = "could not confirm the issue was created; nothing was filed again"
	bugReasonLabelMissing     = "filed but not yet labelled: the bug label is missing; create the bug label first"
	bugReasonLabelUnclear     = "filed but not yet labelled; retrying the label"
	bugReasonLabelRateLimited = "filed but not yet labelled; rate limited, retrying after the provider's wait"
	bugReasonLabelling        = "filed; adding the bug label"
)

// bugIssue is the forge observation the bug filing processor compares.
type bugIssue struct {
	ID        int64
	NodeID    string
	Number    int
	Title     string
	Body      string
	Link      string
	AuthorID  int64
	Labels    []string
	CreatedAt time.Time
}

// bugFilingPort is the narrow forge port of the bug filing processor: create
// with title and body only, the bot's own issues since a time, one issue read,
// one exact label read and one ID-only label write. It can never create a label.
type bugFilingPort interface {
	CreateIssue(ctx context.Context, repositoryRef, title, body string) (bugIssue, error)
	ListBotIssues(ctx context.Context, repositoryRef string, since time.Time) ([]bugIssue, error)
	ReadIssue(ctx context.Context, repositoryRef string, number int) (bugIssue, error)
	LabelNodeID(ctx context.Context, repositoryRef, name string) (string, bool, error)
	AddLabelsByID(ctx context.Context, labelableNodeID string, labelIDs []string) error
}

// bugFilingClient is what the real GitHub client offers the processor.
type bugFilingClient interface {
	brainstormTaskForge
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	RepoLabelNodeID(context.Context, github.Repo, string) (string, bool, error)
	AddLabelsByID(context.Context, string, []string) error
}

// bugFilingProcessor drives active filings from admitted to labelled.
type bugFilingProcessor struct {
	db        *store.DB
	forge     bugFilingPort
	authority func(context.Context, store.BugFiling) error
	// linear builds the Linear target for a connection version; redactor
	// protects Linear filing text before it is sent.
	linear   func(context.Context, store.ConnectionVersion) (bugLinearTarget, error)
	redactor *redact.Redactor
	bot      int64
	now      func() time.Time
	log      *slog.Logger
}

// stepBugFilings advances every active filing once, with at most one create
// per step, under the factory bot credential.
func (d *Daemon) stepBugFilings(ctx context.Context) {
	p := d.bugFilingProcessor()
	if p == nil {
		return
	}
	if err := p.step(ctx); err != nil && ctx.Err() == nil {
		d.log.WarnContext(ctx, "bug filing requires inspection")
	}
}

// bugFilingForge is the GitHub client bug filing uses: the configured bot
// client, or the tick's forge when it offers the same calls.
func (d *Daemon) bugFilingForge() any {
	if d.bugForge != nil {
		return d.bugForge
	}
	return d.forge
}

func (d *Daemon) bugFilingProcessor() *bugFilingProcessor {
	client, ok := d.bugFilingForge().(bugFilingClient)
	if !ok || d.db == nil || d.filter == nil {
		return nil
	}
	bot := d.filter.Identities().BotUserID
	if bot < 1 {
		return nil
	}
	var sanitizer *redact.Redactor
	if d.ownerActions != nil {
		sanitizer = d.ownerActions.sanitizer
	}
	if sanitizer == nil {
		sanitizer = redact.New(redact.Options{Logger: d.log})
	}
	return &bugFilingProcessor{
		db:        d.db,
		forge:     &bugFilingGitHub{base: &brainstormTaskGitHub{db: d.db, forge: client, bot: bot, redactor: sanitizer}, client: client},
		authority: d.bugFilingAuthority,
		linear:    d.bugFilingLinear,
		redactor:  sanitizer,
		bot:       bot,
		now:       d.brainstormTasks.wallNow,
		log:       d.log,
	}
}

func (p *bugFilingProcessor) clock() time.Time { return p.now().UTC() }

func (p *bugFilingProcessor) step(ctx context.Context) error {
	active, err := p.db.ActiveBugFilings(ctx, 500)
	if err != nil {
		return err
	}
	// Create spacing comes from the durable create attempts, so it holds
	// across restarts without in-memory state.
	var lastCreate time.Time
	for _, f := range active {
		if f.AttemptAt.After(lastCreate) {
			lastCreate = f.AttemptAt
		}
	}
	created := false
	var errs []error
	for i := range active {
		f := active[i]
		var e error
		switch f.State {
		case store.BugFilingCreating:
			// One loop drives filings, so a begun create seen here was cut off.
			e = p.toUncertain(ctx, f, bugReasonRestart)
		case store.BugFilingUncertain:
			e = p.check(ctx, f)
		case store.BugFilingAdmitted:
			if created || !lastCreate.IsZero() && p.clock().Before(lastCreate.Add(brainstormtasks.CreateSpacing)) {
				continue
			}
			if !f.NextCheckAt.IsZero() && p.clock().Before(f.NextCheckAt) {
				continue
			}
			var did bool
			did, e = p.create(ctx, f)
			created = created || did
		case store.BugFilingCreated:
			e = p.label(ctx, f)
		}
		if e != nil {
			errs = append(errs, fmt.Errorf("bug filing %d: %w", f.ID, e))
		}
	}
	return errors.Join(errs...)
}

func (p *bugFilingProcessor) save(ctx context.Context, next store.BugFiling, from string) (bool, error) {
	return p.db.SaveBugFiling(context.WithoutCancel(ctx), next, from, p.clock())
}

func (p *bugFilingProcessor) matches(issue bugIssue, f store.BugFiling) bool {
	return p.bot > 0 && issue.AuthorID == p.bot && issue.Title == f.Title && issue.Body == f.Body && issue.Number > 0 && issue.ID > 0
}

func (p *bugFilingProcessor) create(ctx context.Context, f store.BugFiling) (bool, error) {
	if f.Tracker == store.BugTrackerLinear {
		return p.createLinear(ctx, f)
	}
	at := p.clock()
	begun := f
	begun.State = store.BugFilingCreating
	begun.AttemptAt = at
	begun.ChecksDone = 0
	begun.NextCheckAt = time.Time{}
	begun.Reason = ""
	saved, err := p.save(ctx, begun, store.BugFilingAdmitted)
	if err != nil || !saved {
		return false, err
	}
	// The authority check follows the durable begin: a refusal here proves
	// no write was sent.
	if err = p.authority(ctx, begun); err != nil {
		back := begun
		if errors.Is(err, errBugFilingRevoked) {
			back.State = store.BugFilingNotCreated
			back.Reason = bugReasonRevokedNothing
			_, e := p.save(ctx, back, store.BugFilingCreating)
			return true, e
		}
		back.State = store.BugFilingAdmitted
		back.AttemptAt = f.AttemptAt
		back.Reason = f.Reason
		_, e := p.save(ctx, back, store.BugFilingCreating)
		return true, errors.Join(err, e)
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	issue, createErr := p.forge.CreateIssue(call, f.RepositoryRef, f.Title, f.Body)
	cancel()
	next := begun
	switch {
	case createErr == nil && p.matches(issue, f):
		next.State = store.BugFilingCreated
		next.IssueID, next.IssueNodeID, next.IssueNumber, next.IssueLink = issue.ID, issue.NodeID, issue.Number, issue.Link
	case createErr == nil:
		next.State = store.BugFilingUncertain
		next.Reason = bugReasonMismatch
		next.NextCheckAt = at.Add(brainstormtasks.CheckSchedule[0])
	case errors.Is(createErr, github.ErrRateLimited):
		if f.CreateRetries < brainstormtasks.MaxRateLimitRetries {
			next.State = store.BugFilingAdmitted
			next.CreateRetries++
			next.NextCheckAt = p.clock().Add(retryAfter(createErr))
			next.Reason = bugReasonRateLimited
		} else {
			next.State = store.BugFilingNotCreated
			next.Reason = bugReasonRateLimitedDone
		}
	case featuredraft.KnownCreateFailure(createErr):
		next.State = store.BugFilingNotCreated
		next.Reason = "refused before any write (" + bugRefusal(createErr) + "); nothing was filed"
	default:
		next.State = store.BugFilingUncertain
		next.Reason = bugReasonUnclear
		next.NextCheckAt = at.Add(brainstormtasks.CheckSchedule[0])
	}
	_, err = p.save(ctx, next, store.BugFilingCreating)
	return true, err
}

func retryAfter(err error) time.Duration {
	var limited *github.RateLimitError
	if errors.As(err, &limited) && limited.RetryAfter > 0 {
		return limited.RetryAfter
	}
	return github.DefaultRetryAfter
}

func bugRefusal(err error) string {
	for _, c := range []struct {
		err  error
		text string
	}{{github.ErrInvalidArgument, "invalid argument"}, {github.ErrUnauthorized, "unauthorized"}, {github.ErrForbidden, "forbidden"}, {github.ErrNotFound, "not found"}, {github.ErrUnprocessable, "unprocessable"}} {
		if errors.Is(err, c.err) {
			return c.text
		}
	}
	return "refused"
}

func (p *bugFilingProcessor) toUncertain(ctx context.Context, f store.BugFiling, reason string) error {
	next := f
	next.State = store.BugFilingUncertain
	if next.AttemptAt.IsZero() {
		next.AttemptAt = p.clock()
	}
	next.ChecksDone = 0
	next.NextCheckAt = next.AttemptAt.Add(brainstormtasks.CheckSchedule[0])
	next.Reason = reason
	_, err := p.save(ctx, next, store.BugFilingCreating)
	return err
}

// check runs one due delayed-visibility check. It only reads, so it runs
// after a revocation too, and it never creates.
func (p *bugFilingProcessor) check(ctx context.Context, f store.BugFiling) error {
	schedule := brainstormtasks.CheckSchedule
	due := f.NextCheckAt
	if due.IsZero() {
		due = f.AttemptAt.Add(schedule[min(f.ChecksDone, len(schedule)-1)])
	}
	if p.clock().Before(due) {
		return nil
	}
	next := f
	next.ChecksDone++
	if f.Tracker == store.BugTrackerLinear {
		p.checkLinear(ctx, f, &next)
		return p.finishCheck(ctx, f, next, schedule)
	}
	since := f.CreatedAt.Add(-brainstormtasks.ListingLookback)
	call, cancel := context.WithTimeout(ctx, time.Minute)
	issues, err := p.forge.ListBotIssues(call, f.RepositoryRef, since)
	cancel()
	if err != nil {
		next.Reason = "the check could not list issues; nothing was filed again"
	} else {
		var found []bugIssue
		for _, issue := range issues {
			if (issue.CreatedAt.IsZero() || !issue.CreatedAt.Before(since)) && bugMarkerLine(issue.Body, f.Marker) {
				found = append(found, issue)
			}
		}
		switch {
		case len(found) == 1 && p.matches(found[0], f):
			next.State = store.BugFilingCreated
			next.IssueID, next.IssueNodeID, next.IssueNumber, next.IssueLink = found[0].ID, found[0].NodeID, found[0].Number, found[0].Link
			next.Reason = ""
			next.NextCheckAt = time.Time{}
		case len(found) == 1:
			next.Reason = fmt.Sprintf("an issue carries this bug's marker but differs: #%d", found[0].Number)
		case len(found) > 1:
			numbers := make([]string, len(found))
			for i, m := range found {
				numbers[i] = fmt.Sprintf("#%d", m.Number)
			}
			next.Reason = "several issues carry this bug's marker: " + strings.Join(numbers, ", ")
		default:
			next.Reason = "checking whether the issue was created; not found yet"
		}
	}
	return p.finishCheck(ctx, f, next, schedule)
}

// finishCheck schedules the next check or gives up, then saves.
func (p *bugFilingProcessor) finishCheck(ctx context.Context, f, next store.BugFiling, schedule []time.Duration) error {
	if next.State == store.BugFilingUncertain {
		if next.ChecksDone >= len(schedule) {
			next.State = store.BugFilingFailedUnconfirmed
			next.Reason = bugReasonUnconfirmed
			next.NextCheckAt = time.Time{}
		} else {
			next.NextCheckAt = f.AttemptAt.Add(schedule[next.ChecksDone])
		}
	}
	_, err := p.save(ctx, next, store.BugFilingUncertain)
	return err
}

// bugMarkerLine reports whether one whole line of body is the filing's
// marker line. The marker may be stored as the line or as its hex value.
func bugMarkerLine(body, marker string) bool {
	if marker == "" {
		return false
	}
	if !strings.HasPrefix(marker, "<!--") {
		marker = "<!-- lf:bug-filing v=1 marker=" + marker + " -->"
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimRight(line, "\r") == marker {
			return true
		}
	}
	return false
}

func bugLabelled(issue bugIssue) bool { return slices.Contains(issue.Labels, bugLabel) }

// readFiled re-reads the filed issue, bound to its immutable id.
func (p *bugFilingProcessor) readFiled(ctx context.Context, f store.BugFiling) (bugIssue, error) {
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	issue, err := p.forge.ReadIssue(call, f.RepositoryRef, f.IssueNumber)
	if err != nil {
		return bugIssue{}, err
	}
	if issue.ID != f.IssueID {
		return bugIssue{}, errors.New("bug filing: the re-read issue is not the filed one")
	}
	return issue, nil
}

func (p *bugFilingProcessor) labelFailed(ctx context.Context, f store.BugFiling, reason string) error {
	next := f
	next.State = store.BugFilingLabelFailed
	next.NextCheckAt = time.Time{}
	next.Reason = reason
	_, err := p.save(ctx, next, store.BugFilingCreated)
	return err
}

func (p *bugFilingProcessor) markLabelled(ctx context.Context, f store.BugFiling, issue bugIssue) error {
	next := f
	next.State = store.BugFilingLabelled
	next.IssueNodeID = issue.NodeID
	next.NextCheckAt = time.Time{}
	next.Reason = ""
	_, err := p.save(ctx, next, store.BugFilingCreated)
	return err
}

// label adds the existing bug label by node ID. Before every write and retry
// it saves the durable begin and then checks authority; after a revocation it
// only re-reads the issue. It never creates a label and never refiles.
func (p *bugFilingProcessor) label(ctx context.Context, f store.BugFiling) error {
	if f.Tracker == store.BugTrackerLinear {
		return p.labelLinear(ctx, f)
	}
	if !f.NextCheckAt.IsZero() && p.clock().Before(f.NextCheckAt) {
		return nil
	}
	if f.IssueID < 1 || f.IssueNumber < 1 {
		return errors.New("bug filing: created without an issue identity")
	}
	revoked := "authority was revoked; filed as " + f.IssueLink + ", not labelled"
	// A previous attempt may have written the label without a clear answer,
	// and the node ID may still be unknown: one read settles both.
	if f.LabelRetries > 0 || f.IssueNodeID == "" {
		issue, err := p.readFiled(ctx, f)
		if err != nil {
			return p.labelLater(ctx, f, bugReasonLabelUnclear, bugLabelRetry, false, err)
		}
		if bugLabelled(issue) {
			return p.markLabelled(ctx, f, issue)
		}
		if f.IssueNodeID == "" {
			if issue.NodeID == "" {
				return errors.New("bug filing: the issue has no node id")
			}
			f.IssueNodeID = issue.NodeID
		}
	}
	labelID, found, err := p.forge.LabelNodeID(ctx, f.RepositoryRef, bugLabel)
	if err != nil {
		return p.labelLater(ctx, f, bugReasonLabelUnclear, bugLabelRetry, false, err)
	}
	if !found {
		if f.LabelRetries+1 >= bugLabelAttempts {
			return p.labelFailed(ctx, f, "the bug label is missing; filed as "+f.IssueLink+", not labelled; create the bug label in the repository and add it to the issue")
		}
		return p.labelLater(ctx, f, bugReasonLabelMissing, bugLabelRetry, true, nil)
	}
	begun := f
	begun.LabelRetries++
	begun.NextCheckAt = p.clock().Add(bugLabelRetry)
	begun.Reason = bugReasonLabelling
	saved, err := p.save(ctx, begun, store.BugFilingCreated)
	if err != nil || !saved {
		return err
	}
	if err = p.authority(ctx, begun); err != nil {
		if !errors.Is(err, errBugFilingRevoked) {
			return err
		}
		issue, readErr := p.readFiled(ctx, begun)
		if readErr == nil && bugLabelled(issue) {
			return p.markLabelled(ctx, begun, issue)
		}
		return p.labelFailed(ctx, begun, revoked)
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	writeErr := p.forge.AddLabelsByID(call, begun.IssueNodeID, []string{labelID})
	cancel()
	if writeErr == nil {
		return p.markLabelled(ctx, begun, bugIssue{NodeID: begun.IssueNodeID})
	}
	if issue, readErr := p.readFiled(ctx, begun); readErr == nil && bugLabelled(issue) {
		return p.markLabelled(ctx, begun, issue)
	}
	if begun.LabelRetries >= bugLabelAttempts {
		return p.labelFailed(ctx, begun, "could not add the bug label; filed as "+begun.IssueLink+", not labelled; add the bug label to the issue")
	}
	if errors.Is(writeErr, github.ErrRateLimited) {
		return p.labelLater(ctx, begun, bugReasonLabelRateLimited, retryAfter(writeErr), false, nil)
	}
	return p.labelLater(ctx, begun, bugReasonLabelUnclear, bugLabelRetry, false, nil)
}

// labelLater keeps the filing created with its reason and schedules the next
// label attempt; count records a missing-label observation as an attempt.
func (p *bugFilingProcessor) labelLater(ctx context.Context, f store.BugFiling, reason string, wait time.Duration, count bool, cause error) error {
	next := f
	if count {
		next.LabelRetries++
	}
	next.NextCheckAt = p.clock().Add(wait)
	next.Reason = reason
	_, err := p.save(ctx, next, store.BugFilingCreated)
	return errors.Join(cause, err)
}

// bugFilingGitHub adapts the bot's GitHub client to the processor's port.
type bugFilingGitHub struct {
	base   *brainstormTaskGitHub
	client bugFilingClient
}

func filedIssue(issue github.Issue) bugIssue {
	return bugIssue{ID: issue.ID, NodeID: issue.NodeID, Number: issue.Number, Title: issue.Title, Body: issue.Body, Link: issue.HTMLURL, AuthorID: issue.Author.ID, Labels: issue.Labels, CreatedAt: issue.CreatedAt}
}

// CreateIssue sends the exact protected bytes. If protection would change the
// title or body, nothing is sent and the refusal is a known pre-write one.
func (g *bugFilingGitHub) CreateIssue(ctx context.Context, ref, title, body string) (bugIssue, error) {
	repo, err := g.base.repo(ctx, ref)
	if err != nil {
		return bugIssue{}, err
	}
	t, err := g.base.redactor.Apply(ctx, title, 64<<10)
	if err != nil || t.String() != title {
		return bugIssue{}, fmt.Errorf("bug filing: protection changed the title: %w", github.ErrInvalidArgument)
	}
	b, err := g.base.redactor.Apply(ctx, body, 64<<10)
	if err != nil || b.String() != body {
		return bugIssue{}, fmt.Errorf("bug filing: protection changed the body: %w", github.ErrInvalidArgument)
	}
	issue, err := g.client.CreateIssue(ctx, repo, t, b)
	if err != nil {
		return bugIssue{}, err
	}
	return filedIssue(issue), nil
}

// ListBotIssues walks the bot's issues newest first and stops at the first
// issue created before since.
func (g *bugFilingGitHub) ListBotIssues(ctx context.Context, ref string, since time.Time) ([]bugIssue, error) {
	repo, err := g.base.repo(ctx, ref)
	if err != nil {
		return nil, err
	}
	login, err := g.base.creator(ctx)
	if err != nil {
		return nil, err
	}
	var out []bugIssue
	for page := 1; page > 0 && page <= 100; {
		result, err := g.client.ListCreatedIssuesPage(ctx, repo, login, page, 100)
		if err != nil {
			return nil, err
		}
		for _, issue := range result.Items {
			if issue.CreatedAt.Before(since) {
				return out, nil
			}
			if issue.Author.ID == g.base.bot {
				out = append(out, filedIssue(issue))
			}
		}
		if result.NextPage != 0 && result.NextPage <= page {
			return nil, errors.New("bug filing: invalid issue pagination")
		}
		page = result.NextPage
	}
	return out, nil
}

func (g *bugFilingGitHub) ReadIssue(ctx context.Context, ref string, number int) (bugIssue, error) {
	repo, err := g.base.repo(ctx, ref)
	if err != nil {
		return bugIssue{}, err
	}
	issue, err := g.client.GetIssue(ctx, repo, number)
	if err != nil {
		return bugIssue{}, err
	}
	return filedIssue(issue), nil
}

func (g *bugFilingGitHub) LabelNodeID(ctx context.Context, ref, name string) (string, bool, error) {
	repo, err := g.base.repo(ctx, ref)
	if err != nil {
		return "", false, err
	}
	return g.client.RepoLabelNodeID(ctx, repo, name)
}

func (g *bugFilingGitHub) AddLabelsByID(ctx context.Context, labelableNodeID string, labelIDs []string) error {
	return g.client.AddLabelsByID(ctx, labelableNodeID, labelIDs)
}
