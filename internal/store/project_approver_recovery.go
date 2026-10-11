package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// RecoverConsumedProjectAuthorityClaims settles abandoned remote guards only
// from original qualified source evidence. Existing effect owners still govern
// pending publications; this never changes their bytes or terminal state.
func (db *DB) RecoverConsumedProjectAuthorityClaims(ctx context.Context, repoID int64) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		var live int
		if e := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sessions s WHERE s.repo_id=? AND (s.state='running' OR COALESCE(s.lock_owner,'')<>'' OR EXISTS(SELECT 1 FROM runs r WHERE r.session_id=s.id AND r.status IN ('prelaunch','running'))))+(SELECT COUNT(*) FROM project_authority_claims WHERE repo_id=? AND kind='worker' AND completed=0)`, repoID, repoID).Scan(&live); e != nil {
			return e
		}
		if live > 0 {
			return nil
		}
		rows, err := tx.QueryContext(ctx, `SELECT id,actor_provider,actor_origin,actor_id,source_provider,source_origin,source_id,source_created_at,kind,claim_key FROM project_authority_claims WHERE repo_id=? AND kind<>'worker' AND completed=0`, repoID)
		if err != nil {
			return err
		}
		var claims []AuthorityClaim
		for rows.Next() {
			var c AuthorityClaim
			var created string
			if err = rows.Scan(&c.ID, &c.Actor.Provider, &c.Actor.Origin, &c.Actor.RemoteID, &c.Source.Provider, &c.Source.Origin, &c.Source.RemoteID, &created, &c.Kind, &c.Key); err != nil {
				_ = rows.Close()
				return err
			}
			c.CreatedAt, err = ParseTime(created)
			if err != nil {
				_ = rows.Close()
				return err
			}
			claims = append(claims, c)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		for _, c := range claims {
			if c.Source.Provider != ticket.GitHub || c.Actor.Provider != ticket.GitHub || c.Actor.Origin != c.Source.Origin {
				continue
			}
			if c.Kind == "doctor-instruction" {
				sourceID, e := strconv.ParseInt(c.Source.RemoteID, 10, 64)
				if e != nil || sourceID <= 0 || c.Key != c.Source.RemoteID {
					continue
				}
				var admitted int
				e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM doctor_inbox i JOIN project_authority_claims claim ON claim.id=? JOIN project_approver_appointments a ON a.id=claim.appointment_id WHERE i.repo_id=? AND i.comment_id=? AND CAST(i.actor_id AS TEXT)=? AND i.created_at=? AND a.repo_id=i.repo_id AND a.provider=? AND a.api_origin=? AND a.actor_id=?`, c.ID, repoID, sourceID, c.Actor.RemoteID, FormatTime(c.CreatedAt), c.Source.Provider, c.Source.Origin, c.Actor.RemoteID).Scan(&admitted)
				if e != nil {
					return e
				}
				if admitted > 0 {
					if _, e = tx.ExecContext(ctx, `UPDATE project_authority_claims SET completed=1 WHERE id=? AND completed=0`, c.ID); e != nil {
						return e
					}
				}
				continue
			}
			var sessionID int64
			var taskRef string
			var body bool
			switch c.Kind {
			case "instruction", "turn":
				key, trigger, ok := strings.Cut(c.Key, ":")
				var e error
				sessionID, e = strconv.ParseInt(key, 10, 64)
				if !ok || e != nil || sessionID <= 0 {
					continue
				}
				body = c.Kind == "turn" && trigger == "0"
			case "code-control":
				if !strings.HasPrefix(c.Key, "turn:") {
					key, trigger, ok := strings.Cut(c.Key, ":")
					var e error
					sessionID, e = strconv.ParseInt(key, 10, 64)
					if !ok || e != nil || sessionID <= 0 || trigger != c.Source.RemoteID {
						continue
					}
					break
				}
				fallthrough
			case "creation-approval":
				key := strings.TrimPrefix(c.Key, "turn:")
				index := strings.LastIndex(key, ":")
				if index < 1 {
					continue
				}
				taskRef = key[:index]
			case "child-instruction":
			default:
				continue
			}
			var consumed int
			sourceID, parseErr := strconv.ParseInt(c.Source.RemoteID, 10, 64)
			if !body && parseErr == nil && sourceID > 0 && strconv.FormatInt(sourceID, 10) == c.Source.RemoteID {
				actorID, e := strconv.ParseInt(c.Actor.RemoteID, 10, 64)
				if e != nil || actorID <= 0 {
					continue
				}
				err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments c JOIN sessions s ON s.id=c.session_id JOIN session_ticket_identities t ON t.session_id=s.id JOIN watermarks w ON w.session_id=s.id WHERE s.repo_id=? AND t.provider=? AND t.api_origin=? AND c.forge_comment_id=? AND c.author_user_id=? AND c.author_is_bot=0 AND c.created_at=? AND (?=0 OR s.id=?) AND (?='' OR s.task_ref=?) AND w.last_comment_id>=c.forge_comment_id`, repoID, c.Source.Provider, c.Source.Origin, sourceID, actorID, FormatTime(c.CreatedAt), sessionID, sessionID, taskRef, taskRef).Scan(&consumed)
				if err != nil {
					return err
				}
				if consumed == 0 && c.Kind == "child-instruction" {
					err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM story_refinements r JOIN sessions s ON s.id=r.session_id JOIN session_ticket_identities t ON t.session_id=s.id WHERE s.repo_id=? AND t.provider=? AND t.api_origin=? AND r.comment_id=? AND r.actor_id=? AND r.consumed=1`, repoID, c.Source.Provider, c.Source.Origin, sourceID, actorID).Scan(&consumed)
					if err != nil {
						return err
					}
				}
			}
			// Body source is the ticket identity, not a remote comment. Only its exact
			// session's completed body run can prove that this instruction settled.
			if consumed == 0 && body {
				err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions s JOIN session_ticket_identities t ON t.session_id=s.id JOIN runs r ON r.session_id=s.id WHERE s.id=? AND s.repo_id=? AND t.provider=? AND t.api_origin=? AND t.remote_id=? AND r.trigger_comment_id=0 AND r.status='reported' AND r.result_comment_id>0`, sessionID, repoID, c.Source.Provider, c.Source.Origin, c.Source.RemoteID).Scan(&consumed)
				if err != nil {
					return err
				}
			}

			if consumed > 0 {
				if _, err = tx.ExecContext(ctx, `UPDATE project_authority_claims SET completed=1 WHERE id=? AND completed=0`, c.ID); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
