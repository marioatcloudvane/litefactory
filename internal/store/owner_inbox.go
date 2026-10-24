package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/planbrief"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// InboxScope binds saved browsing content to its authenticated conversation.
type InboxScope struct{ Owner, Transport, Conversation string }

// InboxFilter selects recorded facts before paging.
type InboxFilter struct{ Source, Project, Kind string }

// InboxEvidence binds a display-only promotion claim to recorded request facts.
type InboxEvidence struct {
	ID, Kind, DecisionKey, Source, Revision, Excerpt, Target string
}

// InboxDecision is a read projection, never workflow authority.
type InboxDecision struct {
	WaitingAt                                                                                             time.Time
	WaitingProvenance                                                                                     string
	Evidence                                                                                              []InboxEvidence
	Key, Source, Project, Ticket, WaitID, Workflow, Kind, Summary, Destination, Action, ActionUnavailable string
	Ordinal                                                                                               int
	RepoID, ConnectionID, SessionID                                                                       int64
	Locator                                                                                               ticket.Locator
	Ref                                                                                                   ticket.Ref
}

// InboxCoverage describes configured ticket coverage without credential fields.
type InboxCoverage struct {
	RepoID, ConnectionID    int64
	Source, Project, Status string
	ObservedAt              time.Time
}

// InboxSnapshot is immutable captured recorded state, not an atomic provider view.
type InboxSnapshot struct {
	ID                    string
	Scope                 InboxScope
	Filter                InboxFilter
	CapturedAt, ExpiresAt time.Time
	Coverage              []InboxCoverage
	Total                 int
}

// InboxPage uses saved ordinals; Next is -1 at the end.
type InboxPage struct {
	Snapshot     InboxSnapshot
	Entries      []InboxDecision
	Offset, Next int
}

var (
	// ErrInboxExpired requests a fresh browse after expiry or authorization loss.
	ErrInboxExpired = errors.New("inbox reference expired or unavailable; refresh the inbox")
	// ErrInboxFilter identifies unsupported recorded filter facts.
	ErrInboxFilter = errors.New("invalid inbox filter")
)

type inboxQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func inboxScopeValid(s InboxScope) bool {
	return s.Owner != "" && s.Transport != "" && s.Conversation != ""
}

// CreateInboxSnapshot captures membership and publishes all rows in one short transaction.
func (db *DB) CreateInboxSnapshot(ctx context.Context, scope InboxScope, filter InboxFilter, now time.Time) (InboxSnapshot, error) {
	return db.createInboxSnapshot(ctx, scope, filter, now, nil)
}

// CreateProtectedInboxSnapshot captures display summaries and evidence through a
// local redactor before persistence. The redactor must not perform external I/O.
func (db *DB) CreateProtectedInboxSnapshot(ctx context.Context, scope InboxScope, filter InboxFilter, now time.Time, protect func(string) (string, error)) (InboxSnapshot, error) {
	if protect == nil {
		return InboxSnapshot{}, errors.New("inbox display protection unavailable")
	}
	return db.createInboxSnapshot(ctx, scope, filter, now, protect)
}

