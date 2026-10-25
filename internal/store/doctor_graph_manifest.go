package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// doctorGraphImage is an immutable, owner-visible exact proposal. Related is a
// digest of the complete scoped row set, not merely the target's status. Revision
// additionally detects intervening progress that restores the same row values.
type doctorGraphRelation struct {
	Table string           `json:"table"`
	Rows  []map[string]any `json:"rows"`
}

type doctorGraphImage struct {
	Version        int                   `json:"version"`
	SessionID      int64                 `json:"session_id"`
	InstanceID     int64                 `json:"instance_id"`
	Row            map[string]any        `json:"row"`
	Guidance       map[string]any        `json:"guidance,omitempty"`
	Related        string                `json:"related"`
	Revision       int64                 `json:"revision"`
	Reason         string                `json:"reason"`
	ReversalOf     string                `json:"reversal_of,omitempty"`
	ReversalNumber int                   `json:"reversal_number,omitempty"`
	Relations      []doctorGraphRelation `json:"relations"`
	Preconditions  string                `json:"preconditions"`
}

// DoctorGraphRepairKind recognizes precisely the four bounded graph transitions.
func DoctorGraphRepairKind(kind string) bool {
	switch kind {
	case "graph_dispatch_cancel", "graph_wait_cancel", "graph_attempt_cancel", "graph_fail":
		return true
	}
	return false
}

func graphImage(s string) (doctorGraphImage, error) {
	var image doctorGraphImage
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if len(s) > 65536 {
		return image, errors.New("graph manifest exceeds bound")
	}
	if err := dec.Decode(&image); err != nil {
		return image, err
	}
	if image.Version != 1 || image.SessionID < 1 || image.InstanceID < 1 || len(image.Related) != 64 || image.Revision < 0 || strings.TrimSpace(image.Reason) == "" || len(image.Reason) > 4096 || image.Row == nil || len(image.Relations) == 0 || image.Preconditions == "" {
		return image, errors.New("invalid exact graph manifest")
	}
	canonical, err := json.Marshal(image)
	if err != nil || string(canonical) != s {
		return image, errors.New("noncanonical exact graph manifest")
	}
	return image, nil
}
func graphStatus(kind string, image doctorGraphImage) string {
	key := "status"
	if kind == "graph_wait_cancel" {
		key = "state"
	}
	s, _ := image.Row[key].(string)
	return s
}

// DoctorGraphRepairBeforeStatus supports factual diagnosis validation without
// interpreting arbitrary row values or granting mutation authority.
func DoctorGraphRepairBeforeStatus(kind, before string) string {
	image, err := graphImage(before)
	if err != nil {
		return ""
	}
	return graphStatus(kind, image)
}

// DoctorGraphRepairTarget validates the exact closed mutation shape. Inverses
// additionally require their retained verified journal at reservation and apply.
func DoctorGraphRepairTarget(kind, target, before, after string) bool {
	if !DoctorGraphRepairKind(kind) {
		return false
	}
	a, e := graphImage(before)
	b, f := graphImage(after)
	if e != nil || f != nil || a.Preconditions != graphPreconditionDescription(kind) {
		return false
	}
	if a.SessionID != b.SessionID || a.InstanceID != b.InstanceID || a.Related != b.Related || a.Revision != b.Revision || a.Reason != b.Reason || a.ReversalOf != b.ReversalOf || a.ReversalNumber != b.ReversalNumber || !reflect.DeepEqual(a.Relations, b.Relations) || a.Preconditions != b.Preconditions {
		return false
	}
	from, to := graphStatus(kind, a), graphStatus(kind, b)
	if a.ReversalOf != "" {
		from, to = to, from
		if a.ReversalNumber < 1 {
			return false
		}
	}
	if !doctorGraphStatusTarget(kind, target, from, to) {
		return false
	}
	allowed := map[string]bool{"status": true}
	switch kind {
	case "graph_wait_cancel":
		allowed = map[string]bool{"state": true}
	case "graph_dispatch_cancel":
		allowed["finished_at"] = true
		allowed["cancellation_reason"] = true
	case "graph_attempt_cancel":
		allowed["finished_at"] = true
	case "graph_fail":
		allowed["finished_at"] = true
		allowed["next_due_at"] = true
	}
	if len(a.Row) != len(b.Row) {
		return false
	}
	for k, v := range a.Row {
		if !allowed[k] && !reflect.DeepEqual(v, b.Row[k]) {
			return false
		}
	}
	if a.ReversalOf == "" {
		if kind != "graph_wait_cancel" {
			v, ok := b.Row["finished_at"].(string)
			if !ok {
				return false
			}
			if _, err := ParseTime(v); err != nil {
				return false
			}
		}
		if kind == "graph_dispatch_cancel" && b.Row["cancellation_reason"] != a.Reason {
			return false
		}
		if kind == "graph_fail" && b.Row["next_due_at"] != nil {
			return false
		}
	}
	key := strings.TrimPrefix(target, doctorRowPrefixes[kind]+":")
	if kind == "graph_wait_cancel" {
		return a.Row["delivery_key"] == key
	}
	return fmt.Sprint(a.Row["id"]) == key
}
func doctorGraphStatusTarget(kind, target, before, after string) bool {
	prefix := doctorRowPrefixes[kind]
	if !strings.HasPrefix(target, prefix+":") {
		return false
	}
	key := strings.TrimPrefix(target, prefix+":")
	if key == "" || strings.ContainsAny(key, " \t\r\n\x00\\/\"'`") {
		return false
	}
	if kind != "graph_wait_cancel" {
		n, e := strconv.ParseInt(key, 10, 64)
		if e != nil || n < 1 || strconv.FormatInt(n, 10) != key {
			return false
		}
	}
	switch kind {
	case "graph_dispatch_cancel":
		return (before == "pending" || before == "reserved" || before == "running") && after == "cancelled"
	case "graph_wait_cancel":
		return (before == "waiting" || before == "claimed" || before == "allocated") && after == "cancelled"
	case "graph_attempt_cancel":
		return (before == "pending" || before == "running") && after == "cancelled"
	case "graph_fail":
		return before == "running" && after == "failed"
	}
	return false
}

