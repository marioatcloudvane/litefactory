package projectconnection

import (
	"context"
	"errors"
	"strconv"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// InitializeApprovers carries forward only migration-captured GitHub authority.
// Failures remain local to the project and retain every saved decision and effect.
func InitializeApprovers(ctx context.Context, db *store.DB, factory *Factory) error {
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		if existing, err := db.RawCurrentApprover(ctx, repo.ID); err == nil {
			if existing.Provenance != "legacy" {
				continue
			}
			state, stateErr := db.GetApproverInitialization(ctx, repo.ID)
			if stateErr != nil && !errors.Is(stateErr, store.ErrNotFound) {
				return stateErr
			}
			if state.Status == "complete" {
				continue
			}
		} else if !errors.Is(err, store.ErrApproverMissing) {
			return err
		}
		eligible, err := db.LegacyApproverEligible(ctx, repo.ID)
		if err != nil {
			return err
		}
		if !eligible {
			continue
		}
		reason := initializeApprover(ctx, db, factory, repo.ID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		status := "complete"
		if reason != "" {
			status = "conflict"
		}
		if err := db.SetApproverInitialization(ctx, store.ApproverInitialization{RepoID: repo.ID, Status: status, Reason: reason}); err != nil {
			return err
		}
	}
	return nil
}

func initializeApprover(ctx context.Context, db *store.DB, factory *Factory, projectID int64) string {
	v, err := db.CurrentConnection(ctx, projectID, store.TicketConnection)
	if err != nil || v.Provider != ticket.GitHub || v.Origin != ticket.Origin("https://api.github.com") {
		return "legacy_approver_origin_unknown; configure a verified explicit approver"
	}
	owner, err := db.LegacyApproverIdentity(ctx, projectID)
	if err != nil || owner.UserID <= 0 || owner.Source != store.SourceGH {
		return "legacy_approver_identity_missing; configure a verified explicit approver"
	}
	actors, err := db.LegacyApproverOwnerIDs(ctx, projectID)
	if err != nil {
		return "legacy_approval_history_unresolved; inspect saved owner decisions"
	}
	for _, actor := range actors {
		// An unknown (0) historical author is not a different author.
		if actor > 0 && actor != owner.UserID {
			return "legacy_approval_actor_conflict; reconcile historical owner identities"
		}
	}
	clients, err := factory.Clients(ctx, v, 0)
	if err != nil {
		return "legacy_approver_verification_unavailable; retry credential or provider verification"
	}
	user, err := clients.Code.UserByID(ctx, owner.UserID)
	if err != nil {
		return "legacy_approver_verification_unavailable; verify the saved immutable GitHub account"
	}
	a, err := verifiedAppointment(v, user, clients.Actor)
	if err != nil {
		return "legacy_approver_account_invalid; configure a verified human account distinct from publisher"
	}
	if a.Actor.RemoteID != strconv.FormatInt(owner.UserID, 10) {
		return "legacy_approver_identity_conflict; reconcile the saved immutable account"
	}
	a.Provenance = "legacy"
	saved, err := db.ImportLegacyApprover(ctx, a, v.Revision)
	if err != nil {
		return "legacy_approver_admission_conflict; inspect connection and authority revisions"
	}
	if err := db.ReconcileLegacyApproverEvidence(ctx, saved); err != nil {
		return "legacy_approval_history_unresolved; reconcile saved authority evidence"
	}
	return ""
}
