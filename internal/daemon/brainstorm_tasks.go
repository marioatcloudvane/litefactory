package daemon

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstormtasks"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// brainstormTaskRuntime holds the chat channel allowlist the workflow notice
// rule checks; its zero value means no Discord is configured yet.
type brainstormTaskRuntime struct {
	channels atomic.Pointer[[]string]
	// clock is wall time for the processor and launch step; nil means time.Now.
	clock func() time.Time
}

// wallNow is the processor and launch step's clock, outside the heartbeat tick.
func (r *brainstormTaskRuntime) wallNow() time.Time {
	if r.clock != nil {
		return r.clock()
	}
	return time.Now()
}

// brainstormTaskForge is what the real GitHub client offers the processor.
type brainstormTaskForge interface {
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	ListCreatedIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error)
	AuthenticatedUser(context.Context) (github.User, error)
}

// startBrainstormTasks runs the batch processor and the Plan launch step beside
// the owner-action loop, once per second, joined by the serving wait group.
func (d *Daemon) startBrainstormTasks(ctx context.Context, wg *sync.WaitGroup) {
	processor := d.brainstormTaskProcessor()
	wg.Go(func() {
		timer := time.NewTicker(time.Second)
		defer timer.Stop()
		for {
			if ctx.Err() != nil {
				return
			}
			if processor != nil {
				if err := processor.Step(ctx); err != nil {
					d.log.WarnContext(ctx, "brainstorm task batch requires inspection")
				}
			}
			d.stepBrainstormLaunches(ctx)
			d.stepBugFilings(ctx)
			d.stepBugFilingStarts(ctx)
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	})
}

func (d *Daemon) brainstormTaskProcessor() *brainstormtasks.Processor {
	forge, ok := d.forge.(brainstormTaskForge)
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
	// Like the owner-action coordinator, the processor runs on wall time
	// outside the heartbeat tick.
	clock := d.brainstormTasks.wallNow
	return &brainstormtasks.Processor{
		Store:     d.db,
		Forge:     &brainstormTaskGitHub{db: d.db, forge: forge, bot: bot, redactor: sanitizer},
		Announcer: brainstormTaskAnnouncer{d: d},
		Authority: brainstormTaskAuthority{db: d.db, clock: clock},
		BotUserID: bot,
		Now:       clock,
	}
}

// brainstormTaskGitHub adapts the GitHub client to the processor's forge port.
type brainstormTaskGitHub struct {
	db       *store.DB
	forge    brainstormTaskForge
	bot      int64
	redactor *redact.Redactor
	mu       sync.Mutex
	login    string
}

func (g *brainstormTaskGitHub) repo(ctx context.Context, ref string) (github.Repo, error) {
	forge, slug, ok := strings.Cut(ref, ":")
	if !ok {
		forge, slug = store.ForgeGitHub, ref
	}
	owner, name, ok := strings.Cut(slug, "/")
	if !ok || owner == "" || name == "" {
		return github.Repo{}, fmt.Errorf("brainstorm tasks: invalid repository reference: %w", github.ErrInvalidArgument)
	}
	repo, err := g.db.GetRepoBySlug(ctx, forge, owner, name)
	if errors.Is(err, store.ErrNotFound) {
		return github.Repo{}, fmt.Errorf("brainstorm tasks: repository is not registered: %w", github.ErrNotFound)
	}
	if err != nil {
		return github.Repo{}, err
	}
	return github.Repo{Owner: repo.Owner, Name: repo.Name}, nil
}

// CreateIssue sends the exact protected bytes. If protection would change the
// title or body, nothing is sent and the refusal is a known pre-write one.
func (g *brainstormTaskGitHub) CreateIssue(ctx context.Context, ref, title, body string) (brainstormtasks.Issue, error) {
	repo, err := g.repo(ctx, ref)
	if err != nil {
		return brainstormtasks.Issue{}, err
	}
	t, err := g.redactor.Apply(ctx, title, 64<<10)
	if err != nil || t.String() != title {
		return brainstormtasks.Issue{}, fmt.Errorf("brainstorm tasks: protection changed the title: %w", github.ErrInvalidArgument)
	}
	b, err := g.redactor.Apply(ctx, body, 64<<10)
	if err != nil || b.String() != body {
		return brainstormtasks.Issue{}, fmt.Errorf("brainstorm tasks: protection changed the body: %w", github.ErrInvalidArgument)
	}
	issue, err := g.forge.CreateIssue(ctx, repo, t, b)
	if err != nil {
		return brainstormtasks.Issue{}, err
	}
	return taskIssue(issue), nil
}

func (g *brainstormTaskGitHub) creator(ctx context.Context) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.login != "" {
		return g.login, nil
	}
	user, err := g.forge.AuthenticatedUser(ctx)
	if err != nil {
		return "", err
	}
	if user.ID != g.bot || user.Login == "" {
		return "", errors.New("brainstorm tasks: forge credential is not the bot account")
	}
	g.login = user.Login
	return g.login, nil
}

