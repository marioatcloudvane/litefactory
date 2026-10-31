package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MakeOwnerActionAvailable records first confirmed availability, never extending expiry.
func (db *DB) MakeOwnerActionAvailable(ctx context.Context, id, message string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if !p.AvailableAt.IsZero() {
			return nil
		}
		if p.State != "awaiting_confirmation" || !at.Before(p.ExpiresAt) || at.Before(p.CreatedAt) {
			return ErrNotFound
		}
		if err := ownerActionAuthority(ctx, tx, p.Principal, p, at); err != nil {
			return err
		}
		if p.Principal.Transport == "discord" && message == "" {
			return errors.New("proposal delivery is unconfirmed")
		}
		_, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET available_at=?,confirm_not_before=?,proposal_message=? WHERE id=? AND available_at=''`, ownerActionTime(at), ownerActionTime(at.Add(5*time.Second)), message, id)
		return err
	})
}

// ClaimOwnerActionDelivery persists intent before an external send. A lost send
// response leaves sending and is never automatically sent again.
func (db *DB) ClaimOwnerActionDelivery(ctx context.Context, id string, at time.Time) (bool, error) {
	var claimed bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if p.State != "awaiting_discord" || p.DeliveryState != "pending" || !at.Before(p.ExpiresAt) || p.DeliveryAttempts >= 3 || (!p.DeliveryRetryAt.IsZero() && at.Before(p.DeliveryRetryAt)) {
			return nil
		}
		if err = ownerActionAuthority(ctx, tx, p.Principal, p, at); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE owner_action_proposals SET delivery_state='sending',delivery_attempts=delivery_attempts+1 WHERE id=? AND delivery_state='pending'`, id)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		claimed = n == 1
		return err
	})
	return claimed, err
}

// FinishOwnerActionDelivery binds only a confirmed provider message identity.
func (db *DB) FinishOwnerActionDelivery(ctx context.Context, id, message, reason string, at time.Time) error {
	state := "delivered"
	if message == "" {
		state = "uncertain"
	}
	r, err := db.sql.ExecContext(ctx, `UPDATE owner_action_proposals SET approval_message=?,delivery_state=?,reason=? WHERE id=? AND state='awaiting_discord' AND delivery_state='sending' AND expires_at>?`, message, state, reason, id, ownerActionTime(at))
	return chatAffected(r, err)
}

// ClaimOwnerActionExecution shares the old journal claim before effects.
func (db *DB) ClaimOwnerActionExecution(ctx context.Context, id string, at time.Time) (bool, error) {
	var claimed bool
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if p.State != "admitted" {
			return nil
		}
		if err = ownerActionAuthority(ctx, tx, p.Principal, p, at); err != nil {
			return err
		}
		r, err := tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state='executing' WHERE identity=? AND state='authorized'`, p.ExecutionID)
		if err = chatAffected(r, err); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET state='executing',reason='' WHERE id=? AND state='admitted'`, id)
		claimed = err == nil
		return err
	})
	return claimed, err
}

