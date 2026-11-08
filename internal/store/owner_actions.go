package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// OwnerActionPrincipal is trusted authenticated provenance, not a tool argument.
type OwnerActionPrincipal struct {
	OwnerID         string `json:"owner_id"`
	OwnerGeneration int64  `json:"owner_generation"`
	Transport       string `json:"transport"`
	ClientID        string `json:"client_id,omitempty"`
	GrantID         string `json:"grant_id,omitempty"`
	ChannelID       string `json:"channel_id,omitempty"`
	DiscordOwnerID  string `json:"discord_owner_id,omitempty"`
}

// OwnerAction is durable policy and presentation state over the existing journal.
type OwnerAction struct {
	ID                                                                                    string
	PolicyVersion                                                                         int
	Principal                                                                             OwnerActionPrincipal
	RequestKey, RequestDigest, Facts, Payload, Digest                                     string
	RepositoryID                                                                          int64
	RepositoryRef, Scope, Tier, State, Reason                                             string
	CreatedAt, AvailableAt, ConfirmNotBefore, ExpiresAt, CompletedAt                      time.Time
	ExecutionID, Result, ProposalMessage, ApprovalChannel, ApprovalMessage, DeliveryState string
	DeliveryRetryAt                                                                       time.Time
	DeliveryAttempts                                                                      int
}

// OwnerActionAttempt retains a timed decision and authenticated evidence.
type OwnerActionAttempt struct {
	ID                                           int64     `json:"id"`
	ReceivedAt                                   time.Time `json:"received_at"`
	Method, Principal, Decision, State, Evidence string
}

const ownerActionColumns = `id,policy_version,owner_id,owner_generation,transport,client_id,grant_id,channel_id,discord_owner_id,request_key,request_digest,facts,payload,digest,repository_id,repository_ref,scope,tier,state,reason,created_at,available_at,confirm_not_before,expires_at,completed_at,execution_id,result,proposal_message,approval_channel,approval_message,delivery_state,delivery_retry_at,delivery_attempts`

func scanOwnerAction(row scanner) (OwnerAction, error) {
	var p OwnerAction
	var created, available, notBefore, expires, completed, retry string
	err := row.Scan(&p.ID, &p.PolicyVersion, &p.Principal.OwnerID, &p.Principal.OwnerGeneration, &p.Principal.Transport, &p.Principal.ClientID, &p.Principal.GrantID, &p.Principal.ChannelID, &p.Principal.DiscordOwnerID, &p.RequestKey, &p.RequestDigest, &p.Facts, &p.Payload, &p.Digest, &p.RepositoryID, &p.RepositoryRef, &p.Scope, &p.Tier, &p.State, &p.Reason, &created, &available, &notBefore, &expires, &completed, &p.ExecutionID, &p.Result, &p.ProposalMessage, &p.ApprovalChannel, &p.ApprovalMessage, &p.DeliveryState, &retry, &p.DeliveryAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	for _, v := range []struct {
		raw string
		out *time.Time
	}{{created, &p.CreatedAt}, {available, &p.AvailableAt}, {notBefore, &p.ConfirmNotBefore}, {expires, &p.ExpiresAt}, {completed, &p.CompletedAt}, {retry, &p.DeliveryRetryAt}} {
		if v.raw != "" {
			parsed, e := time.Parse(time.RFC3339Nano, v.raw)
			if e != nil {
				return p, e
			}
			*v.out = parsed
		}
	}
	return p, nil
}

func ownerActionTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

// ReadOwnerAction is internal persistence access. Adapters use the authenticated service.
func (db *DB) ReadOwnerAction(ctx context.Context, id string) (OwnerAction, error) {
	return scanOwnerAction(db.sql.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
}

// OwnerActionByRequest preserves response-loss idempotency without moving clocks.
func (db *DB) OwnerActionByRequest(ctx context.Context, p OwnerActionPrincipal, key string) (OwnerAction, error) {
	return scanOwnerAction(db.sql.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE owner_id=? AND owner_generation=? AND transport=? AND client_id=? AND grant_id=? AND request_key=?`, p.OwnerID, p.OwnerGeneration, p.Transport, p.ClientID, p.GrantID, key))
}

// CreateOwnerAction freezes the reviewed facts under current durable authority.
func (db *DB) CreateOwnerAction(ctx context.Context, p OwnerAction) error {
	if p.ID == "" || p.RequestKey == "" || p.Digest == "" || !json.Valid([]byte(p.Facts)) || !json.Valid([]byte(p.Payload)) || p.CreatedAt.IsZero() || !p.ExpiresAt.Equal(p.CreatedAt.Add(15*time.Minute)) || (p.Tier != "conversational" && p.Tier != "binding") {
		return errors.New("invalid owner action proposal")
	}
	return db.withTx(ctx, func(tx *sql.Tx) error {
		if err := ownerActionAuthority(ctx, tx, p.Principal, p, p.CreatedAt); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO owner_action_proposals(`+ownerActionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.PolicyVersion, p.Principal.OwnerID, p.Principal.OwnerGeneration, p.Principal.Transport, p.Principal.ClientID, p.Principal.GrantID, p.Principal.ChannelID, p.Principal.DiscordOwnerID, p.RequestKey, p.RequestDigest, p.Facts, p.Payload, p.Digest, p.RepositoryID, p.RepositoryRef, p.Scope, p.Tier, p.State, p.Reason, ownerActionTime(p.CreatedAt), ownerActionTime(p.AvailableAt), ownerActionTime(p.ConfirmNotBefore), ownerActionTime(p.ExpiresAt), "", p.ExecutionID, "", "", p.ApprovalChannel, "", "", "", 0)
		return err
	})
}

