package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ownerInputNodePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// OwnerInputTarget is an optional exact receiver. A replacement instance never
// inherits another instance's input. Zero means the next eligible agent step.
type OwnerInputTarget struct {
	InstanceID int64
	Node       string
	LaunchID   int64
}

// ParseOwnerInputTarget reads only explicit first-line metadata. It does not
// modify the admitted bytes, infer a target from prose or choose a replacement.
// Daemon admission and transactional recovery share this exact grammar.
func ParseOwnerInputTarget(body string) (OwnerInputTarget, error) {
	var out OwnerInputTarget
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	if !strings.HasPrefix(line, "Input-Target:") {
		return out, nil
	}
	seen := map[string]bool{}
	for _, field := range strings.Fields(strings.TrimPrefix(line, "Input-Target:")) {
		key, value, ok := strings.Cut(field, "=")
		if !ok || seen[key] {
			return out, errors.New("invalid explicit owner input target")
		}
		seen[key] = true
		switch key {
		case "instance", "launch":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil || n < 1 {
				return out, errors.New("invalid owner input target identity")
			}
			if key == "instance" {
				out.InstanceID = n
			} else {
				out.LaunchID = n
			}
		case "node":
			out.Node = value
		default:
			return out, fmt.Errorf("unknown owner input target field %q", key)
		}
	}
	if out.InstanceID < 1 || !ownerInputNodePattern.MatchString(out.Node) {
		return out, errors.New("owner input target needs instance and valid node")
	}
	return out, nil
}

// OwnerInstruction retains admitted bytes through its immutable comment source.
// A zero AttemptID is visibly undelivered, even after ingestion is acknowledged.
type OwnerInstruction struct {
	SessionID, CommentID, ActorID, AttemptID int64
	Body                                     string
	Target                                   OwnerInputTarget
	AssignedAt                               time.Time
}

// RetainOwnerInstruction authenticates against the retained source before any
// ingestion cursor advances. Replays cannot change bytes, identity or target.
func (db *DB) RetainOwnerInstruction(ctx context.Context, sessionID, commentID, ownerID int64, body string, target OwnerInputTarget) (OwnerInstruction, error) {
	var out OwnerInstruction
	if ownerID < 1 || commentID < 1 || strings.TrimSpace(body) == "" || len(body) > 65536 || target.InstanceID < 0 || target.LaunchID < 0 || (target.InstanceID == 0 && (target.Node != "" || target.LaunchID != 0)) || (target.InstanceID > 0 && !ownerInputNodePattern.MatchString(target.Node)) {
		return out, graphInvariant("invalid ordinary owner instruction")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = retainOwnerInstructionTx(ctx, tx, sessionID, commentID, ownerID, body, target)
		return e
	})
	return out, err
}

// retainOwnerInstructionTx shares the authenticated handoff with atomic Doctor repair.
func retainOwnerInstructionTx(ctx context.Context, tx *sql.Tx, sessionID, commentID, ownerID int64, body string, target OwnerInputTarget) (OwnerInstruction, error) {
	c, e := scanComment(tx.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE session_id=? AND forge_comment_id=?`, sessionID, commentID))
	if e != nil {
		return OwnerInstruction{}, e
	}
	if c.AuthorUserID != ownerID || c.Classification != ClassificationInstruction || c.Body != body {
		return OwnerInstruction{}, graphInvariant("owner instruction source mismatch")
	}
	if target.InstanceID > 0 {
		var matches bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM graph_instances WHERE id=? AND session_id=?)`, target.InstanceID, sessionID).Scan(&matches); e != nil {
			return OwnerInstruction{}, e
		}
		if !matches {
			return OwnerInstruction{}, graphInvariant("owner instruction target belongs to another task")
		}
		if target.LaunchID > 0 {
			if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM research_launch_definitions WHERE id=? AND instance_id=?)`, target.LaunchID, target.InstanceID).Scan(&matches); e != nil {
				return OwnerInstruction{}, e
			}
			if !matches {
				return OwnerInstruction{}, graphInvariant("owner instruction launch differs from target")
			}
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO owner_instruction_inputs(comment_id,session_id,target_instance_id,target_node,target_launch_id) VALUES(?,?,?,?,?) ON CONFLICT(comment_id) DO NOTHING`, c.ID, sessionID, target.InstanceID, target.Node, target.LaunchID)
	if e != nil {
		return OwnerInstruction{}, e
	}
	out, e := scanOwnerInstruction(tx.QueryRowContext(ctx, `SELECT `+ownerInstructionColumns+` FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.comment_id=?`, c.ID))
	if e != nil {
		return OwnerInstruction{}, e
	}
	if out.Target != target {
		return OwnerInstruction{}, graphInvariant("owner instruction target changed on replay")
	}
	return out, nil
}

