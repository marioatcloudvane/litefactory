package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ExecutionSnapshot retains the admitted destinations and publishing actor.
// CodeTarget is projected from the immutable retained code connection.
type ExecutionSnapshot struct {
	SessionID, RepoID, BindingID, CodeVersionID, TicketVersionID int64
	ActorID                                                      int64
	CodeActorID                                                  int64
	CodeActorLogin                                               string
	CodeRemoteID, TicketRemoteID                                 string
	ActorLogin, NamingSuffix                                     string
	CodeTarget                                                   Repo
}

const snapshotColumns = `session_id,repo_id,binding_id,code_version_id,ticket_version_id,actor_id,actor_login,naming_suffix,code_remote_id,ticket_remote_id,code_actor_id,code_actor_login`

func scanSnapshot(row scanner) (ExecutionSnapshot, error) {
	var s ExecutionSnapshot
	err := row.Scan(&s.SessionID, &s.RepoID, &s.BindingID, &s.CodeVersionID, &s.TicketVersionID, &s.ActorID, &s.ActorLogin, &s.NamingSuffix, &s.CodeRemoteID, &s.TicketRemoteID, &s.CodeActorID, &s.CodeActorLogin)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}
func targetFromVersion(v ConnectionVersion) Repo {
	return Repo{ID: v.RepoID, Path: v.ClonePath, Forge: string(v.Provider), Owner: splitDestination(v.Destination, 0), Name: splitDestination(v.Destination, 1), RemoteURL: v.RemoteURL, BaseBranch: v.BaseBranch, FeaturePrefix: v.FeaturePrefix, WorktreeRoot: v.WorktreeRoot}
}
func splitDestination(s string, index int) string {
	for i, c := range s {
		if c == '/' {
			if index == 0 {
				return s[:i]
			}
			return s[i+1:]
		}
	}
	return ""
}

// GetExecutionSnapshot returns retained execution targets, never current settings.
func (db *DB) GetExecutionSnapshot(ctx context.Context, sessionID int64) (ExecutionSnapshot, error) {
	s, err := scanSnapshot(db.sql.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, sessionID))
	if err != nil {
		return s, err
	}
	v, err := db.GetConnectionVersion(ctx, s.CodeVersionID)
	if err != nil {
		return s, err
	}
	s.CodeTarget = targetFromVersion(v)
	return s, nil
}

