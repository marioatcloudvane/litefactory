package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// InboxOrganization is an immutable display projection of a complete snapshot.
type InboxOrganization struct {
	Version        int                    `json:"version"`
	Layout         string                 `json:"layout"`
	Mode           string                 `json:"mode"`
	FallbackReason string                 `json:"fallback_reason,omitempty"`
	Tickets        []InboxOrganizedTicket `json:"tickets"`
}

// InboxOrganizedTicket retains all request identities for one qualified ticket.
type InboxOrganizedTicket struct {
	Key             string   `json:"key"`
	DecisionKeys    []string `json:"decision_keys"`
	Summary         string   `json:"summary"`
	PromotionReason string   `json:"promotion_reason,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids,omitempty"`
}

// ReadCompleteInboxSnapshot returns every entry, refusing any lost source access.
func (db *DB) ReadCompleteInboxSnapshot(ctx context.Context, scope InboxScope, id string, now time.Time) (InboxPage, error) {
	out := InboxPage{Next: -1}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = readCompleteInbox(ctx, tx, scope, id, now)
		return err
	})
	return out, err
}

func readCompleteInbox(ctx context.Context, q inboxQuery, scope InboxScope, id string, now time.Time) (InboxPage, error) {
	out := InboxPage{Next: -1}
	if !inboxScopeValid(scope) {
		return out, ErrInboxExpired
	}
	var raw string
	err := q.QueryRowContext(ctx, `SELECT metadata FROM owner_inbox_snapshots WHERE id=? AND owner=? AND transport=? AND conversation=? AND expires_at>?`, id, scope.Owner, scope.Transport, scope.Conversation, FormatTime(now)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrInboxExpired
	}
	if err != nil {
		return out, err
	}
	if err = json.Unmarshal([]byte(raw), &out.Snapshot); err != nil {
		return out, err
	}
	rows, err := q.QueryContext(ctx, `SELECT content FROM owner_inbox_entries WHERE snapshot_id=? ORDER BY ordinal`, id)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var d InboxDecision
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal([]byte(raw), &d); err != nil {
			break
		}
		out.Entries = append(out.Entries, d)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return out, err
	}
	authorized := map[int64]bool{}
	for _, d := range out.Entries {
		if authorized[d.ConnectionID] {
			continue
		}
		v, e := currentConnection(ctx, q, d.RepoID, TicketConnection)
		if e != nil || !v.Enabled || v.ID != d.ConnectionID || !inboxSourceAllowed(ctx, q, v.ID) {
			return InboxPage{}, ErrInboxExpired
		}
		authorized[d.ConnectionID] = true
	}
	for i, coverage := range out.Snapshot.Coverage {
		if authorized[coverage.ConnectionID] {
			continue
		}
		v, err := currentConnection(ctx, q, coverage.RepoID, TicketConnection)
		if err != nil || !v.Enabled || v.ID != coverage.ConnectionID || !inboxSourceAllowed(ctx, q, v.ID) {
			out.Snapshot.Coverage[i].Status = "access unavailable or configuration changed"
		}
	}
	return out, nil
}

// ReadInboxOrganization checks current access before returning saved display text.
func (db *DB) ReadInboxOrganization(ctx context.Context, scope InboxScope, id string, version int, now time.Time) (InboxOrganization, error) {
	var out InboxOrganization
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		// An absent organization must leave independent factual browsing available.
		// Read display bytes only after the saved view's current access is checked.
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT content FROM owner_inbox_organizations WHERE snapshot_id=? AND version=?`, id, version).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := readCompleteInbox(ctx, tx, scope, id, now); err != nil {
			return err
		}
		return json.Unmarshal([]byte(raw), &out)
	})
	return out, err
}

// FinalizeInboxOrganization publishes one winner, without spanning a model call.
func (db *DB) FinalizeInboxOrganization(ctx context.Context, scope InboxScope, id string, in InboxOrganization, now time.Time) (InboxOrganization, error) {
	var out InboxOrganization
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		page, err := readCompleteInbox(ctx, tx, scope, id, now)
		if err != nil {
			return err
		}
		if in.Version < 1 {
			return fmt.Errorf("invalid organization version")
		}
		var raw string
		err = tx.QueryRowContext(ctx, `SELECT content FROM owner_inbox_organizations WHERE snapshot_id=? AND version=?`, id, in.Version).Scan(&raw)
		if err == nil {
			return json.Unmarshal([]byte(raw), &out)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err = validateInboxOrganizationMembership(page.Entries, in); err != nil {
			return err
		}
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO owner_inbox_organizations(snapshot_id,version,content) VALUES(?,?,?)`, id, in.Version, string(b)); err != nil {
			return err
		}
		return json.Unmarshal(b, &out)
	})
	return out, err
}

