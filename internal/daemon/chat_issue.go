package daemon

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// ChatIssueForge is the only write capability granted to chat. There is no
// comment, assignment, label, edit, close or workflow operation in this port.
type ChatIssueForge interface {
	CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error)
	ListIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error)
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
}

// ChatIssueCreator reconciles a durable claim before any first or retry create.
// It runs only on the existing single background chat worker.
type ChatIssueCreator struct {
	DB        *store.DB
	Forge     ChatIssueForge
	BotUserID int64
	Redactor  *redact.Redactor
	Now       func() time.Time
}

// Confirm claims a current code, reconciles its marker and records the outcome.
func (c *ChatIssueCreator) Confirm(ctx context.Context, transport, channel, message, code string, at time.Time) (string, error) {
	if c.DB == nil || c.Forge == nil || c.Redactor == nil || c.BotUserID < 1 {
		presentation.Assess(ctx, presentation.ReportableError)
		return "Issue creation unavailable right now.", nil
	}
	p, status, err := c.DB.ClaimChatIssuePreview(ctx, transport, channel, message, code, at)
	if err != nil {
		presentation.Assess(ctx, presentation.ReportableError)
		return "Nothing was created; preview storage is unavailable.", err
	}
	switch status {
	case "unknown":
		presentation.Assess(ctx, presentation.Refusal)
		return "No such preview in this channel. Ask for /draft.", nil
	case "separate":
		presentation.Assess(ctx, presentation.Refusal)
		return "Confirm in a separate message with /confirm.", nil
	case "created":
		presentation.Assess(ctx, presentation.Success)
		return "Feature issue: " + p.IssueLink, nil
	case "superseded":
		current, e := c.DB.LatestChatIssuePreview(ctx, transport, channel)
		if errors.Is(e, store.ErrNotFound) {
			presentation.Assess(ctx, presentation.Refusal)
			return "That preview was replaced. Ask for /draft.", nil
		}
		if e != nil {
			presentation.Assess(ctx, presentation.ReportableError)
			return "", e
		}
		presentation.Assess(ctx, presentation.Refusal)
		return "That preview was replaced. Current code: " + current.Code + ".", nil
	case "expired":
		presentation.Assess(ctx, presentation.Refusal)
		return "That preview expired. Ask for a new /draft.", nil
	case "busy", "repeat":
		presentation.Assess(ctx, presentation.Uncertain)
		return "Creation is being checked. Send a new /confirm later to check the outcome.", nil
	}
	finish := func(state string, issue github.Issue, text string) (string, error) {
		assessment := presentation.Refusal
		switch state {
		case "created":
			assessment = presentation.Success
		case "uncertain":
			assessment = presentation.Uncertain
		}
		presentation.Assess(ctx, assessment)
		now := at
		if c.Now != nil {
			now = c.Now()
		}
		err := c.DB.FinishChatIssuePreview(ctx, p, state, issue.Number, issue.HTMLURL, now)
		return text, err
	}
	if p.ContentHash != feature.Hash(p.Title, p.Body) || !strings.HasPrefix(p.Body, feature.Heading+"\n") {
		return finish("previewed", github.Issue{}, "Preview content changed. Nothing was created; ask for a new /draft.")
	}
	repo, err := c.DB.GetRepo(ctx, p.RepoID)
	if err != nil {
		return finish("uncertain", github.Issue{}, "Could not check the project. Send /confirm again later.")
	}
	target := github.Repo{Owner: repo.Owner, Name: repo.Name}
	body := feature.IssueBody(p)
	// The protected payload must still be the exact preview bytes and fixed
	// additions. A changed redaction policy requires a fresh preview.
	titleProtected, err := c.Redactor.Apply(ctx, p.Title, 64<<10)
	if err != nil {
		return finish("previewed", github.Issue{}, "Could not protect the issue. Nothing was created.")
	}
	bodyProtected, err := c.Redactor.Apply(ctx, body, 64<<10)
	if err != nil || titleProtected.String() != p.Title || bodyProtected.String() != body {
		return finish("previewed", github.Issue{}, "Preview protection changed. Nothing was created; ask for a new /draft.")
	}
	existing, found, err := c.find(ctx, target, feature.Marker(p))
	if err != nil {
		return finish("uncertain", github.Issue{}, "Could not check whether the issue exists. Send /confirm again later.")
	}
	if found {
		observed, e := c.Forge.GetIssue(ctx, target, existing.Number)
		if e != nil || !c.matches(observed, p.Title, body) {
			return finish("uncertain", github.Issue{}, "Found an issue but could not verify it. Send /confirm again later.")
		}
		return finish("created", observed, "Feature issue: "+observed.HTMLURL)
	}
	if status == "uncertain" && at.Before(p.LastAttemptAt.Add(2*time.Minute)) {
		return finish("uncertain", github.Issue{}, "Creation outcome is uncertain. Wait at least 2 minutes from the attempt, then /confirm again.")
	}
	// Marker listing can take time. Start the retry guard at the actual create
	// attempt, rather than at the beginning of reconciliation.
	attemptAt := at
	if c.Now != nil {
		attemptAt = c.Now()
	}
	if err := c.DB.BeginChatIssueCreate(ctx, p, attemptAt); err != nil {
		presentation.Assess(ctx, presentation.ReportableError)
		return "Nothing was created; preview storage is unavailable.", err
	}
	created, err := c.Forge.CreateIssue(ctx, target, titleProtected, bodyProtected)
	if err != nil {
		if knownCreateFailure(err) {
			return finish("previewed", github.Issue{}, "Could not create the issue. Try /confirm again before the preview expires.")
		}
		return finish("uncertain", github.Issue{}, "Creation outcome is uncertain. Send /confirm again to check the issue.")
	}
	observed, err := c.Forge.GetIssue(ctx, target, created.Number)
	if err != nil || !c.matches(observed, p.Title, body) {
		return finish("uncertain", github.Issue{}, "Creation outcome is uncertain. Send /confirm again to verify the issue.")
	}
	return finish("created", observed, "Feature issue: "+observed.HTMLURL)
}
func (c *ChatIssueCreator) matches(issue github.Issue, title, body string) bool {
	return issue.Author.ID == c.BotUserID && issue.Title == title && issue.Body == body && issue.Number > 0 && issue.HTMLURL != ""
}
func knownCreateFailure(err error) bool {
	return errors.Is(err, github.ErrInvalidArgument) || errors.Is(err, github.ErrUnauthorized) || errors.Is(err, github.ErrForbidden) || errors.Is(err, github.ErrNotFound) || errors.Is(err, github.ErrUnprocessable) || errors.Is(err, github.ErrRateLimited)
}
func (c *ChatIssueCreator) find(ctx context.Context, repo github.Repo, marker string) (github.Issue, bool, error) {
	var match github.Issue
	// Closed issues also matter: closing a created issue must not make a retry
	// create another one. Walk every page, fail closed on incomplete reads.
	for _, state := range []string{github.StateOpen, "closed"} {
		for page := 1; page > 0; {
			result, err := c.Forge.ListIssuesPage(ctx, repo, state, page, 100)
			if err != nil {
				return match, false, err
			}
			for _, issue := range result.Items {
				if issue.Author.ID != c.BotUserID {
					continue
				}
				for _, line := range strings.Split(issue.Body, "\n") {
					if line == marker {
						if match.Number > 0 && match.Number != issue.Number {
							return match, false, errors.New("multiple issues carry preview marker")
						}
						match = issue
					}
				}
			}
			if result.NextPage != 0 && result.NextPage <= page {
				return match, false, errors.New("invalid issue pagination")
			}
			page = result.NextPage
		}
	}
	return match, match.Number > 0, nil
}