func ownerActionAuthority(ctx context.Context, tx *sql.Tx, actor OwnerActionPrincipal, p OwnerAction, at time.Time) error {
	var owner string
	var generation int64
	err := tx.QueryRowContext(ctx, `SELECT CAST(o.owner_id AS TEXT),o.generation FROM connector_owner o JOIN identities i ON i.forge='github' AND i.role='owner' AND i.user_id=o.owner_id WHERE o.singleton=1`).Scan(&owner, &generation)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectorOwner
		}
		return err
	}
	if actor.OwnerID != owner || actor.OwnerGeneration != generation || p.Principal.OwnerID != owner || p.Principal.OwnerGeneration != generation {
		return ErrConnectorOwner
	}
	if actor.Transport == "discord" {
		if actor.DiscordOwnerID == "" || actor.ChannelID == "" {
			return ErrConnectorOwner
		}
	} else if actor.Transport != "mcp" {
		return ErrConnectorOwner
	}
	// The original grant remains authoritative even when Discord supplies the tap.
	grantActor := actor
	if p.Principal.Transport == "mcp" {
		grantActor = p.Principal
	}
	if grantActor.Transport == "mcp" {
		var scope string
		err = tx.QueryRowContext(ctx, `SELECT scope FROM connector_grants WHERE id=? AND client_id=? AND owner_id=? AND generation=? AND revoked_at IS NULL AND expires_at>?`, grantActor.GrantID, grantActor.ClientID, owner, generation, FormatTime(at)).Scan(&scope)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrConnectorInvalid
		}
		if err != nil {
			return err
		}
		if p.Scope == "standards:management" && p.RepositoryID > 0 && !ConnectorScopeHas(scope, "task:discussion") && !ConnectorScopeHas(scope, "task:code-review") {
			return ErrConnectorScope
		}
		if !ConnectorScopeHas(scope, p.Scope) {
			return ErrConnectorScope
		}
		if p.RepositoryID > 0 {
			table := "connector_grant_action_repositories"
			if p.Scope == "standards:management" || p.Scope == "task:advisory" || p.Scope == "task:discussion-publish" {
				table = "connector_grant_repositories"
			}
			var count int
			query := `SELECT count(*) FROM ` + table + ` c JOIN repos r ON r.id=c.repo_id AND r.forge||':'||r.owner||'/'||r.name=c.repository_ref WHERE c.grant_id=? AND c.repo_id=? AND c.repository_ref=?`
			if err = tx.QueryRowContext(ctx, query, grantActor.GrantID, p.RepositoryID, p.RepositoryRef).Scan(&count); err != nil {
				return err
			}
			if count != 1 {
				// Standards context may use existing all-repository read consent.
				if p.Scope != "standards:management" {
					return ErrConnectorScope
				}
				var all bool
				if err = tx.QueryRowContext(ctx, `SELECT all_repositories FROM connector_grants WHERE id=?`, grantActor.GrantID).Scan(&all); err != nil {
					return err
				}
				if !all {
					return ErrConnectorScope
				}
			}
		}
	}
	if p.RepositoryID > 0 {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM repos WHERE id=? AND forge||':'||owner||'/'||name=?`, p.RepositoryID, p.RepositoryRef).Scan(&count); err != nil {
			return err
		}
		if count != 1 {
			return ErrConnectorScope
		}
	}
	return nil
}

// CheckOwnerActionAuthority rechecks current authority without admitting execution.
func (db *DB) CheckOwnerActionAuthority(ctx context.Context, actor OwnerActionPrincipal, p OwnerAction, at time.Time) error {
	return db.withTx(ctx, func(tx *sql.Tx) error { return ownerActionAuthority(ctx, tx, actor, p, at) })
}

func ownerActionPending(state string) bool {
	return state == "awaiting_confirmation" || state == "awaiting_discord"
}

func ownerActionAttempt(ctx context.Context, tx *sql.Tx, p OwnerAction, actor OwnerActionPrincipal, method, decision, evidence string, at time.Time) error {
	principal, err := json.Marshal(actor)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO owner_action_attempts(proposal_id,received_at,method,principal,decision,state,evidence) VALUES(?,?,?,?,?,?,?)`, p.ID, FormatTime(at), method, string(principal), decision, p.State, evidence)
	return err
}

