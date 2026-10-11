package turn

import (
	"context"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// bindProjectApprover makes one invocation's ticket authority immutable while
// keeping publishing and code delivery identities in their existing bindings.
func bindProjectApprover(ctx context.Context, opts Options, sessionID int64) (Options, error) {
	if !opts.ProjectApprovers {
		return opts, nil
	}
	opts.TicketApprover = nil
	session, err := opts.DB.GetSession(ctx, sessionID)
	if err != nil {
		return opts, err
	}
	a, err := opts.DB.CurrentApprover(ctx, session.RepoID)
	if err != nil {
		return opts, err
	}
	mapping, err := opts.DB.GetSessionTicket(ctx, sessionID)
	if err != nil {
		return opts, err
	}
	if a.Actor.Provider != mapping.Locator.Provider || a.Actor.Origin != mapping.Locator.Origin {
		return opts, store.ErrApproverUnauthorized
	}
	connection, err := opts.DB.CurrentConnection(ctx, session.RepoID, store.TicketConnection)
	if err != nil {
		return opts, err
	}
	if !connection.Enabled {
		return opts, store.ErrConnectionDisabled
	}
	if a.Actor.Provider != connection.Provider || a.Actor.Origin != connection.Origin {
		return opts, store.ErrApproverUnauthorized
	}
	opts.TicketApprover = &a
	return opts, nil
}

func ticketOwnerID(ctx context.Context, opts Options) (int64, error) {
	if opts.ProjectApprovers {
		if opts.TicketApprover == nil {
			return 0, store.ErrApproverMissing
		}
		id, err := strconv.ParseInt(opts.TicketApprover.Actor.RemoteID, 10, 64)
		if err != nil || id <= 0 {
			return 0, store.ErrApproverUnauthorized
		}
		return id, nil
	}
	owner, err := opts.DB.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
	return owner.UserID, err
}

func claimTurnAuthority(ctx context.Context, opts Options, req Request) (store.AuthorityClaim, error) {
	a := opts.TicketApprover
	if a == nil {
		return store.AuthorityClaim{}, store.ErrApproverMissing
	}
	if req.Trigger.AuthorID <= 0 || strconv.FormatInt(req.Trigger.AuthorID, 10) != req.Trigger.Actor.RemoteID {
		return store.AuthorityClaim{}, store.ErrApproverUnauthorized
	}
	return opts.DB.ClaimProjectAuthority(ctx, store.AuthorityClaim{RepoID: a.RepoID, AppointmentID: a.ID, Revision: a.Revision, Actor: req.Trigger.Actor, Source: req.Trigger.Source, CreatedAt: req.Trigger.CreatedAt, Kind: "turn", Key: strconv.FormatInt(req.SessionID, 10) + ":" + strconv.FormatInt(req.Trigger.CommentID, 10)})
}

func isValidateCodeControlBody(body string) bool {
	fields := strings.Fields(body)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case ValidatePushCommand, ValidateMergeCommand, ValidateCloseCommand, ValidateShipCommand:
		return true
	}
	return false
}

func verifyTurnCodeAuthority(ctx context.Context, opts Options, session store.Session, trigger Trigger) error {
	owner, err := opts.DB.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner)
	if err != nil {
		return err
	}
	saved, err := opts.DB.GetExecutionSnapshot(ctx, session.ID)
	if err != nil {
		return err
	}
	connection, err := opts.DB.GetConnectionVersion(ctx, saved.CodeVersionID)
	if err != nil {
		return err
	}
	if !connection.Enabled {
		return store.ErrConnectionDisabled
	}
	// Legacy gh discovery is pinned to the public GitHub origin. Equal bare IDs
	// on another provider instance never establish code authority.
	expected := ticket.Identity{Provider: ticket.GitHub, Origin: "https://api.github.com", RemoteID: strconv.FormatInt(owner.UserID, 10)}
	if owner.Source != store.SourceGH || owner.UserID <= 0 || owner.UserID == saved.ActorID || trigger.AuthorID != owner.UserID || !expected.Equal(trigger.Actor) || trigger.Source.Provider != expected.Provider || trigger.Source.Origin != expected.Origin || connection.Provider != expected.Provider || connection.Origin != expected.Origin {
		return store.ErrApproverUnauthorized
	}
	return trigger.Source.Validate()
}