const ownerInstructionColumns = `o.session_id,c.forge_comment_id,c.author_user_id,c.body,o.target_instance_id,o.target_node,o.target_launch_id,COALESCE(o.assigned_attempt_id,0),o.assigned_at`

func scanOwnerInstruction(s scanner) (OwnerInstruction, error) {
	var o OwnerInstruction
	var at sql.NullString
	err := s.Scan(&o.SessionID, &o.CommentID, &o.ActorID, &o.Body, &o.Target.InstanceID, &o.Target.Node, &o.Target.LaunchID, &o.AttemptID, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return o, ErrNotFound
	}
	if err != nil {
		return o, err
	}
	o.AssignedAt, err = scanTime(at)
	return o, err
}

// ListOwnerInstructions returns pending input or the immutable input assigned
// to one attempt, in admitted source-comment order. attemptID zero means pending.
func (db *DB) ListOwnerInstructions(ctx context.Context, sessionID, attemptID int64) ([]OwnerInstruction, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+ownerInstructionColumns+` FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=? AND COALESCE(o.assigned_attempt_id,0)=? ORDER BY c.forge_comment_id`, sessionID, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnerInstruction
	for rows.Next() {
		o, e := scanOwnerInstruction(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func assignOwnerInstructionsTx(ctx context.Context, tx *sql.Tx, in GraphAttemptStart, attemptID, sessionID int64) error {
	if !in.AcceptOwnerInput {
		return nil
	}
	if in.RunID < 1 {
		return graphInvariant("ordinary input requires an eligible agent step")
	}
	_, err := tx.ExecContext(ctx, `UPDATE owner_instruction_inputs SET assigned_attempt_id=?,assigned_at=? WHERE session_id=? AND assigned_attempt_id IS NULL AND (target_instance_id=0 OR (target_instance_id=? AND target_node=? AND (target_launch_id=0 OR target_launch_id=COALESCE((SELECT launch_id FROM graph_node_attempts WHERE id=?),0))))`, attemptID, FormatTime(in.StartedAt), sessionID, in.InstanceID, in.Node, attemptID)
	return err
}

// OwnerGuidance is separate from one-time inputs and decisions. Empty Body at
// a positive revision explicitly withdraws guidance from a resumed conversation.
type OwnerGuidance struct {
	Revision int64
	Body     string
}

// SetOwnerGuidance retains an immutable authenticated set/clear revision.
func (db *DB) SetOwnerGuidance(ctx context.Context, sessionID, commentID, ownerID int64, body string) (OwnerGuidance, error) {
	var out OwnerGuidance
	if ownerID < 1 || len(body) > 65536 {
		return out, graphInvariant("invalid owner guidance")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		c, e := scanComment(tx.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE session_id=? AND forge_comment_id=?`, sessionID, commentID))
		if e != nil {
			return e
		}
		if c.AuthorUserID != ownerID || c.Classification != ClassificationInstruction {
			return graphInvariant("guidance requires authenticated owner source")
		}
		source := strings.TrimSpace(c.Body)
		if source != "/guide clear" && (!strings.HasPrefix(source, "/guide ") && !strings.HasPrefix(source, "/guide\t") || strings.TrimSpace(strings.TrimPrefix(source, "/guide")) != body) {
			return graphInvariant("guidance does not match admitted source")
		}
		if source == "/guide clear" && body != "" {
			return graphInvariant("clear cannot retain active guidance")
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO issue_owner_guidance(session_id,comment_id,body) VALUES(?,?,?) ON CONFLICT(comment_id) DO NOTHING`, sessionID, c.ID, body)
		if e != nil {
			return e
		}
		e = tx.QueryRowContext(ctx, `SELECT id,body FROM issue_owner_guidance WHERE comment_id=?`, c.ID).Scan(&out.Revision, &out.Body)
		if e == nil && out.Body != body {
			return graphInvariant("guidance changed on replay")
		}
		return e
	})
	return out, err
}

// CurrentOwnerGuidance reads standing guidance independently of bounded history.
func (db *DB) CurrentOwnerGuidance(ctx context.Context, sessionID int64) (OwnerGuidance, error) {
	var o OwnerGuidance
	err := db.sql.QueryRowContext(ctx, `SELECT g.id,g.body FROM issue_owner_guidance g JOIN comments c ON c.id=g.comment_id WHERE g.session_id=? ORDER BY c.forge_comment_id DESC LIMIT 1`, sessionID).Scan(&o.Revision, &o.Body)
	if errors.Is(err, sql.ErrNoRows) {
		return o, nil
	}
	return o, err
}

// OwnerGuidanceForRun snapshots one revision before prompt construction. A
// prelaunch retry reuses it even if the owner replaces standing guidance.
func (db *DB) OwnerGuidanceForRun(ctx context.Context, runID int64) (OwnerGuidance, error) {
	var out OwnerGuidance
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO owner_run_guidance(run_id,guidance_id) SELECT r.id,(SELECT g.id FROM issue_owner_guidance g JOIN comments c ON c.id=g.comment_id WHERE g.session_id=r.session_id ORDER BY c.forge_comment_id DESC LIMIT 1) FROM runs r WHERE r.id=? ON CONFLICT(run_id) DO NOTHING`, runID)
		if e != nil {
			return e
		}
		return tx.QueryRowContext(ctx, `SELECT COALESCE(g.id,0),COALESCE(g.body,'') FROM owner_run_guidance s LEFT JOIN issue_owner_guidance g ON g.id=s.guidance_id WHERE s.run_id=?`, runID).Scan(&out.Revision, &out.Body)
	})
	if err != nil {
		return out, fmt.Errorf("store: snapshot owner guidance: %w", err)
	}
	return out, nil
}

// GetOwnerInstruction reads the immutable admitted source and target, regardless
// of assignment. Ingestion can replay it without adopting edited forge bytes.
func (db *DB) GetOwnerInstruction(ctx context.Context, sessionID, commentID int64) (OwnerInstruction, error) {
	return scanOwnerInstruction(db.sql.QueryRowContext(ctx, `SELECT `+ownerInstructionColumns+` FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=? AND c.forge_comment_id=?`, sessionID, commentID))
}

// OwnerInstructionsForAttempt recovers the input of the same logical step when
// orphan reconciliation must allocate another attempt. Assignment identities
// remain immutable. A succeeded invocation closes that logical input window;
// later loop entries or another launch never receive it as current direction.
func (db *DB) OwnerInstructionsForAttempt(ctx context.Context, attemptID int64) ([]OwnerInstruction, error) {
	rows, err := db.sql.QueryContext(ctx, `SELECT `+ownerInstructionColumns+` FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id JOIN graph_node_attempts assigned ON assigned.id=o.assigned_attempt_id JOIN graph_node_attempts current ON current.id=? WHERE assigned.instance_id=current.instance_id AND assigned.node=current.node AND COALESCE(assigned.launch_id,0)=COALESCE(current.launch_id,0) AND assigned.id<=current.id AND (assigned.id=current.id OR (assigned.status IN ('failed','pending') AND assigned.id>COALESCE((SELECT MAX(done.id) FROM graph_node_attempts done WHERE done.instance_id=current.instance_id AND done.node=current.node AND done.id<current.id AND done.status='succeeded'),0))) ORDER BY c.forge_comment_id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OwnerInstruction
	for rows.Next() {
		o, e := scanOwnerInstruction(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
