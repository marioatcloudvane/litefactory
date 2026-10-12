package store

import (
	"context"
	"database/sql"
)

// retainedCodeClaimConnections verifies the immutable source and execution
// destination of a saved session before admitting a separately authorized control.
func retainedCodeClaimConnections(ctx context.Context, tx *sql.Tx, c AuthorityClaim) (ConnectionVersion, ConnectionVersion, error) {
	var code, tickets ConnectionVersion
	session, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=?`, c.SessionID))
	if err != nil {
		return code, tickets, err
	}
	snapshot, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, c.SessionID))
	if err != nil {
		return code, tickets, err
	}
	binding, err := scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE id=?`, snapshot.BindingID))
	if err != nil {
		return code, tickets, err
	}
	identity, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, c.SessionID))
	if err != nil {
		return code, tickets, err
	}
	ref, err := identity.Ref()
	if err != nil {
		return code, tickets, err
	}
	if session.RepoID != c.RepoID || snapshot.RepoID != c.RepoID || binding.RepoID != c.RepoID || ref != binding.Ref {
		return code, tickets, ErrBindingConflict
	}
	code, err = scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, snapshot.CodeVersionID))
	if err != nil {
		return code, tickets, err
	}
	tickets, err = scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, snapshot.TicketVersionID))
	if err != nil {
		return code, tickets, err
	}
	if code.RepoID != c.RepoID || code.Role != CodeConnection || tickets.RepoID != c.RepoID || tickets.Role != TicketConnection || tickets.Provider != ref.Identity.Provider || tickets.Origin != ref.Identity.Origin || tickets.Destination != ref.Locator.Container || c.Source.Provider != ref.Identity.Provider || c.Source.Origin != ref.Identity.Origin {
		return code, tickets, ErrApproverUnauthorized
	}
	return code, tickets, nil
}
