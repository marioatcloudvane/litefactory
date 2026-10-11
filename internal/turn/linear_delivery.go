package turn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

type codeDeliveryComments interface {
	ListDeliveryComments(context.Context, github.Repo, int, int64) ([]github.Comment, error)
	PostDeliveryComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}

func (p *StoryProgress) linearDelivery(ctx context.Context, parent string) (bool, error) {
	s, err := p.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return false, err
	}
	binding, err := p.db.GetSessionTicket(ctx, s.ID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return binding.Locator.Provider == ticket.Linear, nil
}
func (p *StoryProgress) deliveryPublisherID(ctx context.Context, parent string) (int64, error) {
	session, err := p.db.GetSessionByTaskRef(ctx, parent)
	if err != nil {
		return 0, err
	}
	snapshot, err := p.db.GetExecutionSnapshot(ctx, session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return p.botID, nil
	}
	if err != nil {
		return 0, err
	}
	if snapshot.CodeActorID > 0 {
		return snapshot.CodeActorID, nil
	}
	return snapshot.ActorID, nil
}

func (p *StoryProgress) proposeDeliveryNotice(ctx context.Context, q store.DeliveryProposal, detail string) error {
	linear, err := p.linearDelivery(ctx, q.Parent)
	if err != nil {
		return err
	}
	if !linear {
		return p.deliveryNotice(ctx, q.Parent, q.Key, "proposal", detail)
	}
	s, err := p.db.GetSessionByTaskRef(ctx, q.Parent)
	if err != nil {
		return err
	}
	authority, err := p.db.EnsureDeliveryCodeAuthority(ctx, s.ID)
	if err != nil {
		return err
	}
	if _, err = p.db.BindDeliveryPR(ctx, q, s.ID); err != nil {
		return err
	}
	gateway, err := p.deliveryGateway(ctx, q.Parent)
	if err != nil {
		return err
	}
	comments, ok := gateway.(codeDeliveryComments)
	if !ok {
		return errors.New("bound GitHub PR comment publication unavailable")
	}
	repo, err := p.deliveryCodeRepository(ctx, q.Parent)
	if err != nil {
		return err
	}
	pr, err := gateway.GetDeliveryPR(ctx, repo, q.PRNumber)
	if err != nil {
		return err
	}
	if err = deliveryPRMatches(q, pr, repo); err != nil {
		return err
	}
	marker := "<!-- litefactory:code-proposal:" + q.Key + " -->"
	renderer, ok := p.renderer.(deliveryRenderer)
	if !ok {
		return errors.New("delivery renderer unavailable")
	}
	body, err := renderer.RenderDeliveryNotice(ctx, detail, marker)
	if err != nil {
		return err
	}
	key := "code-pr-proposal:" + q.Key
	intent, err := p.db.EnsureReportDelivery(ctx, store.ReportDelivery{Key: key, SessionID: s.ID, Task: q.Parent, Destination: fmt.Sprintf("%s#%d", authority.Repository, q.PRNumber), TriggerNamespace: "code-pr-proposal", Purpose: "exact-code-approval", BotUserID: authority.PublisherID, Body: body.String(), Marker: marker})
	if err != nil {
		return err
	}
	if !intent.Completed {
		observed, err := comments.ListDeliveryComments(ctx, repo, q.PRNumber, 0)
		if err != nil {
			return err
		}
		var receipt int64
		var publishedAt time.Time
		for _, c := range observed {
			if c.Author.ID == intent.BotUserID && exactProgressMarker(c.Body, marker) {
				if err = verifyDeliveryPromptReceipt(c, authority, q.PRNumber, intent); err != nil {
					return err
				}
				if receipt > 0 && receipt != c.ID {
					return errors.New("ambiguous GitHub proposal comment receipts")
				}
				receipt = c.ID
				publishedAt = c.CreatedAt
			}
		}
		if receipt == 0 {
			claimed, err := p.db.MarkReportDeliverySent(ctx, key)
			if err != nil {
				return err
			}
			if !claimed {
				return errors.New("GitHub approval prompt write remains uncertain; awaiting exact receipt")
			}
			prepared, err := renderer.RenderDeliveryNotice(ctx, detail, marker)
			if err != nil {
				return err
			}
			if prepared.String() != intent.Body {
				return errors.New("prepared GitHub approval prompt changed")
			}
			c, err := comments.PostDeliveryComment(ctx, repo, q.PRNumber, prepared)
			if err != nil {
				return err
			}
			if err = verifyDeliveryPromptReceipt(c, authority, q.PRNumber, intent); err != nil {
				return err
			}
			receipt = c.ID
			publishedAt = c.CreatedAt
		}
		if err = p.db.EnrollDeliveryPRNotice(ctx, q.Key, key, publishedAt); err != nil {
			return err
		}
		if _, err = p.db.ConfirmReportDelivery(ctx, key, receipt); err != nil {
			return err
		}
		if err = p.db.CompleteDeliveryPRNotice(ctx, key); err != nil {
			return err
		}
	}
	return p.deliveryNotice(ctx, q.Parent, q.Key, "proposal", "Code delivery is waiting for the GitHub code owner's exact approval on "+fmt.Sprintf("https://github.com/%s/pull/%d", authority.Repository, q.PRNumber)+". Linear ticket approvals do not authorize merging.")
}

// verifyDeliveryPromptReceipt checks the same exact prepared publication on
// both fresh mutation responses and recovery reads before replay enrollment.
func verifyDeliveryPromptReceipt(c github.Comment, authority store.DeliveryCodeAuthority, prNumber int, intent store.ReportDelivery) error {
	if c.ID <= 0 || c.Author.ID != intent.BotUserID || c.Author.ID != authority.PublisherID || c.Body != intent.Body || c.CreatedAt.IsZero() || !exactProgressMarker(c.Body, intent.Marker) {
		return errors.New("GitHub approval prompt receipt differs")
	}
	if c.APIURL != fmt.Sprintf("%s/repos/%s/issues/comments/%d", authority.Origin, authority.Repository, c.ID) || c.IssueAPIURL != fmt.Sprintf("%s/repos/%s/issues/%d", authority.Origin, authority.Repository, prNumber) {
		return errors.New("GitHub approval prompt receipt belongs to another destination")
	}
	return nil
}