// ListBotIssues walks the bot's issues newest first and stops at the first
// issue created before since.
func (g *brainstormTaskGitHub) ListBotIssues(ctx context.Context, ref string, since time.Time) ([]brainstormtasks.Issue, error) {
	repo, err := g.repo(ctx, ref)
	if err != nil {
		return nil, err
	}
	login, err := g.creator(ctx)
	if err != nil {
		return nil, err
	}
	var out []brainstormtasks.Issue
	for page := 1; page > 0 && page <= 100; {
		result, err := g.forge.ListCreatedIssuesPage(ctx, repo, login, page, 100)
		if err != nil {
			return nil, err
		}
		for _, issue := range result.Items {
			if issue.CreatedAt.Before(since) {
				return out, nil
			}
			if issue.Author.ID == g.bot {
				out = append(out, taskIssue(issue))
			}
		}
		if result.NextPage != 0 && result.NextPage <= page {
			return nil, errors.New("brainstorm tasks: invalid issue pagination")
		}
		page = result.NextPage
	}
	return out, nil
}

func taskIssue(issue github.Issue) brainstormtasks.Issue {
	return brainstormtasks.Issue{ID: issue.ID, Number: issue.Number, Title: issue.Title, Body: issue.Body, Link: issue.HTMLURL, AuthorID: issue.Author.ID, CreatedAt: issue.CreatedAt}
}

// brainstormTaskAnnouncer queues the summary on the project's workflow notice
// channel, with the same rule durable workflows use for their notices.
type brainstormTaskAnnouncer struct{ d *Daemon }

func (a brainstormTaskAnnouncer) Announce(ctx context.Context, repositoryRef, key, text string, at time.Time) (string, error) {
	d := a.d
	if d.ownerActions == nil {
		return brainstormtasks.SummaryPending, nil
	}
	chat := d.ownerActions.notices.Load()
	channels := d.brainstormTasks.channels.Load()
	if chat == nil || channels == nil {
		return brainstormtasks.SummaryPending, nil
	}
	if d.remoteConnector == nil {
		return brainstormtasks.SummaryNoChannel, nil
	}
	channel := ""
	for _, association := range d.remoteConnector.config.WorkflowNotices {
		if association.Project == repositoryRef || "github:"+association.Project == repositoryRef || association.Project == "github:"+repositoryRef {
			channel = association.ChannelID
			break
		}
	}
	allowed := false
	for _, id := range *channels {
		allowed = allowed || id == channel
	}
	if channel == "" || !allowed {
		return brainstormtasks.SummaryNoChannel, nil
	}
	if err := chat.Announce(ctx, channel, key, "", text, at); err != nil {
		return brainstormtasks.SummaryPending, err
	}
	return brainstormtasks.SummarySent, nil
}

// brainstormTaskAuthority rechecks the continuation's owner action and the
// owner's brainstorm consent immediately before a remote write.
type brainstormTaskAuthority struct {
	db    *store.DB
	clock func() time.Time
}

func (a brainstormTaskAuthority) Allowed(ctx context.Context, executionID string) (bool, error) {
	if !strings.HasPrefix(executionID, "owner-action:") {
		return false, nil
	}
	at := a.clock().UTC()
	err := a.db.CheckOwnerActionEffect(store.WithOwnerActionEffect(ctx, executionID, a.clock))
	if err == nil {
		var record store.OwnerAction
		record, err = a.db.ReadOwnerAction(ctx, strings.TrimPrefix(executionID, "owner-action:"))
		if err == nil {
			var batch store.BrainstormTaskContinuation
			batch, err = a.db.BrainstormTaskContinuationByExecution(ctx, executionID)
			if err == nil {
				var b store.BrainstormTaskBatch
				if b, err = a.db.ReadBrainstormTaskBatch(ctx, batch.BatchID); err == nil {
					err = a.db.CheckBrainstormConsent(ctx, record.Principal, b.RepositoryRef, at)
				}
			}
		}
	}
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrConnectorOwner), errors.Is(err, store.ErrConnectorInvalid), errors.Is(err, store.ErrConnectorScope), errors.Is(err, store.ErrDoctorFenced):
		return false, nil
	}
	return false, err
}
