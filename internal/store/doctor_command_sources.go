package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// DoctorCommandSource pins the observed ticket destination and publishing actor
// independently of the ticket approver or current project connection.
type DoctorCommandSource struct {
	CommandID, ConnectionVersionID, PublisherID int64
	Ticket                                      ticket.Ref
}

const doctorSourceColumns = `command_id,connection_version_id,publisher_id,provider,api_origin,remote_id,container,locator_key`

func scanDoctorSource(row scanner) (DoctorCommandSource, error) {
	var s DoctorCommandSource
	err := row.Scan(&s.CommandID, &s.ConnectionVersionID, &s.PublisherID, &s.Ticket.Identity.Provider, &s.Ticket.Identity.Origin, &s.Ticket.Identity.RemoteID, &s.Ticket.Locator.Container, &s.Ticket.Locator.Key)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Ticket.Locator.Provider = s.Ticket.Identity.Provider
	s.Ticket.Locator.Origin = s.Ticket.Identity.Origin
	return s, s.Ticket.Validate()
}

// GetDoctorCommandSource reads original trusted routing for an admitted command.
func (db *DB) GetDoctorCommandSource(ctx context.Context, id int64) (DoctorCommandSource, error) {
	return scanDoctorSource(db.sql.QueryRowContext(ctx, `SELECT `+doctorSourceColumns+` FROM doctor_command_sources WHERE command_id=?`, id))
}

// GetDoctorDeliverySource resolves a publication to its original command source.
func (db *DB) GetDoctorDeliverySource(ctx context.Context, key string) (DoctorCommandSource, error) {
	return scanDoctorSource(db.sql.QueryRowContext(ctx, `SELECT `+doctorSourceColumns+` FROM doctor_command_sources WHERE command_id=(SELECT doctor_command_id FROM report_deliveries WHERE delivery_key=?)`, key))
}
func retainDoctorSource(ctx context.Context, tx *sql.Tx, c DoctorCommand, source DoctorCommandSource) error {
	ref, parseErr := naming.ParseRef(c.TaskRef)
	if parseErr != nil || ref.Forge != string(source.Ticket.Locator.Provider) || !strings.EqualFold(ref.Owner+"/"+ref.Repo, source.Ticket.Locator.Container) {
		return ErrTicketIdentityConflict
	}
	if source.PublisherID <= 0 || source.PublisherID == c.ActorID || source.Ticket.Validate() != nil || source.Ticket.Identity.RemoteID != strconv.FormatInt(c.IssueID, 10) || source.Ticket.Locator.Key != strconv.Itoa(c.IssueNumber) {
		return ErrTicketIdentityConflict
	}
	connection, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, source.ConnectionVersionID))
	if err != nil {
		return err
	}
	if connection.RepoID != c.RepoID || connection.Role != TicketConnection || connection.Provider != source.Ticket.Identity.Provider || connection.Origin != source.Ticket.Identity.Origin || connection.Destination != source.Ticket.Locator.Container {
		return ErrTicketIdentityConflict
	}
	var permitted int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM project_connections WHERE repo_id=? AND role='ticket' AND version_id=?)+(SELECT COUNT(*) FROM session_execution_snapshots e JOIN session_ticket_identities t ON t.session_id=e.session_id WHERE e.repo_id=? AND e.ticket_version_id=? AND t.provider=? AND t.api_origin=? AND t.remote_id=? AND t.container=? AND t.locator_key=?)`, c.RepoID, source.ConnectionVersionID, c.RepoID, source.ConnectionVersionID, source.Ticket.Identity.Provider, source.Ticket.Identity.Origin, source.Ticket.Identity.RemoteID, source.Ticket.Locator.Container, source.Ticket.Locator.Key).Scan(&permitted)
	if err != nil {
		return err
	}
	if permitted == 0 {
		return ErrConnectionConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO doctor_command_sources(command_id,connection_version_id,publisher_id,provider,api_origin,remote_id,container,locator_key) VALUES(?,?,?,?,?,?,?,?)`, c.ID, source.ConnectionVersionID, source.PublisherID, source.Ticket.Identity.Provider, source.Ticket.Identity.Origin, source.Ticket.Identity.RemoteID, source.Ticket.Locator.Container, source.Ticket.Locator.Key)
	return err
}