// GetSessionByTicket resolves the immutable qualified ticket rather than its display number.
func (db *DB) GetSessionByTicket(ctx context.Context, id ticket.Identity) (Session, error) {
	if err := id.Validate(); err != nil {
		return Session{}, err
	}
	return scanSession(db.sql.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=(SELECT session_id FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?)`, id.Provider, id.Origin, id.RemoteID))
}
func attachSnapshot(ctx context.Context, tx *sql.Tx, s ExecutionSnapshot) error {
	if s.ActorID <= 0 {
		return errors.New("store: verified publishing actor required")
	}
	session, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=?`, s.SessionID))
	if err != nil {
		return err
	}
	if session.RepoID != s.RepoID {
		return ErrBindingConflict
	}
	binding, err := scanBinding(tx.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM project_ticket_bindings WHERE id=?`, s.BindingID))
	if err != nil {
		return err
	}
	if binding.RepoID != s.RepoID {
		return ErrBindingConflict
	}
	for _, p := range []struct {
		id   int64
		role ConnectionRole
	}{{s.CodeVersionID, CodeConnection}, {s.TicketVersionID, TicketConnection}} {
		v, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, p.id))
		if err != nil {
			return err
		}
		if v.RepoID != s.RepoID || v.Role != p.role {
			return ErrBindingConflict
		}
		if v.RemoteID != "" && ((p.role == CodeConnection && s.CodeRemoteID != "" && s.CodeRemoteID != v.RemoteID) || (p.role == TicketConnection && s.TicketRemoteID != "" && s.TicketRemoteID != v.RemoteID)) {
			return ErrBindingConflict
		}
		if p.role == CodeConnection && s.CodeRemoteID == "" {
			s.CodeRemoteID = v.RemoteID
		}
		if p.role == TicketConnection && s.TicketRemoteID == "" {
			s.TicketRemoteID = v.RemoteID
		}
		if p.role == TicketConnection && (v.Provider != binding.Ref.Identity.Provider || v.Origin != binding.Ref.Identity.Origin || v.Destination != binding.Ref.Locator.Container) {
			return ErrBindingConflict
		}
	}
	old, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, s.SessionID))
	if err == nil {
		if old.SessionID != s.SessionID || old.RepoID != s.RepoID || old.BindingID != s.BindingID || old.CodeVersionID != s.CodeVersionID || old.TicketVersionID != s.TicketVersionID || old.ActorID != s.ActorID || old.ActorLogin != s.ActorLogin || old.NamingSuffix != s.NamingSuffix || old.CodeRemoteID != s.CodeRemoteID || old.TicketRemoteID != s.TicketRemoteID || old.CodeActorID != s.CodeActorID || old.CodeActorLogin != s.CodeActorLogin {
			return ErrBindingConflict
		}
		return nil
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	identity, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, s.SessionID))
	switch {
	case err == nil:
		ref, refErr := identity.Ref()
		if refErr != nil || ref != binding.Ref {
			return ErrTicketIdentityConflict
		}
	case errors.Is(err, ErrNotFound):
		_, err = tx.ExecContext(ctx, `INSERT INTO session_ticket_identities(session_id,task_ref,provider,api_origin,container,locator_key,remote_id) VALUES(?,?,?,?,?,?,?)`, s.SessionID, session.TaskRef, binding.Ref.Identity.Provider, binding.Ref.Identity.Origin, binding.Ref.Locator.Container, binding.Ref.Locator.Key, binding.Ref.Identity.RemoteID)
		if err != nil {
			return ErrTicketIdentityConflict
		}
	default:
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO session_execution_snapshots(session_id,repo_id,binding_id,code_version_id,ticket_version_id,actor_id,actor_login,naming_suffix,created_at,code_remote_id,ticket_remote_id,code_actor_id,code_actor_login) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, s.SessionID, s.RepoID, s.BindingID, s.CodeVersionID, s.TicketVersionID, s.ActorID, s.ActorLogin, s.NamingSuffix, FormatTime(nowUTC()), s.CodeRemoteID, s.TicketRemoteID, s.CodeActorID, s.CodeActorLogin)
	return err
}

// AttachExecutionSnapshot reconciles saved work using explicitly retained versions.
// It never replaces an existing snapshot or validates against mutable current pointers.
func (db *DB) AttachExecutionSnapshot(ctx context.Context, s ExecutionSnapshot) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return attachSnapshot(ctx, tx, s) })
}

// AdmitSession checks current revisions and enabled ownership in the same
// transaction that creates the session and pins its execution context.
func (db *DB) AdmitSession(ctx context.Context, repo Repo, ref ticket.Ref, codeRevision, ticketRevision, actorID int64, actorLogin, taskRef string, remoteIDs ...string) (Session, error) {
	return db.admitSession(ctx, repo, ref, codeRevision, ticketRevision, actorID, actorLogin, taskRef, 0, PublishingActor{}, remoteIDs...)
}

// AdmitSessionWithApprover pins the observed authority revision in the session admission transaction.
func (db *DB) AdmitSessionWithApprover(ctx context.Context, repo Repo, ref ticket.Ref, codeRevision, ticketRevision, actorID int64, actorLogin, taskRef string, authorityRevision int64, remoteIDs ...string) (Session, error) {
	return db.admitSession(ctx, repo, ref, codeRevision, ticketRevision, actorID, actorLogin, taskRef, authorityRevision, PublishingActor{}, remoteIDs...)
}

// PublishingActor is verified immutable publishing evidence, separate from ticket authority.
type PublishingActor struct {
	ID    int64
	Login string
}

// AdmitSessionWithPublishers retains both publishers in the admission transaction,
// so interruption cannot leave a Linear session dependent on a later credential.
func (db *DB) AdmitSessionWithPublishers(ctx context.Context, repo Repo, ref ticket.Ref, codeRevision, ticketRevision int64, publisher, codePublisher PublishingActor, taskRef string, authorityRevision int64, remoteIDs ...string) (Session, error) {
	if codePublisher.ID <= 0 || codePublisher.Login == "" {
		return Session{}, errors.New("store: verified code publisher required")
	}
	return db.admitSession(ctx, repo, ref, codeRevision, ticketRevision, publisher.ID, publisher.Login, taskRef, authorityRevision, codePublisher, remoteIDs...)
}

func (db *DB) admitSession(ctx context.Context, repo Repo, ref ticket.Ref, codeRevision, ticketRevision, actorID int64, actorLogin, taskRef string, authorityRevision int64, codePublisher PublishingActor, remoteIDs ...string) (Session, error) {
	if err := ref.Validate(); err != nil {
		return Session{}, err
	}
	if taskRef == "" {
		return Session{}, errors.New("store: admission task reference required")
	}
	var out Session
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if authorityRevision > 0 {
			a, e := currentApprover(ctx, tx, repo.ID)
			if e != nil {
				return e
			}
			if a.Revision != authorityRevision {
				return ErrApproverConflict
			}
			if a.Actor.Provider != ref.Identity.Provider || a.Actor.Origin != ref.Identity.Origin {
				return ErrApproverUnauthorized
			}
		}

		existing, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=(SELECT session_id FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?)`, ref.Identity.Provider, ref.Identity.Origin, ref.Identity.RemoteID))
		if err == nil {
			if existing.RepoID != repo.ID {
				return ErrBindingConflict
			}
			retained, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, existing.ID))
			if err != nil {
				return err
			}
			if retained.Locator != ref.Locator {
				return ErrTicketIdentityConflict
			}
			if _, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, existing.ID)); err != nil {
				return err
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		var status string
		err = tx.QueryRowContext(ctx, `SELECT status FROM project_connection_imports WHERE repo_id=?`, repo.ID).Scan(&status)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if status == "conflict" {
			return ErrTicketIdentityConflict
		}
		code, err := currentConnection(ctx, tx, repo.ID, CodeConnection)
		if err != nil {
			return err
		}
		tickets, err := currentConnection(ctx, tx, repo.ID, TicketConnection)
		if err != nil {
			return err
		}
		if code.Revision != codeRevision || tickets.Revision != ticketRevision {
			return ErrConnectionConflict
		}
		if !code.Enabled || !tickets.Enabled {
			return ErrConnectionDisabled
		}
		binding, err := getBinding(ctx, tx, ref.Identity)
		if err != nil {
			return err
		}
		if !binding.Enabled {
			return ErrConnectionDisabled
		}
		if binding.RepoID != repo.ID || binding.Ref != ref {
			return ErrBindingConflict
		}
		verifiedTicketID := ""
		if len(remoteIDs) > 1 {
			verifiedTicketID = remoteIDs[1]
		}
		if err := checkTicketBindingSource(ctx, tx, binding, tickets, verifiedTicketID); err != nil {
			return err
		}
		number, err := sessionIssueNumber(ctx, tx, ref)
		if err != nil {
			return err
		}
		var collision int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE (repo_id=? AND issue_number=?) OR task_ref=?`, repo.ID, number, taskRef).Scan(&collision); err != nil {
			return err
		}
		suffix := ""
		if collision > 0 {
			suffix = naming.QualifiedTaskSuffix(ref.Identity)
			if _, _, qualified := strings.Cut(taskRef, "~"); !qualified {
				taskRef += "~" + suffix
			}
		}
		now := FormatTime(nowUTC())
		out, err = scanSession(tx.QueryRowContext(ctx, `INSERT INTO sessions(repo_id,issue_number,task_ref,state,created_at,updated_at) VALUES(?,?,?,?,?,?) RETURNING `+sessionColumns, repo.ID, number, taskRef, SessionIdle, now, now))
		if err != nil {
			return err
		}
		codeRemoteID, ticketRemoteID := code.RemoteID, tickets.RemoteID
		if len(remoteIDs) > 0 {
			codeRemoteID = remoteIDs[0]
		}
		if len(remoteIDs) > 1 {
			ticketRemoteID = remoteIDs[1]
		}
		if err := attachSnapshot(ctx, tx, ExecutionSnapshot{SessionID: out.ID, RepoID: repo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: actorID, ActorLogin: actorLogin, NamingSuffix: suffix, CodeRemoteID: codeRemoteID, TicketRemoteID: ticketRemoteID, CodeActorID: codePublisher.ID, CodeActorLogin: codePublisher.Login}); err != nil {
			return err
		}
		if codePublisher.ID > 0 {
			_, err := db.ensureDeliveryCodeAuthority(ctx, tx, out.ID)
			return err
		}
		return nil
	})
	return out, err
}

// ListRepoSessions reads all saved work for reconciliation, including idle sessions.
func (db *DB) ListRepoSessions(ctx context.Context, repoID int64) ([]Session, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE repo_id=? ORDER BY id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// GetSessionByTicketLocator uses an explicitly qualified saved address. It
// cannot collapse matching display numbers from different origins or containers.
func (db *DB) GetSessionByTicketLocator(ctx context.Context, l ticket.Locator) (Session, error) {
	if err := l.Validate(); err != nil {
		return Session{}, err
	}
	return scanSession(db.sql.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=(SELECT session_id FROM session_ticket_identities WHERE provider=? AND api_origin=? AND container=? AND locator_key=?)`, l.Provider, l.Origin, l.Container, l.Key))
}