// graphRows only accepts factory-owned fixed queries. It snapshots complete rows
// so new metadata columns cannot silently escape exact proposal comparisons.
func graphRows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]map[string]any, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		if len(out) >= 1024 {
			return nil, errors.New("graph related rows exceed bounded inspection")
		}
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if err := rows.Scan(ptr...); err != nil {
			return nil, err
		}
		row := map[string]any{}
		for i, v := range vals {
			if bytes, ok := v.([]byte); ok {
				v = string(bytes)
			}
			row[cols[i]] = v
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
func graphRelated(ctx context.Context, tx *sql.Tx, sessionID int64) (string, int64, error) {
	queries := []string{
		`SELECT * FROM graph_instances WHERE session_id=? ORDER BY id`,
		`SELECT * FROM graph_node_attempts WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY id`,
		`SELECT * FROM graph_dispatches WHERE session_id=? ORDER BY id`,
		`SELECT * FROM graph_owner_waits WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY delivery_key`,
		`SELECT * FROM runs WHERE session_id=? ORDER BY id`,
		`SELECT * FROM research_launch_definitions WHERE session_id=? ORDER BY id`,
		`SELECT * FROM research_successors WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY instance_id`,
		`SELECT * FROM owner_instruction_inputs WHERE session_id=? ORDER BY comment_id`,
		`SELECT * FROM graph_outputs WHERE node_attempt_id IN(SELECT a.id FROM graph_node_attempts a JOIN graph_instances g ON g.id=a.instance_id WHERE g.session_id=?) ORDER BY id`,
		`SELECT * FROM graph_transitions WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY id`,
	}
	var all [][]map[string]any
	for _, q := range queries {
		rows, e := graphRows(ctx, tx, q, sessionID)
		if e != nil {
			return "", 0, e
		}
		all = append(all, rows)
	}
	data, e := json.Marshal(all)
	if e != nil {
		return "", 0, e
	}
	if len(data) > 1048576 {
		return "", 0, errors.New("graph related evidence exceeds bound")
	}
	var revision int64
	e = tx.QueryRowContext(ctx, `SELECT doctor_graph_revision FROM sessions WHERE id=?`, sessionID).Scan(&revision)
	return doctorDigest(data), revision, e
}
func graphTargetImage(ctx context.Context, tx *sql.Tx, ref, kind, target, reason string) (doctorGraphImage, error) {
	var out doctorGraphImage
	key := strings.TrimPrefix(target, doctorRowPrefixes[kind]+":")
	var query string
	switch kind {
	case "graph_dispatch_cancel":
		query = `SELECT d.* FROM graph_dispatches d JOIN sessions s ON s.id=d.session_id WHERE d.id=? AND s.task_ref=?`
	case "graph_wait_cancel":
		query = `SELECT w.* FROM graph_owner_waits w JOIN graph_instances g ON g.id=w.instance_id JOIN sessions s ON s.id=g.session_id WHERE w.delivery_key=? AND s.task_ref=?`
	case "graph_attempt_cancel":
		query = `SELECT a.* FROM graph_node_attempts a JOIN graph_instances g ON g.id=a.instance_id JOIN sessions s ON s.id=g.session_id WHERE a.id=? AND s.task_ref=?`
	case "graph_fail":
		query = `SELECT g.* FROM graph_instances g JOIN sessions s ON s.id=g.session_id WHERE g.id=? AND s.task_ref=?`
	default:
		return out, errors.New("unsupported graph repair")
	}
	rows, e := graphRows(ctx, tx, query, key, ref)
	if e != nil {
		return out, e
	}
	if len(rows) != 1 {
		return out, ErrNotFound
	}
	out = doctorGraphImage{Version: 1, Row: rows[0], Reason: reason}
	var instanceID int64
	if kind == "graph_fail" {
		instanceID, _ = strconv.ParseInt(key, 10, 64)
	} else {
		instanceID, _ = strconv.ParseInt(fmt.Sprint(out.Row["instance_id"]), 10, 64)
	}
	out.InstanceID = instanceID
	e = tx.QueryRowContext(ctx, `SELECT session_id FROM graph_instances WHERE id=?`, instanceID).Scan(&out.SessionID)
	if e != nil {
		return out, e
	}
	out.Related, out.Revision, e = graphRelated(ctx, tx, out.SessionID)
	if e == nil {
		out.Relations, e = graphRelatedSummary(ctx, tx, out.SessionID)
	}
	out.Preconditions = graphPreconditionDescription(kind)
	if e == nil && kind == "graph_wait_cancel" {
		inputs, readErr := graphRows(ctx, tx, `SELECT o.* FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=? AND c.forge_comment_id=?`, out.SessionID, out.Row["reply_id"])
		if readErr != nil {
			return out, readErr
		}
		if len(inputs) > 0 {
			out.Guidance = inputs[0]
		}
	}
	// Normalize SQLite integers to JSON numbers, making comparison independent of
	// whether the image was just read or decoded from the retained proposal.
	data, marshalErr := json.Marshal(out)
	if marshalErr != nil {
		return out, marshalErr
	}
	if e == nil {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.UseNumber()
		e = decoder.Decode(&out)
	}
	return out, e
}

// PrepareDoctorGraphAction is read-only. The returned exact bytes must be
// displayed and approved before reservation; diagnosis alone authorizes nothing.
func (db *DB) PrepareDoctorGraphAction(ctx context.Context, ref, kind, target, reason string) (DoctorRepairAction, error) {
	var action DoctorRepairAction
	if !DoctorGraphRepairKind(kind) || strings.TrimSpace(reason) == "" || len(reason) > 4096 {
		return action, errors.New("invalid bounded graph proposal")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		before, e := graphTargetImage(ctx, tx, ref, kind, target, reason)
		if e != nil {
			return e
		}
		b, e := json.Marshal(before)
		if e != nil {
			return e
		}
		after := before
		after.Row = make(map[string]any, len(before.Row))
		for k, v := range before.Row {
			after.Row[k] = v
		}
		switch kind {
		case "graph_wait_cancel":
			after.Row["state"] = "cancelled"
			if e := graphPlanWaitGuidanceTx(ctx, tx, &after); e != nil {
				return e
			}
		case "graph_fail":
			after.Row["status"] = "failed"
			after.Row["finished_at"] = FormatTime(nowUTC())
			after.Row["next_due_at"] = nil
		default:
			after.Row["status"] = "cancelled"
			after.Row["finished_at"] = FormatTime(nowUTC())
			if kind == "graph_dispatch_cancel" {
				after.Row["cancellation_reason"] = reason
			}
		}
		a, e := json.Marshal(after)
		if e != nil {
			return e
		}
		action = DoctorRepairAction{Kind: kind, Before: string(b), After: string(a)}
		if !DoctorGraphRepairTarget(kind, target, action.Before, action.After) {
			return errors.New("graph proposal is not a bounded forward transition")
		}
		return nil
	})
	return action, err
}