func (db *DB) createInboxSnapshot(ctx context.Context, scope InboxScope, filter InboxFilter, now time.Time, protect func(string) (string, error)) (InboxSnapshot, error) {
	var snapshot InboxSnapshot
	if !inboxScopeValid(scope) {
		return snapshot, ErrInboxExpired
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		entries, coverage, err := projectInbox(ctx, tx)
		if err != nil {
			return err
		}
		choices := func(field func(InboxDecision) string, name string) string {
			m := map[string]bool{}
			for _, d := range entries {
				m[field(d)] = true
			}
			for _, c := range coverage {
				switch name {
				case "source":
					m[c.Source] = true
				case "project":
					m[c.Project] = true
				}
			}
			var out []string
			for v := range m {
				out = append(out, v)
			}
			sort.Strings(out)
			return strings.Join(out, ", ")
		}
		for _, f := range []struct {
			value, name string
			get         func(InboxDecision) string
		}{{filter.Source, "source", func(d InboxDecision) string { return d.Source }}, {filter.Project, "project", func(d InboxDecision) string { return d.Project }}, {filter.Kind, "kind", func(d InboxDecision) string { return d.Kind }}} {
			if f.value == "" {
				continue
			}
			valid := false
			for _, d := range entries {
				if f.get(d) == f.value {
					valid = true
				}
			}
			if f.name != "kind" {
				for _, c := range coverage {
					if (f.name == "source" && c.Source == f.value) || (f.name == "project" && c.Project == f.value) {
						valid = true
					}
				}
			}
			if !valid {
				return fmt.Errorf("%w: %s; available: %s", ErrInboxFilter, f.name, choices(f.get, f.name))
			}
		}
		filtered := entries[:0]
		for _, d := range entries {
			if (filter.Source == "" || filter.Source == d.Source) && (filter.Project == "" || filter.Project == d.Project) && (filter.Kind == "" || filter.Kind == d.Kind) {
				filtered = append(filtered, d)
			}
		}
		token := make([]byte, 16)
		if _, err = rand.Read(token); err != nil {
			return err
		}
		snapshot = InboxSnapshot{ID: hex.EncodeToString(token), Scope: scope, Filter: filter, CapturedAt: now.UTC(), ExpiresAt: now.UTC().Add(24 * time.Hour), Coverage: coverage, Total: len(filtered)}
		raw, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM owner_inbox_snapshots WHERE expires_at<=?`, FormatTime(now)); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO owner_inbox_snapshots VALUES(?,?,?,?,?,?,?)`, snapshot.ID, scope.Owner, scope.Transport, scope.Conversation, FormatTime(snapshot.CapturedAt), FormatTime(snapshot.ExpiresAt), string(raw)); err != nil {
			return err
		}
		for i, d := range filtered {
			if protect == nil {
				d.Evidence = nil
			}
			if protect != nil {
				if d.Summary, err = protect(d.Summary); err != nil {
					return err
				}
				for j := range d.Evidence {
					if d.Evidence[j].Excerpt, err = protect(d.Evidence[j].Excerpt); err != nil {
						return err
					}
				}
			}
			d.Ordinal = i
			raw, err = json.Marshal(d)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO owner_inbox_entries VALUES(?,?,?,?)`, snapshot.ID, i, d.Key, string(raw)); err != nil {
				return err
			}
		}
		return nil
	})
	return snapshot, err
}

// ReadInboxSnapshot checks scope, expiry and current ticket configuration on every read.
func (db *DB) ReadInboxSnapshot(ctx context.Context, scope InboxScope, id string, offset, limit int, now time.Time) (InboxPage, error) {
	out := InboxPage{Next: -1}
	if !inboxScopeValid(scope) || offset < 0 || limit < 1 || limit > 100 {
		return out, ErrInboxExpired
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM owner_inbox_snapshots WHERE expires_at<=?`, FormatTime(now)); err != nil {
			return err
		}
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT metadata FROM owner_inbox_snapshots WHERE id=? AND owner=? AND transport=? AND conversation=? AND expires_at>?`, id, scope.Owner, scope.Transport, scope.Conversation, FormatTime(now)).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInboxExpired
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal([]byte(raw), &out.Snapshot); err != nil {
			return err
		}
		authorized := map[int64]bool{}
		for i, c := range out.Snapshot.Coverage {
			v, e := currentConnection(ctx, tx, c.RepoID, TicketConnection)
			if e == nil && v.Enabled && v.ID == c.ConnectionID && inboxSourceAllowed(ctx, tx, v.ID) {
				authorized[c.ConnectionID] = true
			} else {
				out.Snapshot.Coverage[i].Status = "access unavailable or configuration changed"
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT content FROM owner_inbox_entries WHERE snapshot_id=? AND ordinal>=? AND ordinal<? ORDER BY ordinal`, id, offset, offset+limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			var d InboxDecision
			if err = json.Unmarshal([]byte(raw), &d); err != nil {
				return err
			}
			if authorized[d.ConnectionID] {
				out.Entries = append(out.Entries, d)
			}
		}
		out.Offset = offset
		if offset+limit < out.Snapshot.Total {
			out.Next = offset + limit
		}
		return rows.Err()
	})
	return out, err
}

// CurrentInboxDecision refreshes exact qualified wait membership without provider I/O.
func (db *DB) CurrentInboxDecision(ctx context.Context, key string) (InboxDecision, error) {
	var out InboxDecision
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		entries, _, err := projectInbox(ctx, tx)
		if err != nil {
			return err
		}
		for _, d := range entries {
			if d.Key == key {
				out = d
				return nil
			}
		}
		return ErrNotFound
	})
	return out, err
}

