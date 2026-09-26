package store

import (
	"context"
	"database/sql"
	"errors"
)

func bugClarificationsTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview) error {
	if len(r.Presentation) > 32 {
		return errors.New("bug clarification presentation exceeds bound")
	}
	seen := make(map[int64]bool)
	for _, c := range r.Presentation {
		if c.CommentID <= 0 || seen[c.CommentID] || len(c.References) > 5 {
			return errors.New("invalid Bug clarification reference")
		}
		seen[c.CommentID] = true
		var body, kind string
		if err := tx.QueryRowContext(ctx, `SELECT body,kind FROM bug_delivery_replies WHERE session_id=? AND digest=? AND comment_id=?`, r.SessionID, c.ReviewDigest, c.CommentID).Scan(&body, &kind); err != nil {
			return err
		}
		if c.Body != body || (kind != "clarify" && kind != "discussion") {
			return errors.New("bug clarification differs from retained discussion")
		}
		refs := make(map[string]bool)
		for _, ref := range c.References {
			if refs[ref] {
				return errors.New("duplicate Bug clarification evidence")
			}
			refs[ref] = true
			switch ref {
			case "analysis", "repair", "regression", "focused", "full":
			default:
				return errors.New("bug clarification can reference only retained evidence")
			}
		}
	}
	var priorDigest string
	err := tx.QueryRowContext(ctx, `SELECT digest FROM bug_delivery_reviews WHERE session_id=? AND active=1`, r.SessionID).Scan(&priorDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	prior, err := bugDeliveryReviewTx(ctx, tx, r.SessionID, priorDigest)
	if err != nil {
		return err
	}
	if prior.Evidence.Child.ID != r.Evidence.Child.ID {
		return nil
	}
	// A changed presentation cannot silently drop pending discussion or earlier
	// clarification history while reusing the very same check evidence.
	for _, c := range prior.Presentation {
		if !seen[c.CommentID] {
			return errors.New("bug presentation omits earlier clarification")
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT comment_id FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind!='approved'`, r.SessionID, priorDigest)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return err
		}
		if !seen[id] {
			return errors.New("bug presentation omits unresolved feedback")
		}
	}
	return rows.Err()
}

// BugDeliveryDiscussion supplies retained text; the factory may select relevant
// existing evidence sections as a response. No new cause or remedy can be added.
func (d *DB) BugDeliveryDiscussion(ctx context.Context, session int64, digest string) ([]BugDeliveryClarification, error) {
	var out []BugDeliveryClarification
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		r, e := bugDeliveryReviewTx(ctx, tx, session, digest)
		if e != nil {
			return e
		}
		out = append(out, r.Presentation...)
		rows, e := tx.QueryContext(ctx, `SELECT comment_id,body FROM bug_delivery_replies WHERE session_id=? AND digest=? AND kind IN ('clarify','discussion') ORDER BY comment_id`, session, digest)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			c := BugDeliveryClarification{ReviewDigest: digest}
			if e = rows.Scan(&c.CommentID, &c.Body); e != nil {
				return e
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

func bugDeliveryWaitTx(ctx context.Context, tx *sql.Tx, r BugDeliveryReview, in GraphOwnerReply) error {
	var session int64
	var workflow, status, waitState string
	err := tx.QueryRowContext(ctx, `SELECT i.session_id,d.workflow,i.status,w.state FROM graph_owner_waits w JOIN graph_instances i ON i.id=w.instance_id JOIN graph_definitions d ON d.id=i.definition_id WHERE w.delivery_key=? AND w.instance_id=?`, r.DeliveryKey, in.InstanceID).Scan(&session, &workflow, &status, &waitState)
	if err != nil {
		return err
	}
	if session != r.SessionID || workflow != "bug" || status != "running" || waitState != "waiting" {
		return errors.New("bug delivery reply has no active factory owner wait")
	}
	return nil
}
