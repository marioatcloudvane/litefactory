package projectconnection

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ObservationStore owns the atomic current-version check after network reads.
type ObservationStore interface {
	SaveConnectionObservation(context.Context, store.ConnectionObservation) error
}

// Check performs only bounded reads. Unknown write access stays unknown.
func (f *Factory) Check(ctx context.Context, v store.ConnectionVersion) store.ConnectionObservation {
	observation := store.ConnectionObservation{VersionID: v.ID, CheckedAt: time.Now().UTC(), Status: "unchecked", Reason: "unchecked", Remedy: "Run a connection check."}
	if !v.Enabled {
		observation.Status = "disabled"
		observation.Reason = "disabled"
		observation.Remedy = "Enable a new connection version for future work."
		return observation
	}
	if v.Provider == ticket.Linear {
		return f.checkLinear(ctx, v)
	}
	ctx, cancel := context.WithTimeout(ctx, f.options.Timeout)
	defer cancel()
	clients, err := f.load(ctx, v, 0, true)
	if err != nil {
		observation.Status, observation.Reason, observation.Remedy = healthFailure(err)
		return observation
	}
	observation.Capabilities = []string{"authenticated_actor"}
	parts := strings.Split(v.Destination, "/")
	if len(parts) != 2 {
		observation.Status = "conflict"
		observation.Reason = "invalid_destination"
		observation.Remedy = "Set the configured repository owner and name."
		return observation
	}
	repo := github.Repo{Owner: parts[0], Name: parts[1]}
	access, err := clients.Code.InspectRepository(ctx, repo)
	if err != nil {
		observation.Status, observation.Reason, observation.Remedy = healthFailure(err)
		return observation
	}
	if !strings.EqualFold(access.FullName, v.Destination) || (v.RemoteID != "" && v.RemoteID != strconv.FormatInt(access.ID, 10)) {
		observation.Status = "conflict"
		observation.Reason = "destination_identity_changed"
		observation.Remedy = "Correct the connection after verifying the original destination."
		return observation
	}
	observation.Capabilities = append(observation.Capabilities, "repository_read")
	if v.Role == store.TicketConnection {
		if !access.IssuesEnabled {
			observation.Status = "insufficient_permission"
			observation.Reason = "issues_disabled"
			observation.Remedy = "Enable issues at the configured destination."
			return observation
		}
		// Repository metadata alone does not prove ticket visibility.
		if _, _, err := clients.Code.ReadOpenIssues(ctx, repo, 1); err != nil {
			observation.Status, observation.Reason, observation.Remedy = healthFailure(err)
			return observation
		}
		observation.Capabilities = append(observation.Capabilities, "ticket_read")
	}
	observation.Capabilities = append(observation.Capabilities, "write_unknown")
	observation.Status = "usable_for_checked_capabilities"
	observation.Reason = "read_access_verified"
	observation.Remedy = "Write access is unverified; ordinary operations still enforce permissions."
	return observation
}

// CheckAndSave rejects observations whose version stopped being current during reads.
func (f *Factory) CheckAndSave(ctx context.Context, persistence ObservationStore, v store.ConnectionVersion) (store.ConnectionObservation, error) {
	observation := f.Check(ctx, v)
	err := persistence.SaveConnectionObservation(ctx, observation)
	return observation, err
}

func healthFailure(err error) (status, reason, remedy string) {
	switch {
	case errors.Is(err, ErrActorChanged):
		return "conflict", "actor_changed", "Restore the credential for the retained publishing actor."
	case errors.Is(err, ErrCredentialPolicy):
		return "missing_credential", "credential_policy", "Use an external keychain reference for background work."
	case errors.Is(err, secrets.ErrInteractionNotAllowed):
		return "locked_credential", "keychain_locked", "Unlock the login keychain, then check again."
	case errors.Is(err, secrets.ErrNotConfigured), errors.Is(err, secrets.ErrNotFound), errors.Is(err, secrets.ErrEmpty), errors.Is(err, secrets.ErrIncomplete):
		return "missing_credential", "credential_unavailable", "Correct the external credential reference, then check again."
	case errors.Is(err, github.ErrUnauthorized):
		return "rejected_credential", "credential_rejected", "Restore or rotate the external credential for the same actor."
	case errors.Is(err, github.ErrForbidden):
		return "insufficient_permission", "access_denied", "Grant the existing actor access to the configured destination."
	case errors.Is(err, github.ErrNotFound):
		return "inaccessible_or_missing_destination", "destination_unavailable", "Verify the destination exists and the existing actor can read it."
	case errors.Is(err, github.ErrRateLimited):
		return "rate_limited", "rate_limited", "Wait for the provider rate limit, then check again."
	case errors.Is(err, ErrInvalidConnection):
		return "conflict", "invalid_configuration", "Correct the trusted provider, origin or destination configuration."
	default:
		return "transient", "check_failed", "Retry the bounded check and inspect external access if it persists."
	}
}