// MutateOwnerAction conditionally decides a control and journals admission atomically.
// Domain target rechecks happen before this transaction; conditional domain writes
// retain the reviewed revision at the later effect boundary.
func (db *DB) MutateOwnerAction(ctx context.Context, actor OwnerActionPrincipal, id, method, evidence string, at time.Time) (OwnerAction, error) {
	var p OwnerAction
	denied := false
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=?`, id))
		if err != nil {
			return err
		}
		if actor.OwnerID != p.Principal.OwnerID {
			return ErrNotFound
		}
		if method != "reaction" && method != "cancel_discord" && method != "invalidate" && (actor.Transport != p.Principal.Transport || actor.Transport == "mcp" && (actor.ClientID != p.Principal.ClientID || actor.GrantID != p.Principal.GrantID) || actor.Transport == "discord" && (actor.ChannelID != p.Principal.ChannelID || actor.DiscordOwnerID != p.Principal.DiscordOwnerID)) {
			denied = true
			return ownerActionAttempt(ctx, tx, p, actor, method, "origin_mismatch", evidence, at)
		}
		decision := "recorded_status"
		authorityRecord := p
		if method == "cancel_discord" || method == "cancel" && actor.Transport == "discord" {
			authorityRecord.Principal = actor
			authorityRecord.RepositoryID = 0
			authorityRecord.RepositoryRef = ""
		}
		if err = ownerActionAuthority(ctx, tx, actor, authorityRecord, at); err != nil {
			denied = true
			return ownerActionAttempt(ctx, tx, p, actor, method, "authority_revoked", evidence, at)
		}
		if !ownerActionPending(p.State) {
			return ownerActionAttempt(ctx, tx, p, actor, method, decision, evidence, at)
		}
		switch {
		case !at.Before(p.ExpiresAt):
			p.State = "expired"
			p.Reason = "expired_repropose"
			p.CompletedAt = at
			decision = p.Reason
		case method == "cancel" || method == "cancel_discord":
			p.State = "cancelled"
			p.Reason = "cancelled_before_admission"
			p.CompletedAt = at
			decision = "cancelled"
		case method == "invalidate":
			p.State = "invalidated"
			p.Reason = evidence
			p.CompletedAt = at
			decision = evidence
		case method == "confirm":
			switch {
			case p.State != "awaiting_confirmation":
				decision = "awaiting_discord"
			case p.AvailableAt.IsZero():
				decision = "not_available"
			case at.Before(p.ConfirmNotBefore):
				decision = "too_early"
			case p.Tier == "binding":
				p.State = "awaiting_discord"
				p.DeliveryState = "pending"
				decision = "awaiting_discord"
			default:
				p.State = "admitted"
				decision = "admitted"
			}
		case method == "reaction":
			if p.State != "awaiting_discord" || p.DeliveryState != "delivered" || p.ApprovalMessage == "" {
				decision = "unbound_reaction"
			} else {
				p.State = "admitted"
				decision = "admitted"
			}
		default:
			return errors.New("invalid owner action control")
		}
		if p.State == "admitted" {
			var revision int64
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM chat_natural_executions WHERE transport=? AND owner_id=? AND channel_ref=?`, p.Principal.Transport, p.Principal.OwnerID, p.Principal.ChannelID).Scan(&revision); err != nil {
				return err
			}
			if err = insertNaturalExecution(ctx, tx, NaturalExecution{Identity: p.ExecutionID, Transport: p.Principal.Transport, OwnerID: p.Principal.OwnerID, ChannelRef: p.Principal.ChannelID, Revision: revision, Payload: p.Payload, CreatedAt: at}); err != nil {
				return err
			}
			if err = admitDurableWorkflowTx(ctx, tx, p, at); err != nil {
				return err
			}
		}
		p.Reason = decision
		if p.State == "cancelled" || p.State == "expired" || p.State == "invalidated" {
			p.Payload = "{}"
		}
		_, err = tx.ExecContext(ctx, `UPDATE owner_action_proposals SET state=?,reason=?,completed_at=?,delivery_state=?,payload=? WHERE id=?`, p.State, p.Reason, ownerActionTime(p.CompletedAt), p.DeliveryState, p.Payload, p.ID)
		if err != nil {
			return err
		}
		return ownerActionAttempt(ctx, tx, p, actor, method, decision, evidence, at)
	})
	if err == nil && denied {
		return OwnerAction{}, ErrConnectorInvalid
	}
	return p, err
}

