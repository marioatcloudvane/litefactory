package store

import (
	"context"
	"database/sql"
	"fmt"
)

type planOwnerAdmissionKey struct{}
type planOwnerAdmission struct{ parent, story string }

// WithPlanOwnerAdmission keeps GitHub decisions under the same transaction
// guard as an admitted chat revision. Empty story covers the assembled set.
func WithPlanOwnerAdmission(ctx context.Context, parent, story string) context.Context {
	return context.WithValue(ctx, planOwnerAdmissionKey{}, planOwnerAdmission{parent: parent, story: story})
}

func planOwnerAdmissionTx(ctx context.Context, tx *sql.Tx) error {
	in, ok := ctx.Value(planOwnerAdmissionKey{}).(planOwnerAdmission)
	if !ok {
		return nil
	}
	return stageDecisionPlanRevisionPendingTx(ctx, tx, in.parent, in.story)
}
func stageDecisionPlanRevisionPendingTx(ctx context.Context, tx *sql.Tx, parent, story string) error {
	var count int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM stage_decision_receipts r JOIN graph_dispatches d ON d.id=r.dispatch_id JOIN graph_instances i ON i.id=d.instance_id WHERE r.state='applied' AND json_extract(r.target,'$.operation')='revise_task' AND json_extract(r.target,'$.workflow')='plan' AND json_extract(r.target,'$.plan_parent')=? AND (?='' OR json_extract(r.target,'$.story_key')=?) AND i.status='running' AND d.status IN ('pending','reserved','running')`, parent, story, story).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: a confirmed Plan revision is still pending; inspect its decision status", ErrStageDecisionTarget)
	}
	return nil
}