// FinishOwnerAction records the factual outcome and journal receipt together.
func (db *DB) FinishOwnerAction(ctx context.Context, id, state, result, reason string, at time.Time) error {
	if state != "succeeded" && state != "failed" && state != "uncertain" && state != "executing" {
		return errors.New("invalid owner action result")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if p.State != "executing" && p.State != "admitted" && p.State != "uncertain" {
			return ErrNotFound
		}
		completed := ""
		payload := p.Payload
		journalState := "executing"
		if state != "executing" {
			completed = ownerActionTime(at)
			payload = "{}"
			journalState = "finished"
		}
		if state == "uncertain" {
			journalState = "uncertain"
			payload = p.Payload
		}
		_, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET state=?,result=?,reason=?,completed_at=?,payload=? WHERE id=?`, state, result, reason, completed, payload, id)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE chat_natural_executions SET state=?,outcome=?,finished_at=?,payload=? WHERE identity=?`, journalState, result, nullableOwnerActionTime(completed), payload, p.ExecutionID)
		if err != nil {
			return err
		}
		return settleQueuedWorkflowActionTx(ctx, tx, p, state, result, at)
	})
}
func nullableOwnerActionTime(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// OwnerActionAttempts returns redacted immutable decisions in receipt order.
func (db *DB) OwnerActionAttempts(ctx context.Context, id string) ([]OwnerActionAttempt, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT id,received_at,method,principal,decision,state,evidence FROM owner_action_attempts WHERE proposal_id=? ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OwnerActionAttempt{}
	for rows.Next() {
		var a OwnerActionAttempt
		var at string
		if err = rows.Scan(&a.ID, &at, &a.Method, &a.Principal, &a.Decision, &a.State, &a.Evidence); err != nil {
			return nil, err
		}
		a.ReceivedAt, err = time.Parse(time.RFC3339Nano, at)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListOwnerActions pages by immutable creation time and ID; service authorizes each row.
func (db *DB) ListOwnerActions(ctx context.Context, owner, beforeTime, beforeID string, limit int) ([]OwnerAction, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("invalid owner action page size")
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE owner_id=? AND (?='' OR created_at<? OR (created_at=? AND id<?)) ORDER BY created_at DESC,id DESC LIMIT ?`, owner, beforeTime, beforeTime, beforeTime, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OwnerAction{}
	for rows.Next() {
		p, e := scanOwnerAction(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ReadyOwnerActions includes unresolved work for factual reconciliation, never replay.
func (db *DB) ReadyOwnerActions(ctx context.Context, limit int, at time.Time) ([]string, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("invalid owner action work limit")
	}
	var out []string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT id FROM owner_action_proposals WHERE state IN ('awaiting_confirmation','awaiting_discord','admitted','executing','uncertain') ORDER BY CASE state WHEN 'admitted' THEN 0 WHEN 'awaiting_discord' THEN 1 WHEN 'executing' THEN 2 WHEN 'awaiting_confirmation' THEN 3 ELSE 4 END,progressed_at,created_at,id LIMIT ?`, limit)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err = rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			out = append(out, id)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for _, id := range out {
			if _, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET progressed_at=? WHERE id=?`, ownerActionTime(at), id); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// PurgeOwnerActionHistory retains active and unresolved recovery records, and
// ninety days of terminal history independently of chat cleanup.
func (db *DB) PurgeOwnerActionHistory(ctx context.Context, at time.Time) error {
	cutoff := ownerActionTime(at.Add(-90 * 24 * time.Hour))
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM owner_action_proposals WHERE state IN ('succeeded','failed','cancelled','expired','invalidated') AND completed_at<>'' AND completed_at<=? AND NOT EXISTS(SELECT 1 FROM durable_workflow_requests w WHERE w.proposal_id=owner_action_proposals.id)`, cutoff); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM owner_action_policy_invalidations WHERE julianday(invalidated_at)<=julianday(?)`, cutoff)
		return err
	})
}

// ExpireOwnerAction is a clock-driven pending transition, never an execution cancellation.
func (db *DB) ExpireOwnerAction(ctx context.Context, id string, at time.Time) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_action_proposals SET state='expired',reason='expired_repropose',completed_at=?,payload='{}' WHERE id=? AND state IN ('awaiting_confirmation','awaiting_discord') AND expires_at<=?`, ownerActionTime(at), id, ownerActionTime(at))
	return err
}

// OwnerActionByApprovalMessage resolves only the factory's confirmed send binding.
func (db *DB) OwnerActionByApprovalMessage(ctx context.Context, channel, message string) (OwnerAction, error) {
	return scanOwnerAction(db.sql.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE approval_channel=? AND approval_message=? AND delivery_state='delivered'`, channel, message))
}

// RecordOwnerActionAttempt records refused gateway evidence without granting authority.
func (db *DB) RecordOwnerActionAttempt(ctx context.Context, id string, actor OwnerActionPrincipal, method, decision, evidence string, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		return ownerActionAttempt(ctx, tx, p, actor, method, decision, evidence, at)
	})
}

// OwnerActionReaderFingerprint binds a cursor to current authority and coverage.
func (db *DB) OwnerActionReaderFingerprint(ctx context.Context, p OwnerActionPrincipal, at time.Time) (string, error) {
	var fingerprint string
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var owner string
		var generation int64
		if err := tx.QueryRowContext(ctx, `SELECT CAST(owner_id AS TEXT),generation FROM connector_owner WHERE singleton=1`).Scan(&owner, &generation); err != nil {
			return err
		}
		if owner != p.OwnerID || generation != p.OwnerGeneration {
			return ErrConnectorOwner
		}
		fingerprint = owner + ":" + FormatTime(at.Truncate(24*time.Hour))
		if p.Transport != "mcp" {
			fingerprint = owner + ":discord:" + p.DiscordOwnerID + ":" + p.ChannelID
			return nil
		}
		var scope string
		if err := tx.QueryRowContext(ctx, `SELECT scope FROM connector_grants WHERE id=? AND client_id=? AND owner_id=? AND generation=? AND revoked_at IS NULL AND expires_at>?`, p.GrantID, p.ClientID, p.OwnerID, p.OwnerGeneration, FormatTime(at)).Scan(&scope); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrConnectorInvalid
			}
			return err
		}
		if !ConnectorScopeHas(scope, "repository:actions") && !ConnectorScopeHas(scope, "standards:management") {
			return ErrConnectorScope
		}
		fingerprint = p.GrantID + ":" + p.ClientID + ":" + scope
		rows, err := tx.QueryContext(ctx, `SELECT repository_ref FROM connector_grant_action_repositories WHERE grant_id=? UNION SELECT repository_ref FROM connector_grant_repositories WHERE grant_id=? ORDER BY repository_ref`, p.GrantID, p.GrantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref string
			if err = rows.Scan(&ref); err != nil {
				return err
			}
			fingerprint += "\n" + ref
		}
		return rows.Err()
	})
	return fingerprint, err
}

// StopOwnerActionDelivery makes a lost send response visible without replay.
func (db *DB) StopOwnerActionDelivery(ctx context.Context, id string) error {
	_, err := db.sql.ExecContext(ctx, `UPDATE owner_action_proposals SET delivery_state='uncertain',reason='discord_delivery_unconfirmed_no_execution' WHERE id=? AND state='awaiting_discord' AND delivery_state='sending'`, id)
	return err
}

// DeferOwnerActionDelivery retries only a known provider refusal, at most three
// attempts and within the original proposal lifetime.
func (db *DB) DeferOwnerActionDelivery(ctx context.Context, id string, retryAt, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if p.State != "awaiting_discord" || p.DeliveryState != "sending" {
			return ErrNotFound
		}
		state, reason := "pending", "discord_rate_limited"
		if !retryAt.After(at) || !retryAt.Before(p.ExpiresAt) || p.DeliveryAttempts >= 3 {
			state = "failed"
			reason = "discord_delivery_refused_no_execution"
		}
		_, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET delivery_state=?,delivery_retry_at=?,reason=? WHERE id=?`, state, ownerActionTime(retryAt), reason, id)
		return err
	})
}
