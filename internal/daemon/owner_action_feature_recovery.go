package daemon

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
	"github.com/cloudvane/litefactory/internal/store"
)

// RecoverOwnerActionFeature reconciles observed issue facts without admitting a
// create attempt. It never selects a newer preview or calls the effect handler.
func (c *ChatIssueCreator) RecoverOwnerActionFeature(ctx context.Context, transport, channel, code string, at time.Time) (string, bool, error) {
	if c == nil || c.DB == nil || c.Forge == nil || c.BotUserID < 1 {
		return "", false, errors.New("feature recovery unavailable")
	}
	p, err := c.DB.ChatIssuePreviewByCode(ctx, transport, channel, code)
	if errors.Is(err, store.ErrNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if p.State == "created" && p.IssueNumber > 0 && p.IssueLink != "" {
		return "Feature issue: " + p.IssueLink, true, nil
	}
	if p.State != "previewed" && p.State != "creating" && p.State != "uncertain" {
		return "", false, nil
	}
	if p.ContentHash != feature.Hash(p.Title, p.Body) || !strings.HasPrefix(p.Body, feature.Heading+"\n") {
		return "", false, errors.New("feature recovery preview integrity mismatch")
	}
	repo, err := c.DB.GetRepo(ctx, p.RepoID)
	if err != nil {
		return "", false, err
	}
	if repo.Forge != store.ForgeGitHub {
		return "", false, errors.New("feature recovery repository is unsupported")
	}
	target := github.Repo{Owner: repo.Owner, Name: repo.Name}
	candidate, found, err := c.find(ctx, target, feature.Marker(p))
	if err != nil || !found {
		return "", false, err
	}
	observed, err := c.Forge.GetIssue(ctx, target, candidate.Number)
	if err != nil {
		return "", false, err
	}
	if observed.Number != candidate.Number || !c.matches(observed, p.Title, feature.IssueBody(p)) {
		return "", false, errors.New("feature recovery issue does not match the exact preview")
	}
	text := "Feature issue: " + observed.HTMLURL
	// A live worker owns its lease. Observation can settle the shared journal
	// without stealing that claim. An expired claim is fenced by its version;
	// a concurrent replacement prevents this optional receipt update.
	if p.State == "creating" && p.ClaimVersion > 0 && !p.LeaseUntil.IsZero() && !at.Before(p.LeaseUntil) {
		if err = c.DB.FinishChatIssuePreview(ctx, p, "created", observed.Number, observed.HTMLURL, at); err != nil && !errors.Is(err, store.ErrNotFound) {
			return text, true, err
		}
	}
	return text, true, nil
}