// OwnerActionProvenanceValid permits only the currently claimed persisted effect.
func (db *DB) OwnerActionProvenanceValid(ctx context.Context, actor, operationKey string) (bool, error) {
	if !strings.HasPrefix(actor, "owner-action:") || actor != operationKey {
		return false, nil
	}
	var count int
	err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM owner_action_proposals p JOIN chat_natural_executions e ON e.identity=p.execution_id WHERE p.id=? AND p.execution_id=? AND p.state='executing' AND e.state='executing' AND e.owner_id=p.owner_id`, strings.TrimPrefix(actor, "owner-action:"), operationKey).Scan(&count)
	return count == 1, err
}

// ownerActionEffectAuthority fences current durable authority within a domain write.
func ownerActionEffectAuthority(ctx context.Context, tx *sql.Tx, actor string, at time.Time) error {
	if !strings.HasPrefix(actor, "owner-action:") {
		return ErrConnectorOwner
	}
	p, err := scanOwnerAction(tx.QueryRowContext(ctx, `SELECT `+ownerActionColumns+` FROM owner_action_proposals WHERE id=? AND execution_id=? AND state='executing'`, strings.TrimPrefix(actor, "owner-action:"), actor))
	if err != nil {
		return err
	}
	if err = ownerActionAuthority(ctx, tx, p.Principal, p, at); err != nil {
		return err
	}
	if err = workflowEffectAuthority(ctx, tx, p, at); err != nil {
		return err
	}
	var facts struct {
		Invocation struct {
			Command string `json:"command"`
		} `json:"invocation"`
		TargetID       string `json:"target_id"`
		TargetRevision string `json:"target_revision"`
	}
	if err = json.Unmarshal([]byte(p.Facts), &facts); err != nil {
		return err
	}
	if facts.Invocation.Command == "diagnose" {
		var session Session
		var created string
		if err = tx.QueryRowContext(ctx, `SELECT id,created_at FROM sessions WHERE task_ref=? AND repo_id=?`, facts.TargetID, p.RepositoryID).Scan(&session.ID, &created); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrDoctorFenced
			}
			return err
		}
		session.CreatedAt, err = time.Parse(time.RFC3339Nano, created)
		if err != nil {
			return err
		}
		if OwnerActionSessionRevision(session) != facts.TargetRevision {
			return ErrDoctorFenced
		}
	}
	return nil
}
