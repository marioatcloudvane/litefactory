package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// ApplyDelivery routes explicit owner controls without starting an agent.
func (e turnExecutor) ApplyDelivery(ctx context.Context, c turn.DeliveryControl) error {
	if e.opts.StoryProgress == nil {
		return fmt.Errorf("delivery service unavailable")
	}
	return e.opts.StoryProgress.ApplyDelivery(ctx, c)
}
func (d *Daemon) handleDelivery(ctx context.Context, t *task, c github.Comment, log *slog.Logger) error {
	owner := d.filter.Identities().OwnerUserID
	if owner < 1 || c.Author.ID != owner {
		return fmt.Errorf("delivery requires numeric owner identity")
	}
	applier, ok := d.turn.(interface {
		ApplyDelivery(context.Context, turn.DeliveryControl) error
	})
	if !ok {
		return d.leave(ctx, t, log, "delivery service unavailable", fmt.Errorf("executor lacks delivery controls"))
	}
	err := applier.ApplyDelivery(ctx, turn.DeliveryControl{Parent: t.ref.String(), CommentID: c.ID, ActorID: c.Author.ID, OwnerID: owner, Body: c.Body})
	if err != nil {
		return d.leave(ctx, t, log, "delivery remains pending", err)
	}
	_, err = d.reporter.Consume(ctx, t.session.ID, c.ID)
	return d.leave(ctx, t, log, "could not consume delivery control", err)
}

func isCodeAuthorityControl(name string) bool {
	return name == "/deliver" || name == "/merge" || name == "/validate-ship" || name == "/validate-push" || name == "/validate-merge" || name == "/validate-close"
}

func (d *Daemon) authorizeCodeControl(ctx context.Context, sessionID int64, c github.Comment) error {
	ids := d.filter.Identities()
	if c.Author.ID <= 0 || c.Author.ID != ids.OwnerUserID || c.Author.ID == ids.BotUserID || c.Author.IsBot || turn.HasMarker(c.Body) {
		return errors.New("code delivery requires its authenticated human authority")
	}
	if d.connections == nil {
		return nil
	}
	mapping, err := d.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return err
	}
	saved, err := d.db.GetExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return err
	}
	code, err := d.db.GetConnectionVersion(ctx, saved.CodeVersionID)
	if err != nil {
		return err
	}
	owner, err := d.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
	if err != nil {
		return err
	}
	// Legacy gh identity discovery is explicitly pinned to public GitHub.
	if owner.Source != store.SourceGH || code.Provider != ticket.GitHub || code.Origin != "https://api.github.com" || mapping.Locator.Provider != code.Provider || mapping.Locator.Origin != code.Origin || c.Author.ID == saved.ActorID {
		return errors.New("ticket actor does not match the qualified code authority")
	}
	return nil
}

func (d *Daemon) claimCodeControl(ctx context.Context, sessionID int64, c github.Comment) (store.AuthorityClaim, error) {
	session, err := d.db.GetSession(ctx, sessionID)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	trigger, err := d.projectTrigger(ctx, sessionID, c)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	a, err := d.db.RawCurrentApprover(ctx, session.RepoID)
	if err != nil && !errors.Is(err, store.ErrApproverMissing) {
		return store.AuthorityClaim{}, err
	}
	return d.db.ClaimProjectCodeAuthority(ctx, store.AuthorityClaim{RepoID: session.RepoID, SessionID: sessionID, AppointmentID: a.ID, Revision: a.Revision, Actor: trigger.Actor, Source: trigger.Source, CreatedAt: c.CreatedAt, Kind: "code-control", Key: strconv.FormatInt(sessionID, 10) + ":" + strconv.FormatInt(c.ID, 10)})
}

// claimCodePollingWorker holds the saved code boundary while polling controls.
// It grants no reply authority; each observed control still verifies its author.
func (d *Daemon) claimCodePollingWorker(ctx context.Context, sessionID int64, key string) (store.AuthorityClaim, error) {
	session, err := d.db.GetSession(ctx, sessionID)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	owner, err := d.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	saved, err := d.db.GetExecutionSnapshot(ctx, sessionID)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	mapping, err := d.db.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	ref, err := mapping.Ref()
	if err != nil {
		return store.AuthorityClaim{}, err
	}
	if owner.Source != store.SourceGH || owner.UserID <= 0 || owner.UserID == saved.ActorID || ref.Identity.Provider != ticket.GitHub || ref.Identity.Origin != "https://api.github.com" {
		return store.AuthorityClaim{}, store.ErrApproverUnauthorized
	}
	a, err := d.db.RawCurrentApprover(ctx, session.RepoID)
	if err != nil && !errors.Is(err, store.ErrApproverMissing) {
		return store.AuthorityClaim{}, err
	}
	return d.db.ClaimProjectCodeAuthority(ctx, store.AuthorityClaim{RepoID: session.RepoID, SessionID: sessionID, AppointmentID: a.ID, Revision: a.Revision, Actor: ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(owner.UserID, 10)}, Source: ref.Identity, Kind: "worker", Key: key})
}