func graphImagesEqual(a, b doctorGraphImage) bool {
	return a.SessionID == b.SessionID && a.InstanceID == b.InstanceID && a.Related == b.Related && a.Revision == b.Revision && reflect.DeepEqual(a.Row, b.Row) && reflect.DeepEqual(a.Guidance, b.Guidance) && reflect.DeepEqual(a.Relations, b.Relations) && a.Preconditions == b.Preconditions
}
func graphStepImages(s DoctorRepairStep) (doctorGraphImage, doctorGraphImage, error) {
	a, e := graphImage(s.Before)
	if e != nil {
		return a, doctorGraphImage{}, e
	}
	b, e := graphImage(s.After)
	return a, b, e
}

// ApplyDoctorGraphRepair commits the whole approved graph manifest, authenticated
// Wait handoff, exact verification and all numbered journals in one transaction.
func (db *DB) ApplyDoctorGraphRepair(ctx context.Context, id string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := doctorRepairAuthority(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.ExclusionID == "" {
			return ErrDoctorFenced
		}
		steps, e := graphStepsTx(ctx, tx, id)
		if e != nil {
			return e
		}
		if len(steps) == 0 {
			return errors.New("empty graph repair")
		}
		settled := 0
		for _, s := range steps {
			if s.State == "verified" {
				settled++
			}
		}
		if settled == len(steps) {
			return verifyGraphStepsTx(ctx, tx, r, steps)
		}
		if settled != 0 {
			return errors.New("partial graph journal requires inspection")
		}
		beforeImages := make(map[int]doctorGraphImage)
		afterImages := make(map[int]doctorGraphImage)
		for _, s := range steps {
			if !DoctorGraphRepairTarget(s.Kind, s.Target, s.Before, s.After) {
				return errors.New("invalid exact graph proposal")
			}
			a, b, e := graphStepImages(s)
			if e != nil {
				return e
			}
			current, e := graphTargetImage(ctx, tx, r.TaskRef, s.Kind, s.Target, a.Reason)
			if e != nil {
				return e
			}
			if !graphImagesEqual(a, current) {
				return errors.New("graph exact row or related precondition changed")
			}
			beforeImages[s.Number] = a
			afterImages[s.Number] = b
		}
		inverse := beforeImages[steps[0].Number].ReversalOf != ""
		for _, s := range steps {
			if (beforeImages[s.Number].ReversalOf != "") != inverse {
				return errors.New("mixed forward and inverse repair refused")
			}
		}
		if inverse {
			if e := validateGraphInverseTx(ctx, tx, r, steps, beforeImages, afterImages); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO doctor_repair_write_permits(repair_id) VALUES(?)`, id); e != nil {
			return e
		}
		ordered := append([]DoctorRepairStep(nil), steps...)
		rank := map[string]int{"graph_attempt_cancel": 0, "graph_dispatch_cancel": 1, "graph_wait_cancel": 2, "graph_fail": 3}
		sort.SliceStable(ordered, func(i, j int) bool {
			if inverse {
				return rank[ordered[i].Kind] > rank[ordered[j].Kind]
			}
			return rank[ordered[i].Kind] < rank[ordered[j].Kind]
		})
		for _, s := range ordered {
			a, b := beforeImages[s.Number], afterImages[s.Number]
			if !inverse {
				if e := graphRepairPreconditionTx(ctx, tx, s, a); e != nil {
					return e
				}
				if s.Kind == "graph_wait_cancel" {
					if e := graphWaitHandoffTx(ctx, tx, a); e != nil {
						return e
					}
				}
			}
			if e := graphWriteImageTx(ctx, tx, s, b); e != nil {
				return e
			}
			if inverse && s.Kind == "graph_wait_cancel" {
				if e := graphRestoreWaitGuidanceTx(ctx, tx, a, b); e != nil {
					return e
				}
			}
		}
		for _, s := range steps {
			a, b := beforeImages[s.Number], afterImages[s.Number]
			current, e := graphTargetImage(ctx, tx, r.TaskRef, s.Kind, s.Target, a.Reason)
			if e != nil {
				return e
			}
			if !reflect.DeepEqual(current.Row, b.Row) || !reflect.DeepEqual(current.Guidance, b.Guidance) {
				return errors.New("graph exact postimage verification failed")
			}
			current.ReversalOf = a.ReversalOf
			current.ReversalNumber = a.ReversalNumber
			observed, e := json.Marshal(current)
			if e != nil {
				return e
			}
			backup := []byte(s.Before)
			_, e = tx.ExecContext(ctx, `UPDATE doctor_repair_steps SET backup=?,backup_digest=?,backup_reference=?,observed_after=?,state='verified' WHERE repair_id=? AND number=? AND state IN('pending','backed_up')`, backup, doctorDigest(backup), fmt.Sprintf("database:doctor_repair_steps:%s:%d", id, s.Number), string(observed), id, s.Number)
			if e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM doctor_repair_write_permits WHERE repair_id=?`, id); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `UPDATE doctor_repairs SET state='applying',revision=revision+1,updated_at=? WHERE id=?`, FormatTime(nowUTC()), id)
		return e
	})
}
func graphStepsTx(ctx context.Context, tx *sql.Tx, id string) ([]DoctorRepairStep, error) {
	rows, e := tx.QueryContext(ctx, `SELECT number FROM doctor_repair_steps WHERE repair_id=? ORDER BY number`, id)
	if e != nil {
		return nil, e
	}
	var numbers []int
	for rows.Next() {
		var n int
		if e = rows.Scan(&n); e != nil {
			break
		}
		numbers = append(numbers, n)
	}
	e = errors.Join(e, rows.Err(), rows.Close())
	if e != nil {
		return nil, e
	}
	var out []DoctorRepairStep
	seen := map[string]bool{}
	for _, n := range numbers {
		s, e := doctorRepairStepTx(ctx, tx, id, n)
		if e != nil {
			return nil, e
		}
		if seen[s.Target] {
			return nil, errors.New("duplicate graph repair target")
		}
		seen[s.Target] = true
		if !DoctorGraphRepairKind(s.Kind) {
			return nil, errors.New("graph repair cannot mix unrelated effect kinds")
		}
		out = append(out, s)
	}
	return out, nil
}
func graphRepairPreconditionTx(ctx context.Context, tx *sql.Tx, s DoctorRepairStep, a doctorGraphImage) error {
	var n int
	var q string
	var args []any
	switch s.Kind {
	case "graph_attempt_cancel":
		q = `SELECT count(*) FROM runs WHERE id=? AND status IN('prelaunch','running')`
		args = []any{a.Row["run_id"]}
	case "graph_dispatch_cancel":
		q = `SELECT (SELECT count(*) FROM runs WHERE id=? AND status IN('prelaunch','running'))+(SELECT count(*) FROM graph_node_attempts WHERE status IN('pending','running') AND ((instance_id=? AND node=?) OR run_id=?))`
		args = []any{a.Row["allocated_run_id"], a.InstanceID, a.Row["node"], a.Row["allocated_run_id"]}
	case "graph_wait_cancel":
		q = `SELECT (SELECT count(*) FROM graph_node_attempts WHERE status IN('pending','running') AND (id=? OR instance_id=?))+(SELECT count(*) FROM graph_dispatches WHERE instance_id=? AND status IN('pending','reserved','running'))+(SELECT count(*) FROM runs r WHERE r.status IN('prelaunch','running') AND (EXISTS(SELECT 1 FROM graph_node_attempts a WHERE a.instance_id=? AND a.run_id=r.id) OR EXISTS(SELECT 1 FROM graph_dispatches d WHERE d.instance_id=? AND d.allocated_run_id=r.id)))`
		args = []any{a.Row["continuation_attempt_id"], a.InstanceID, a.InstanceID, a.InstanceID, a.InstanceID}
	case "graph_fail":
		q = `SELECT (SELECT count(*) FROM graph_node_attempts WHERE instance_id=? AND status IN('pending','running'))+(SELECT count(*) FROM graph_dispatches WHERE instance_id=? AND status IN('pending','reserved','running'))+(SELECT count(*) FROM graph_owner_waits WHERE instance_id=? AND state IN('waiting','claimed','allocated'))`
		args = []any{a.InstanceID, a.InstanceID, a.InstanceID}
	}
	args, e := graphSQLArguments(args)
	if e != nil {
		return e
	}
	if e := tx.QueryRowContext(ctx, q, args...).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return errors.New("graph repair refused: related work is active")
	}
	return nil
}
func graphWriteImageTx(ctx context.Context, tx *sql.Tx, s DoctorRepairStep, b doctorGraphImage) error {
	key := strings.TrimPrefix(s.Target, doctorRowPrefixes[s.Kind]+":")
	var result sql.Result
	var e error
	switch s.Kind {
	case "graph_dispatch_cancel":
		result, e = tx.ExecContext(ctx, `UPDATE graph_dispatches SET status=?,finished_at=?,cancellation_reason=? WHERE id=?`, b.Row["status"], b.Row["finished_at"], b.Row["cancellation_reason"], key)
	case "graph_wait_cancel":
		result, e = tx.ExecContext(ctx, `UPDATE graph_owner_waits SET state=? WHERE delivery_key=?`, b.Row["state"], key)
	case "graph_attempt_cancel":
		result, e = tx.ExecContext(ctx, `UPDATE graph_node_attempts SET status=?,finished_at=? WHERE id=?`, b.Row["status"], b.Row["finished_at"], key)
	case "graph_fail":
		result, e = tx.ExecContext(ctx, `UPDATE graph_instances SET status=?,finished_at=?,next_due_at=? WHERE id=?`, b.Row["status"], b.Row["finished_at"], b.Row["next_due_at"], key)
	}
	if e != nil {
		return e
	}
	return requireAffected(result, "exact graph repair row")
}
func graphWaitHandoffTx(ctx context.Context, tx *sql.Tx, a doctorGraphImage) error {
	planned := a
	if err := graphPlanWaitGuidanceTx(ctx, tx, &planned); err != nil {
		return err
	}
	if planned.Guidance == nil {
		return nil
	}
	reply, e := graphInteger(a.Row["reply_id"])
	if e != nil || reply < 1 {
		return errors.New("wait reply lacks exact source identity")
	}
	actor, e := graphInteger(a.Row["reply_author_id"])
	if e != nil || actor < 1 {
		return errors.New("wait reply lacks authenticated owner identity")
	}
	body, ok := a.Row["reply_body"].(string)
	if !ok || body == "" {
		return errors.New("wait reply lacks retained bytes")
	}
	// Existing assignments and optional targets remain untouched. Source matching
	// authenticates exact bytes even when no new pending binding is needed.
	var existing int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM owner_instruction_inputs o JOIN comments c ON c.id=o.comment_id WHERE o.session_id=? AND c.forge_comment_id=? AND c.author_user_id=? AND c.body=? AND c.classification=?`, a.SessionID, reply, actor, body, ClassificationInstruction).Scan(&existing)
	if e != nil {
		return e
	}
	if existing == 1 {
		return nil
	}
	target, e := ParseOwnerInputTarget(body)
	if e != nil {
		return e
	}
	_, e = retainOwnerInstructionTx(ctx, tx, a.SessionID, reply, actor, body, target)
	return e
}
func verifyGraphStepsTx(ctx context.Context, tx *sql.Tx, r DoctorRepair, steps []DoctorRepairStep) error {
	for _, s := range steps {
		var backup []byte
		if e := tx.QueryRowContext(ctx, `SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=?`, r.ID, s.Number).Scan(&backup); e != nil {
			return e
		}
		if string(backup) != s.Before || doctorDigest(backup) != s.BackupDigest || s.BackupReference != fmt.Sprintf("database:doctor_repair_steps:%s:%d", r.ID, s.Number) {
			return errors.New("graph backup integrity failed")
		}
		_, approved, e := graphStepImages(s)
		if e != nil {
			return e
		}
		observed, e := graphImage(s.ObservedAfter)
		if e != nil {
			return e
		}
		current, e := graphTargetImage(ctx, tx, r.TaskRef, s.Kind, s.Target, observed.Reason)
		if e != nil {
			return e
		}
		if s.State != "verified" || !graphImagesEqual(observed, current) || !reflect.DeepEqual(approved.Row, observed.Row) || !reflect.DeepEqual(approved.Guidance, observed.Guidance) {
			return errors.New("verified graph repair or related work changed")
		}
	}
	return nil
}

// VerifyDoctorGraphRepair confirms the complete journal and every exact image.
func (db *DB) VerifyDoctorGraphRepair(ctx context.Context, id string) error {
	return db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=?`, id))
		if e != nil {
			return e
		}
		steps, e := graphStepsTx(ctx, tx, id)
		if e != nil {
			return e
		}
		return verifyGraphStepsTx(ctx, tx, r, steps)
	})
}