func projectInbox(ctx context.Context, q inboxQuery) ([]InboxDecision, []InboxCoverage, error) {
	var coverage []InboxCoverage
	connections := map[int64]ConnectionVersion{}
	rows, err := q.QueryContext(ctx, `SELECT r.id,COALESCE(v.id,0),COALESCE(v.provider,'unknown'),COALESCE(v.destination,r.owner||'/'||r.name),COALESCE(v.enabled,0),COALESCE(o.status,'unknown'),COALESCE(o.checked_at,'') FROM repos r LEFT JOIN project_connections c ON c.repo_id=r.id AND c.role='ticket' LEFT JOIN project_connection_versions v ON v.id=c.version_id LEFT JOIN project_connection_observations o ON o.version_id=v.id ORDER BY r.id`)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var c InboxCoverage
		var enabled bool
		var at string
		if err = rows.Scan(&c.RepoID, &c.ConnectionID, &c.Source, &c.Project, &enabled, &c.Status, &at); err != nil {
			return nil, nil, errors.Join(err, rows.Close())
		}
		if c.ConnectionID == 0 {
			c.Status = "ticket connection missing"
		} else if !enabled {
			c.Status = "disabled"
		}
		if at != "" {
			c.ObservedAt, _ = ParseTime(at)
		}
		coverage = append(coverage, c)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, errors.Join(err, rows.Close())
	}
	if err = rows.Close(); err != nil {
		return nil, nil, err
	}
	for i, c := range coverage {
		if c.ConnectionID > 0 {
			v, e := currentConnection(ctx, q, c.RepoID, TicketConnection)
			if e != nil {
				coverage[i].Status = "ticket configuration unreadable"
				continue
			}
			connections[c.RepoID] = v
		}
	}

	for i := range coverage {
		c := &coverage[i]
		var observed sql.NullString
		var owed bool
		e := q.QueryRowContext(ctx, `SELECT last_poll_finished_at,poll_owed FROM repo_discovery_status WHERE repo_id=?`, c.RepoID).Scan(&observed, &owed)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, nil, e
		}
		if observed.Valid {
			at, pe := ParseTime(observed.String)
			if pe == nil && at.After(c.ObservedAt) {
				c.ObservedAt = at
			}
			if owed {
				c.Status += "; discovery refresh owed"
			}
		} else {
			c.Status += "; discovery observation unknown"
		}
		var paused bool
		e = q.QueryRowContext(ctx, `SELECT discovery_paused FROM daemon_listener_status WHERE id=1`).Scan(&paused)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, nil, e
		}
		if paused {
			c.Status += "; discovery refresh deferred"
		}
	}
	rows, err = q.QueryContext(ctx, `SELECT `+sessionColumns+` FROM sessions WHERE task_ref NOT IN (SELECT parent_task FROM managed_parent_closures) ORDER BY id`)
	if err != nil {
		return nil, nil, err
	}
	var sessions []Session
	for rows.Next() {
		s, e := scanSession(rows)
		if e != nil {
			return nil, nil, errors.Join(e, rows.Close())
		}
		sessions = append(sessions, s)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, errors.Join(err, rows.Close())
	}
	if err = rows.Close(); err != nil {
		return nil, nil, err
	}
	entries := map[string]InboxDecision{}
	for _, s := range sessions {
		v, ok := connections[s.RepoID]
		if !ok || !v.Enabled || !inboxSourceAllowed(ctx, q, v.ID) {
			continue
		}
		binding, e := scanSessionTicket(q.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=?`, s.ID))
		if errors.Is(e, ErrNotFound) {
			if v.Provider != ticket.GitHub {
				continue
			}
			if s.TaskRef != string(v.Provider)+":"+v.Destination+"#"+strconv.Itoa(s.IssueNumber) {
				continue
			}
			binding = SessionTicket{SessionID: s.ID, TaskRef: s.TaskRef, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: v.Destination, Key: strconv.Itoa(s.IssueNumber)}}
		} else if e != nil {
			return nil, nil, e
		}
		if binding.Locator.Provider != v.Provider || binding.Locator.Origin != v.Origin || binding.Locator.Container != v.Destination {
			continue
		}
		base := InboxDecision{Source: string(v.Provider), Project: v.Destination, Ticket: s.TaskRef, RepoID: s.RepoID, ConnectionID: v.ID, SessionID: s.ID, Locator: binding.Locator}
		base.Ref = ticket.Ref{Locator: binding.Locator, Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: binding.RemoteID}}
		base.Destination = s.TaskRef
		add := func(d InboxDecision) {
			d.Key = inboxTicketIdentity(d) + "|" + d.WaitID
			for _, line := range strings.Split(d.Summary, "\n") {
				if strings.HasPrefix(strings.TrimSpace(line), "Urgency:") && strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "Urgency:")) != "" {
					d.Evidence = append(d.Evidence, InboxEvidence{ID: "urgency:" + d.Key, Kind: "urgency", Source: d.Ticket, Revision: d.WaitID + "@" + FormatTime(d.WaitingAt), Excerpt: strings.TrimSpace(line)})
					break
				}
			}
			for i := range d.Evidence {
				d.Evidence[i].DecisionKey = d.Key
			}
			entries[d.Key] = d
		}
		// A graph wait is eligible only after durable publication and only on its current running graph.
		waits, e := q.QueryContext(ctx, `SELECT w.delivery_key,COALESCE(NULLIF(g.workflow,''),g.mode),c.kind,c.candidate,d.destination,d.comment_id,c.node,COALESCE(a.finished_at,c.created_at,''),CASE WHEN a.finished_at IS NOT NULL THEN 'graph source attempt finished_at' WHEN c.created_at IS NOT NULL THEN 'graph stop created_at' ELSE '' END FROM graph_owner_waits w LEFT JOIN graph_node_attempts a ON a.id=w.source_attempt_id JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions g ON g.id=i.definition_id JOIN graph_comment_intents c ON c.delivery_key=w.delivery_key JOIN report_deliveries d ON d.delivery_key=w.delivery_key WHERE i.session_id=? AND i.status='running' AND i.id=(SELECT MAX(id) FROM graph_instances WHERE session_id=?) AND w.state='waiting' AND d.completed=1 AND d.comment_id>0`, s.ID, s.ID)
		if e != nil {
			return nil, nil, e
		}
		for waits.Next() {
			d := base
			var candidate []byte
			var comment int64
			var node, waitingAt string
			if e = waits.Scan(&d.WaitID, &d.Workflow, &d.Kind, &candidate, &d.Destination, &comment, &node, &waitingAt, &d.WaitingProvenance); e != nil {
				return nil, nil, errors.Join(e, waits.Close())
			}
			d.WaitingAt, _ = ParseTime(waitingAt)
			if d.Workflow == "plan" {
				// Stage and set reviews use the exact current Plan policy below.
				// Graph-only questions, including set_ask and failure recovery,
				// have no PlanWait representation and remain independently eligible.
				switch node {
				case "story_ask", "story_review", "technical_ask", "technical_review", "acceptance_ask", "acceptance_review", "set_review":
					continue
				}
			}
			d.Summary = "Waiting for owner " + d.Kind
			var body struct {
				Questions []struct {
					Text string `json:"text"`
				} `json:"questions"`
			}
			if json.Unmarshal(candidate, &body) == nil {
				for _, question := range body.Questions {
					if question.Text != "" {
						d.Summary += "\n" + question.Text
					}
				}
			}
			d.Action = "Reply to the published " + d.Kind + " on the owning ticket"
			add(d)
		}
		if e = waits.Err(); e != nil {
			return nil, nil, errors.Join(e, waits.Close())
		}
		if e = waits.Close(); e != nil {
			return nil, nil, e
		}
		var raw string
		e = q.QueryRowContext(ctx, `SELECT state_json FROM plan_workflow_states WHERE parent=?`, s.TaskRef).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, nil, e
		}

		planActive := true
		var planStatus string
		pe := q.QueryRowContext(ctx, `SELECT i.status FROM graph_instances i JOIN graph_definitions d ON d.id=i.definition_id WHERE i.session_id=? AND (d.workflow='plan' OR d.mode='plan') ORDER BY i.id DESC LIMIT 1`, s.ID).Scan(&planStatus)
		if pe == nil {
			planActive = planStatus == GraphRunning
		} else if !errors.Is(pe, sql.ErrNoRows) {
			return nil, nil, pe
		}
		if e == nil && planActive {
			var state planbrief.PlanState
			if e = json.Unmarshal([]byte(raw), &state); e != nil {
				return nil, nil, e
			}

			parentWait := func(waitID, kind, action string) error {
				var dest string
				e := q.QueryRowContext(ctx, `SELECT destination FROM report_deliveries WHERE session_id=? AND purpose=? AND completed=1 AND comment_id>0 ORDER BY rowid DESC LIMIT 1`, s.ID, waitID).Scan(&dest)
				if errors.Is(e, sql.ErrNoRows) {
					return nil
				}
				if e != nil {
					return e
				}
				d := base
				d.WaitID = waitID
				d.Workflow = "plan"
				d.Kind = kind
				d.Summary = "Waiting for owner " + kind
				d.Action = action
				d.Destination = dest
				inboxWaitTime(ctx, q, &d, s.ID, waitID)
				add(d)
				return nil
			}
			if state.AssembledWait != "" && state.AssembledWait == state.PlanSetRevision() {
				if e = parentWait("plan-assembled:"+state.AssembledWait, "assembled-review", "/approve-plan-set "+state.AssembledWait); e != nil {
					return nil, nil, e
				}
			}
			if set := state.StorySet; set != nil && !set.Promoted && set.Revision != "" {
				var purpose string
				e = q.QueryRowContext(ctx, `SELECT d.purpose FROM report_deliveries d JOIN graph_comment_intents c ON c.delivery_key=d.delivery_key WHERE d.session_id=? AND c.node='set_review' AND d.completed=1 AND d.comment_id>0 AND instr(d.body,?)>0 ORDER BY d.rowid DESC LIMIT 1`, s.ID, set.Revision).Scan(&purpose)
				if e == nil {
					if e = parentWait(purpose, "set-review", planbrief.PlanSetPromotionCommand(set.Revision)); e != nil {
						return nil, nil, e
					}
				} else if !errors.Is(e, sql.ErrNoRows) {
					return nil, nil, e
				}
			}
			for _, story := range state.Stories {
				w := story.Wait
				if w == nil || !story.Active() || w.Candidate != story.Candidate {
					continue
				}
				revision, re := state.ReviewRevision(story.Key, story.Stage)
				if re != nil || revision != w.Revision {
					continue
				}
				var destination string
				var comment int64
				e = q.QueryRowContext(ctx, `SELECT destination,comment_id FROM report_deliveries WHERE (delivery_key=? OR purpose=?) AND session_id=? AND completed=1 AND comment_id>0 ORDER BY rowid DESC LIMIT 1`, w.ID, w.ID, s.ID).Scan(&destination, &comment)
				if errors.Is(e, sql.ErrNoRows) {
					continue
				}
				if e != nil {
					return nil, nil, e
				}
				// Replace a duplicate graph representation with the richer Plan policy projection.
				for key, d := range entries {
					if d.WaitID == w.ID {
						delete(entries, key)
					}
				}
				d := base
				d.WaitID = w.ID
				d.Workflow = "plan"
				d.Kind = w.Kind
				inboxWaitTime(ctx, q, &d, s.ID, w.ID)
				for _, dependent := range state.Stories {
					if !dependent.Active() {
						continue
					}
					for _, dependency := range dependent.Draft.Dependencies.StoryKeys {
						if dependency == story.Key {
							d.Evidence = append(d.Evidence, InboxEvidence{ID: "plan-dependency:" + w.ID + ":" + dependent.Key, Kind: "dependency", Source: s.TaskRef + "/" + dependent.Key, Revision: dependent.Draft.StageRevision("story"), Target: dependent.Key, Excerpt: dependent.Key + " depends on " + story.Key})
						}
					}
				}
				d.Summary = story.Key + ": waiting for owner " + w.Kind + " at " + story.Stage
				d.Destination = destination
				d.ActionUnavailable = state.PlanReplyHold(story.Key)
				if d.ActionUnavailable == "" {
					d.Action = state.PlanCurrentCommand(story.Key)
					if d.Action == "" {
						if w.Kind == "question" {
							d.Action = "Answer the published question on the story ticket"
						} else {
							d.ActionUnavailable = "Current action reference is unavailable"
						}
					}
				}
				var number int
				e = q.QueryRowContext(ctx, `SELECT issue_number FROM plan_story_drafts WHERE parent=? AND story_key=? AND retired=0 AND issue_id=?`, s.TaskRef, story.Key, w.IssueID).Scan(&number)
				if e == nil && number > 0 {
					child, ce := scanSessionTicket(q.QueryRowContext(ctx, `SELECT `+sessionTicketColumns+` FROM session_ticket_identities WHERE session_id=(SELECT id FROM sessions WHERE repo_id=? AND issue_number=?)`, s.RepoID, number))
					switch {
					case ce == nil:
						d.Locator = child.Locator
						d.Ref = ticket.Ref{Locator: child.Locator, Identity: ticket.Identity{Provider: child.Locator.Provider, Origin: child.Locator.Origin, RemoteID: child.RemoteID}}
						d.Ticket = child.TaskRef
					case v.Provider == ticket.Linear:
						var remote string
						ce = q.QueryRowContext(ctx, `SELECT remote_id FROM ticket_local_slots WHERE id=? AND kind='issue' AND provider=? AND api_origin=?`, w.IssueID, v.Provider, v.Origin).Scan(&remote)
						if ce == nil {
							d.Locator.Key = remote
							d.Ref.Locator = d.Locator
							d.Ref.Identity.RemoteID = remote
							d.Ticket = "linear:" + v.Destination + "#" + remote
						} else {
							d.Action = ""
							d.ActionUnavailable = "Story ticket identity is unavailable"
						}
					case v.Provider == ticket.GitHub:
						d.Locator.Key = strconv.Itoa(number)
						d.Ref.Locator = d.Locator
						d.Ref.Identity.RemoteID = strconv.FormatInt(w.IssueID, 10)
						d.Ticket = TaskRef(string(v.Provider), strings.SplitN(v.Destination, "/", 2)[0], strings.SplitN(v.Destination, "/", 2)[1], number)
					}
				}
				add(d)
			}
		}
		parked, e := q.QueryContext(ctx, `SELECT generation,story_key,reason,state_at FROM implement_stories WHERE (worker_session=? OR (worker_session=0 AND parent=?)) AND state='parked' AND parent NOT IN (SELECT parent_task FROM managed_parent_closures) AND NOT EXISTS(SELECT 1 FROM plan_story_drafts p WHERE p.parent=implement_stories.parent AND p.story_key=implement_stories.story_key AND p.retired=1) AND generation=(SELECT MAX(generation) FROM implement_stories x WHERE x.parent=implement_stories.parent)`, s.ID, s.TaskRef)
		if e != nil {
			return nil, nil, e
		}
		for parked.Next() {
			d := base
			var generation int64
			var key, waitingAt string
			if e = parked.Scan(&generation, &key, &d.Summary, &waitingAt); e != nil {
				return nil, nil, errors.Join(e, parked.Close())
			}
			d.WaitID = fmt.Sprintf("implement:%d:%s", generation, key)
			d.Workflow = "implement"
			d.Kind = "parked"
			d.WaitingAt, _ = ParseTime(waitingAt)
			d.WaitingProvenance = "implement story state_at"
			d.Action = "Reply on the owning ticket with guidance to resume this story"
			add(d)
		}
		if e = parked.Err(); e != nil {
			return nil, nil, errors.Join(e, parked.Close())
		}
		if e = parked.Close(); e != nil {
			return nil, nil, e
		}
	}
	out := make([]InboxDecision, 0, len(entries))
	for _, d := range entries {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		for _, pair := range [][2]string{{a.Source, b.Source}, {a.Project, b.Project}, {inboxTicketIdentity(a), inboxTicketIdentity(b)}, {a.Key, b.Key}} {
			if pair[0] != pair[1] {
				return pair[0] < pair[1]
			}
		}
		return false
	})
	return out, coverage, nil
}

// InboxSnapshotAuthorized protects saved answers containing every captured entry.
func (db *DB) InboxSnapshotAuthorized(ctx context.Context, scope InboxScope, id string, now time.Time) error {
	if err := db.InboxSourcesAuthorized(ctx, scope, id, nil, now); err != nil {
		return err
	}
	rows, err := db.sql.QueryContext(ctx, `SELECT DISTINCT json_extract(content,'$.ConnectionID') FROM owner_inbox_entries WHERE snapshot_id=?`, id)
	if err != nil {
		return err
	}
	var represented []int64
	for rows.Next() {
		var connectionID int64
		if err = rows.Scan(&connectionID); err != nil {
			return errors.Join(err, rows.Close())
		}
		represented = append(represented, connectionID)
	}
	if err = rows.Err(); err != nil {
		return errors.Join(err, rows.Close())
	}
	if err = rows.Close(); err != nil {
		return err
	}
	return db.InboxSourcesAuthorized(ctx, scope, id, represented, now)
}

// InboxSourcesAuthorized protects only the factory-selected sources represented
// in one rendered response. Empty source IDs authorize safe navigation and
// coverage under the scoped, unexpired snapshot without exposing ticket content.
func (db *DB) InboxSourcesAuthorized(ctx context.Context, scope InboxScope, id string, sourceIDs []int64, now time.Time) error {
	if !inboxScopeValid(scope) {
		return ErrInboxExpired
	}
	var raw string
	err := db.sql.QueryRowContext(ctx, `SELECT metadata FROM owner_inbox_snapshots WHERE id=? AND owner=? AND transport=? AND conversation=? AND expires_at>?`, id, scope.Owner, scope.Transport, scope.Conversation, FormatTime(now)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInboxExpired
	}
	if err != nil {
		return err
	}
	var snapshot InboxSnapshot
	if err = json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return err
	}
	captured := map[int64]InboxCoverage{}
	for _, c := range snapshot.Coverage {
		if c.ConnectionID > 0 {
			captured[c.ConnectionID] = c
		}
	}
	for _, sourceID := range sourceIDs {
		c, ok := captured[sourceID]
		if !ok {
			return ErrInboxExpired
		}
		v, e := db.CurrentConnection(ctx, c.RepoID, TicketConnection)
		if e != nil || !v.Enabled || v.ID != sourceID || !inboxSourceAllowed(ctx, db.sql, v.ID) {
			return ErrInboxExpired
		}
	}
	return nil
}

// PurgeInboxSnapshots deletes expired display data and its child entries.
func (db *DB) PurgeInboxSnapshots(ctx context.Context, now time.Time) error {
	_, err := db.sql.ExecContext(ctx, `DELETE FROM owner_inbox_snapshots WHERE expires_at<=?`, FormatTime(now))
	return err
}

func inboxSourceAllowed(ctx context.Context, q inboxQuery, version int64) bool {
	var status string
	err := q.QueryRowContext(ctx, `SELECT status FROM project_connection_observations WHERE version_id=?`, version).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if err != nil {
		return false
	}
	switch status {
	case "disabled", "conflict", "missing_credential", "locked_credential", "rejected_credential", "insufficient_permission", "inaccessible_or_missing_destination":
		return false
	}
	return true
}

func inboxTicketIdentity(d InboxDecision) string {
	if d.Ref.Identity.RemoteID != "" {
		return string(d.Ref.Identity.Provider) + "|" + string(d.Ref.Identity.Origin) + "|" + d.Ref.Identity.RemoteID
	}
	return string(d.Locator.Provider) + "|" + string(d.Locator.Origin) + "|" + d.Locator.Container + "|" + d.Locator.Key
}

// InboxTicketIdentity returns the qualified canonical identity used for grouping.
func InboxTicketIdentity(d InboxDecision) string { return inboxTicketIdentity(d) }

func inboxWaitTime(ctx context.Context, q inboxQuery, d *InboxDecision, sessionID int64, waitID string) {
	var at sql.NullString
	err := q.QueryRowContext(ctx, `SELECT COALESCE(a.finished_at,c.created_at) FROM report_deliveries r LEFT JOIN graph_comment_intents c ON c.delivery_key=r.delivery_key LEFT JOIN graph_node_attempts a ON a.id=c.source_attempt_id WHERE r.session_id=? AND (r.delivery_key=? OR r.purpose=?) AND r.completed=1 ORDER BY r.rowid DESC LIMIT 1`, sessionID, waitID, waitID).Scan(&at)
	if err == nil && at.Valid {
		d.WaitingAt, _ = ParseTime(at.String)
		d.WaitingProvenance = "published request creation/finish time"
	}
}