func validateInboxOrganizationMembership(entries []InboxDecision, in InboxOrganization) error {
	if in.Mode != "organized" && in.Mode != "fallback" {
		return fmt.Errorf("invalid organization mode")
	}
	if in.Mode == "fallback" && in.FallbackReason == "" {
		return fmt.Errorf("fallback needs a reason")
	}
	expected := map[string]string{}
	tickets := map[string]bool{}
	for _, d := range entries {
		expected[d.Key] = inboxTicketIdentity(d)
		tickets[inboxTicketIdentity(d)] = true
	}
	seen := map[string]bool{}
	seenTickets := map[string]bool{}
	for _, t := range in.Tickets {
		if !tickets[t.Key] || seenTickets[t.Key] || len(t.DecisionKeys) == 0 {
			return fmt.Errorf("invalid organization ticket membership")
		}
		seenTickets[t.Key] = true
		for _, key := range t.DecisionKeys {
			if expected[key] != t.Key || seen[key] {
				return fmt.Errorf("invalid organization request membership")
			}
			seen[key] = true
		}
	}
	if len(seen) != len(expected) || len(seenTickets) != len(tickets) {
		return fmt.Errorf("incomplete organization membership")
	}
	return nil
}

// ReadInboxNavigation reads an existing view without allowing lost access to
// hide other sources. Ticket positions and entry ordinals never shift. Saved
// agent text is discarded across the whole view when any source loses access.
func (db *DB) ReadInboxNavigation(ctx context.Context, scope InboxScope, id string, version int, now time.Time) (InboxPage, InboxOrganization, error) {
	page := InboxPage{Next: -1}
	var organization InboxOrganization
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if !inboxScopeValid(scope) {
			return ErrInboxExpired
		}
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT metadata FROM owner_inbox_snapshots WHERE id=? AND owner=? AND transport=? AND conversation=? AND expires_at>?`, id, scope.Owner, scope.Transport, scope.Conversation, FormatTime(now)).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInboxExpired
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(raw), &page.Snapshot); err != nil {
			return err
		}
		// Factual continuations have no organized view and must keep using
		// bounded ordinal reads rather than loading their entire snapshot here.
		err = tx.QueryRowContext(ctx, `SELECT content FROM owner_inbox_organizations WHERE snapshot_id=? AND version=?`, id, version).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(raw), &organization); err != nil {
			return err
		}
		authorized := map[int64]bool{}
		lostAccess := false
		for i, c := range page.Snapshot.Coverage {
			v, e := currentConnection(ctx, tx, c.RepoID, TicketConnection)
			allowed := e == nil && v.Enabled && v.ID == c.ConnectionID && inboxSourceAllowed(ctx, tx, v.ID)
			authorized[c.ConnectionID] = allowed
			if !allowed {
				page.Snapshot.Coverage[i].Status = "access unavailable or configuration changed"
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT content FROM owner_inbox_entries WHERE snapshot_id=? ORDER BY ordinal`, id)
		if err != nil {
			return err
		}
		surviving := map[string]InboxDecision{}
		for rows.Next() {
			var d InboxDecision
			if err = rows.Scan(&raw); err != nil {
				break
			}
			if err = json.Unmarshal([]byte(raw), &d); err != nil {
				break
			}
			if authorized[d.ConnectionID] {
				page.Entries = append(page.Entries, d)
				surviving[d.Key] = d
			} else {
				lostAccess = true
			}
		}
		err = errors.Join(err, rows.Err(), rows.Close())
		if err != nil {
			return err
		}
		for i, saved := range organization.Tickets {
			keys := make([]string, len(saved.DecisionKeys))
			var facts []string
			for ordinal, key := range saved.DecisionKeys {
				if d, ok := surviving[key]; ok {
					keys[ordinal] = key
					facts = append(facts, d.Summary)
				}
			}
			if len(facts) == 0 {
				organization.Tickets[i] = InboxOrganizedTicket{}
				continue
			}
			organization.Tickets[i].DecisionKeys = keys
			if lostAccess {
				organization.Tickets[i].Summary = strings.Join(facts, "\n")
				organization.Tickets[i].PromotionReason = ""
				organization.Tickets[i].EvidenceIDs = nil
			}
		}
		if lostAccess {
			organization.Mode = "fallback"
			organization.FallbackReason = "Source access changed; showing authorized recorded facts in the saved positions."
		}
		return nil
	})
	if err != nil && !errors.Is(err, ErrNotFound) {
		return InboxPage{}, InboxOrganization{}, err
	}
	return page, organization, err
}