func graphPlanWaitGuidanceTx(ctx context.Context, tx *sql.Tx, a *doctorGraphImage) error {
	if a.Row["reply_id"] == nil {
		return nil
	}
	replyID, e := graphInteger(a.Row["reply_id"])
	if e != nil {
		return e
	}
	c, e := scanComment(tx.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE session_id=? AND forge_comment_id=?`, a.SessionID, replyID))
	if e != nil {
		return e
	}
	if c.AuthorUserID < 1 || fmt.Sprint(c.AuthorUserID) != fmt.Sprint(a.Row["reply_author_id"]) || c.Body != a.Row["reply_body"] {
		return errors.New("wait reply source identity or bytes changed")
	}
	if c.Classification != ClassificationInstruction {
		return nil
	}
	if a.Guidance == nil {
		target, err := ParseOwnerInputTarget(c.Body)
		if err != nil {
			return err
		}
		a.Guidance = map[string]any{"comment_id": graphJSONInteger(c.ID), "session_id": graphJSONInteger(a.SessionID), "target_instance_id": graphJSONInteger(target.InstanceID), "target_node": target.Node, "target_launch_id": graphJSONInteger(target.LaunchID), "assigned_attempt_id": nil, "assigned_at": nil}
	}
	return nil
}
func graphRestoreWaitGuidanceTx(ctx context.Context, tx *sql.Tx, a, b doctorGraphImage) error {
	if reflect.DeepEqual(a.Guidance, b.Guidance) {
		return nil
	}
	if b.Guidance != nil || a.Guidance == nil || a.Guidance["assigned_attempt_id"] != nil {
		return errors.New("inverse cannot change delivered or existing guidance")
	}
	commentID, e := graphInteger(a.Guidance["comment_id"])
	if e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `DELETE FROM owner_instruction_inputs WHERE comment_id=? AND session_id=? AND assigned_attempt_id IS NULL`, commentID, a.SessionID)
	if e != nil {
		return e
	}
	return requireAffected(result, "inverse pending guidance")
}

// PrepareDoctorGraphRollback constructs one inverse of the entire retained
// verified repair. It neither restores rows nor approves their restoration.
func (db *DB) PrepareDoctorGraphRollback(ctx context.Context, ref, repairID, reason string) ([]DoctorRepairStep, error) {
	var out []DoctorRepairStep
	if strings.TrimSpace(reason) == "" || len(reason) > 4096 {
		return nil, errors.New("inverse requires a bounded reason")
	}
	err := db.withTx(ctx, func(tx *sql.Tx) error {
		r, e := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=? AND task_ref=?`, repairID, ref))
		if e != nil {
			return e
		}
		steps, e := graphStepsTx(ctx, tx, repairID)
		if e != nil {
			return e
		}
		if len(steps) == 0 {
			return errors.New("inverse requires a verified complete graph journal")
		}
		if e := verifyGraphStepsTx(ctx, tx, r, steps); e != nil {
			return e
		}
		for _, s := range steps {
			old, _, e := graphStepImages(s)
			if e != nil {
				return e
			}
			if old.ReversalOf != "" {
				return errors.New("an inverse cannot itself be reversed")
			}
			var backup []byte
			if e = tx.QueryRowContext(ctx, `SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=?`, repairID, s.Number).Scan(&backup); e != nil {
				return e
			}
			if doctorDigest(backup) != s.BackupDigest || string(backup) != s.Before {
				return errors.New("inverse backup integrity failed")
			}
			a, e := graphImage(s.ObservedAfter)
			if e != nil {
				return e
			}
			a.Reason = reason
			a.ReversalOf = repairID
			a.ReversalNumber = s.Number
			b := a
			b.Row = old.Row
			b.Guidance = old.Guidance
			before, e := json.Marshal(a)
			if e != nil {
				return e
			}
			after, e := json.Marshal(b)
			if e != nil {
				return e
			}
			if !DoctorGraphRepairTarget(s.Kind, s.Target, string(before), string(after)) {
				return errors.New("retained journal is not a bounded inverse")
			}
			out = append(out, DoctorRepairStep{Number: s.Number, Kind: s.Kind, Target: s.Target, Before: string(before), After: string(after)})
		}
		return nil
	})
	return out, err
}
func validateGraphInverseTx(ctx context.Context, tx *sql.Tx, r DoctorRepair, steps []DoctorRepairStep, before, after map[int]doctorGraphImage) error {
	source := before[steps[0].Number].ReversalOf
	prior, e := readDoctorRepair(tx.QueryRowContext(ctx, `SELECT `+doctorRepairColumns+` FROM doctor_repairs WHERE id=? AND task_ref=?`, source, r.TaskRef))
	if e != nil {
		return e
	}
	original, e := graphStepsTx(ctx, tx, source)
	if e != nil {
		return e
	}
	if len(original) != len(steps) {
		return errors.New("inverse must retain all dependent transitions")
	}
	if e := verifyGraphStepsTx(ctx, tx, prior, original); e != nil {
		return e
	}
	var reversals int
	e = tx.QueryRowContext(ctx, `SELECT count(*) FROM doctor_repair_steps WHERE repair_id<>? AND state='verified' AND json_valid(before_value) AND json_extract(before_value,'$.reversal_of')=?`, r.ID, source).Scan(&reversals)
	if e != nil {
		return e
	}
	if reversals != 0 {
		return errors.New("graph repair has already been reversed")
	}
	for index, s := range steps {
		a, b := before[s.Number], after[s.Number]
		old := original[index]
		if a.ReversalOf != source || a.ReversalNumber != old.Number || s.Kind != old.Kind || s.Target != old.Target {
			return errors.New("inverse journal binding changed")
		}
		pre, e := graphImage(old.Before)
		if e != nil {
			return e
		}
		post, e := graphImage(old.ObservedAfter)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(a.Row, post.Row) || !reflect.DeepEqual(a.Guidance, post.Guidance) || !reflect.DeepEqual(b.Row, pre.Row) || !reflect.DeepEqual(b.Guidance, pre.Guidance) {
			return errors.New("inverse changes unapproved columns")
		}
		var backup []byte
		if e = tx.QueryRowContext(ctx, `SELECT backup FROM doctor_repair_steps WHERE repair_id=? AND number=?`, source, old.Number).Scan(&backup); e != nil {
			return e
		}
		if doctorDigest(backup) != old.BackupDigest || string(backup) != old.Before {
			return errors.New("inverse backup integrity failed")
		}
	}
	return nil
}