// ListSessionReportDeliveries includes completed immutable publication evidence.
func (db *DB) ListSessionReportDeliveries(ctx context.Context, sessionID int64) ([]ReportDelivery, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE session_id=? ORDER BY delivery_key`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReportDelivery
	for rows.Next() {
		d, err := scanReportDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AdmitManagedChild admits a verified child through the parent's immutable
// execution context. Current connection pointers and enabled states cannot
// redirect work within an already admitted managed feature.
func (db *DB) AdmitManagedChild(ctx context.Context, parentID int64, ref ticket.Ref, taskRef string) (Session, error) {
	if err := ref.Validate(); err != nil {
		return Session{}, err
	}
	if taskRef == "" {
		return Session{}, errors.New("store: child task reference required")
	}
	var out Session
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		parent, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, parentID))
		if err != nil {
			return err
		}
		tickets, err := scanConnection(tx.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM project_connection_versions WHERE id=?`, parent.TicketVersionID))
		if err != nil {
			return err
		}
		if ref.Identity.Provider != tickets.Provider || ref.Identity.Origin != tickets.Origin || ref.Locator.Container != tickets.Destination {
			return ErrTicketIdentityConflict
		}
		existing, err := scanSession(tx.QueryRowContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE id=(SELECT session_id FROM session_ticket_identities WHERE provider=? AND api_origin=? AND remote_id=?)`, ref.Identity.Provider, ref.Identity.Origin, ref.Identity.RemoteID))
		if err == nil {
			snapshot, err := scanSnapshot(tx.QueryRowContext(ctx, `SELECT `+snapshotColumns+` FROM session_execution_snapshots WHERE session_id=?`, existing.ID))
			if err != nil {
				return err
			}
			// A child discovery admitted first may carry another code version of
			// the same source. It is adopted only when a non-empty code remote ID
			// proves the same code repository and actor, remotes and the ticket
			// source proofs below match the parent's.
			if snapshot.CodeVersionID != parent.CodeVersionID && snapshot.CodeRemoteID == "" {
				return ErrBindingConflict
			}
			if snapshot.RepoID != parent.RepoID || snapshot.ActorID != parent.ActorID || snapshot.CodeActorID != parent.CodeActorID || snapshot.CodeRemoteID != parent.CodeRemoteID || snapshot.TicketRemoteID != parent.TicketRemoteID {
				return ErrBindingConflict
			}
			mapping, err := scanSessionTicket(tx.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, existing.ID))
			if err != nil {
				return err
			}
			binding, err := getBinding(ctx, tx, ref.Identity)
			if err != nil {
				return err
			}
			if mapping.Locator != ref.Locator || mapping.RemoteID != ref.Identity.RemoteID || binding.ID != snapshot.BindingID || binding.RepoID != parent.RepoID || binding.Ref != ref {
				return ErrBindingConflict
			}
			if err := checkTicketBindingSource(ctx, tx, binding, tickets, parent.TicketRemoteID); err != nil {
				return err
			}
			// Prove the saved execution version as well as the binding version.
			// Neither record may substitute for the other's retained evidence.
			binding.ConnectionVersionID = snapshot.TicketVersionID
			if err := checkTicketBindingSource(ctx, tx, binding, tickets, parent.TicketRemoteID); err != nil {
				return err
			}
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		binding, err := getBinding(ctx, tx, ref.Identity)
		if errors.Is(err, ErrNotFound) {
			binding, err = addBinding(ctx, tx, TicketBinding{RepoID: parent.RepoID, ConnectionVersionID: parent.TicketVersionID, Ref: ref, Enabled: true, Provenance: "managed-child"})
		}
		if err != nil {
			return err
		}
		if binding.RepoID != parent.RepoID || binding.Ref != ref {
			return ErrBindingConflict
		}
		number, err := sessionIssueNumber(ctx, tx, ref)
		if err != nil {
			return err
		}
		var collision int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sessions WHERE (repo_id=? AND issue_number=?) OR task_ref=?`, parent.RepoID, number, taskRef).Scan(&collision); err != nil {
			return err
		}
		suffix := ""
		if collision > 0 {
			suffix = naming.QualifiedTaskSuffix(ref.Identity)
			if _, _, qualified := strings.Cut(taskRef, "~"); !qualified {
				taskRef += "~" + suffix
			}
		}
		now := FormatTime(nowUTC())
		out, err = scanSession(tx.QueryRowContext(ctx, `INSERT INTO sessions(repo_id,issue_number,task_ref,state,created_at,updated_at) VALUES(?,?,?,?,?,?) RETURNING `+sessionColumns, parent.RepoID, number, taskRef, SessionIdle, now, now))
		if err != nil {
			return err
		}
		return attachSnapshot(ctx, tx, ExecutionSnapshot{SessionID: out.ID, RepoID: parent.RepoID, BindingID: binding.ID, CodeVersionID: parent.CodeVersionID, TicketVersionID: parent.TicketVersionID, ActorID: parent.ActorID, ActorLogin: parent.ActorLogin, NamingSuffix: suffix, CodeRemoteID: parent.CodeRemoteID, TicketRemoteID: parent.TicketRemoteID, CodeActorID: parent.CodeActorID, CodeActorLogin: parent.CodeActorLogin})
	})
	return out, err
}