func graphPreconditionDescription(kind string) string {
	switch kind {
	case "graph_dispatch_cancel":
		return "Cancel only if the allocated run is absent or stopped and no pending/running attempt owns this instance/node or allocated run; exact related rows and revision must match."
	case "graph_wait_cancel":
		return "Cancel only if no continuation attempt, dispatch or related allocated run is active; retain valid ordinary reply guidance, target and assignment atomically; exact related rows and revision must match."
	case "graph_attempt_cancel":
		return "Cancel only if the linked run is absent or stopped; exact related rows and revision must match."
	case "graph_fail":
		return "Fail only if no pending/running attempt, live dispatch or waiting/claimed/allocated owner wait remains after the dependent approved transitions; exact related rows and revision must match."
	}
	return ""
}
func graphRelatedSummary(ctx context.Context, tx *sql.Tx, sessionID int64) ([]doctorGraphRelation, error) {
	// Exact approval includes readable related identities and state. Large retained
	// content is represented by its immutable digest in the complete row-set hash.
	queries := []struct{ table, query string }{
		{"graph_instances", `SELECT * FROM graph_instances WHERE session_id=? ORDER BY id`},
		{"graph_node_attempts", `SELECT * FROM graph_node_attempts WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY id`},
		{"graph_dispatches", `SELECT * FROM graph_dispatches WHERE session_id=? ORDER BY id`},
		{"graph_owner_waits", `SELECT * FROM graph_owner_waits WHERE instance_id IN(SELECT id FROM graph_instances WHERE session_id=?) ORDER BY delivery_key`},
		{"runs", `SELECT * FROM runs WHERE session_id=? ORDER BY id`},
		{"research_launch_definitions", `SELECT * FROM research_launch_definitions WHERE session_id=? ORDER BY id`},
		{"owner_instruction_inputs", `SELECT * FROM owner_instruction_inputs WHERE session_id=? ORDER BY comment_id`},
	}
	retained := map[string]bool{"id": true, "instance_id": true, "session_id": true, "definition_id": true, "status": true, "state": true, "node": true, "current_node": true, "run_id": true, "allocated_run_id": true, "source_attempt_id": true, "continuation_attempt_id": true, "allowance_attempt_id": true, "launch_id": true, "attempt": true, "delivery_key": true, "reply_id": true, "reply_author_id": true, "claimed_at": true, "reserved_at": true, "finished_at": true, "started_at": true, "next_due_at": true, "due_at": true, "created_at": true, "content_digest": true, "topology_digest": true, "trigger_id": true, "comment_id": true, "assigned_attempt_id": true, "assigned_at": true, "target_instance_id": true, "target_node": true, "target_launch_id": true}
	var out []doctorGraphRelation
	for _, q := range queries {
		rows, e := graphRows(ctx, tx, q.query, sessionID)
		if e != nil {
			return nil, e
		}
		for _, row := range rows {
			for key := range row {
				if !retained[key] {
					delete(row, key)
				}
			}
		}
		out = append(out, doctorGraphRelation{Table: q.table, Rows: rows})
	}
	return out, nil
}

// graphInteger preserves SQLite identities throughout JSON proposal round trips.
// Floating point values are refused: their original integer cannot be recovered.
func graphInteger(value any) (int64, error) {
	switch v := value.(type) {
	case int64:
		return v, nil
	case json.Number:
		n, e := v.Int64()
		if e != nil {
			return 0, errors.New("graph identity is not an exact integer")
		}
		return n, nil
	default:
		return 0, errors.New("graph identity is not an exact integer")
	}
}
func graphJSONInteger(value int64) json.Number { return json.Number(strconv.FormatInt(value, 10)) }
func graphSQLArguments(args []any) ([]any, error) {
	for i, arg := range args {
		if number, ok := arg.(json.Number); ok {
			integer, e := number.Int64()
			if e != nil {
				return nil, errors.New("graph SQL identity is not an exact integer")
			}
			args[i] = integer
		}
	}
	return args, nil
}
