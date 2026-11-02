package turn

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/store"
)

// StepCapacity owns the current worker's reservation independently of worker
// lifetime. Replace is atomic and nonblocking; Release is idempotent.
type StepCapacity interface {
	Replace(context.Context, int) (bool, error)
	Release()
	Capacity() (used, total int)
}

// StepCapacityGrower is an optional StepCapacity extension. TryGrow takes
// extra slots of the reservation's pool without waiting and without touching
// the grant already held; refusal leaves the reservation unchanged. The
// returned release is idempotent, and releasing the whole reservation also
// returns every extra slot still held, so a worker that unwinds cannot leak.
type StepCapacityGrower interface {
	TryGrow(context.Context, int) (release func(), ok bool, err error)
}

type stepCapacityKey struct{}

// WithStepCapacity attaches the factory-owned reservation to a worker.
func WithStepCapacity(ctx context.Context, capacity StepCapacity) context.Context {
	return context.WithValue(ctx, stepCapacityKey{}, capacity)
}

func stepCapacity(ctx context.Context) StepCapacity {
	capacity, _ := ctx.Value(stepCapacityKey{}).(StepCapacity)
	return capacity
}

// WeightedValidateSlots is the shared capacity adapter for nested execution.
// Independent reviewers cost one; a child workflow uses its declared step cost.
type WeightedValidateSlots interface {
	TryAcquireWeighted(context.Context, int) (func(), bool, error)
	Capacity() (used, total int)
}

func capacityDiagnostic(pool, workflow, node string, cost, used, total int) string {
	refusal := "temporary capacity deferral"
	if cost > total {
		refusal = "impossible capacity fit"
	}
	return fmt.Sprintf("%s: pool=%s workflow=%s node=%s requested=%d used=%d available=%d total=%d", refusal, pool, workflow, node, cost, used, total-used, total)
}

// GraphDispatchSlotCost is the capacity a dispatch must hold to start: the
// admission cost of its retained node. A Plan parallel group starts on one
// member lane and grows from there (see [graphexec.AdmissionSlotCost]).
func GraphDispatchSlotCost(ctx context.Context, db *store.DB, dispatch store.GraphDispatch) (int, error) {
	need, err := GraphDispatchSlotNeed(ctx, db, dispatch)
	return need.Admission, err
}

// SlotNeed is a node's admission cost and the full cost it may grow to.
type SlotNeed struct {
	Admission, Full int
}

// Deferral names a refused admission plainly. A parallel group that cannot
// get even its minimum says so, because it no longer waits for its full cost.
func (n SlotNeed) Deferral(total int) string {
	if n.Admission > total {
		return "impossible capacity fit"
	}
	if n.Admission < n.Full {
		return fmt.Sprintf("temporary capacity deferral (parallel group cannot get even its minimum of %d slot(s) to run its members one at a time; it would use up to %d)", n.Admission, n.Full)
	}
	return "temporary capacity deferral"
}

// GraphDispatchSlotNeed reads the retained definition, never a mutable team
// workflow. A missing or unreadable retained graph refuses admission.
func GraphDispatchSlotNeed(ctx context.Context, db *store.DB, dispatch store.GraphDispatch) (SlotNeed, error) {
	profile, err := retainedDispatchProfile(ctx, db, dispatch)
	if err != nil {
		return SlotNeed{}, err
	}
	full, err := graphexec.SlotCost(profile, dispatch.Node)
	if err != nil {
		return SlotNeed{}, err
	}
	admission, err := graphexec.AdmissionSlotCost(profile, dispatch.Node)
	if err != nil {
		return SlotNeed{}, err
	}
	return SlotNeed{Admission: admission, Full: full}, nil
}

func retainedDispatchProfile(ctx context.Context, db *store.DB, dispatch store.GraphDispatch) (*graphexec.Profile, error) {
	instance, err := db.GetGraphInstance(ctx, dispatch.InstanceID)
	if err != nil {
		return nil, err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return nil, err
	}
	var capture store.WorkflowCapture
	if dispatch.LaunchID > 0 {
		capture, err = db.GetWorkflowCapture(ctx, dispatch.LaunchID)
	} else {
		trigger := int64(0)
		if dispatch.ID > 0 {
			trigger = dispatch.TriggerID()
		}
		capture, err = db.WorkflowCaptureForAdmission(ctx, instance.SessionID, trigger, pin.Mode)
	}
	if err != nil {
		return nil, err
	}
	if capture.InstanceID != instance.ID || capture.SessionID != instance.SessionID {
		return nil, fmt.Errorf("retained slot-cost capture belongs to another workflow instance")
	}
	raw := capture.Files
	var files map[string][]byte
	if err = json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	graphFile := capture.Provenance.GraphFile
	if graphFile == "" {
		graphFile, err = retainedGraphFile(ctx, db, dispatch.InstanceID)
		if err != nil {
			return nil, err
		}
	}
	dot, found := files[graphFile]
	if !found {
		return nil, fmt.Errorf("retained graph %q is absent", graphFile)
	}
	return graphexec.ParseProfile(dot)
}

// admitGraphCapacity runs before preparation and again at the activity boundary.
// The second check adopts the same live grant instead of charging twice.
func admitGraphCapacity(ctx context.Context, opts Options, instance, launch int64, node, workflow string) (bool, string, error) {
	capacity := stepCapacity(ctx)
	if capacity == nil {
		return true, "", nil
	}
	need, err := GraphDispatchSlotNeed(ctx, opts.DB, store.GraphDispatch{InstanceID: instance, LaunchID: launch, Node: node})
	if err != nil {
		return false, "", fmt.Errorf("read pinned node slot cost: %w", err)
	}
	cost := need.Admission
	admitted, err := capacity.Replace(ctx, cost)
	if err != nil {
		return false, "", err
	}
	if admitted {
		return true, "", nil
	}
	used, total := capacity.Capacity()
	return false, fmt.Sprintf("%s: pool=shared workflow=%s node=%s requested=%d used=%d available=%d total=%d", need.Deferral(total), workflow, node, cost, used, total-used, total), nil
}

func parkCapacityDeferred(ctx context.Context, opts Options, req Request, instance int64, node string, launch int64) error {
	now := opts.MCP.Now()
	if spent, ok := store.GraphTriggerDispatch(req.Trigger.CommentID); ok {
		_, executed, err := opts.DB.GetRunByTrigger(ctx, req.SessionID, req.Trigger.CommentID)
		if err != nil {
			return err
		}
		if !executed {
			return opts.DB.DeferGraphDispatchForCapacity(context.WithoutCancel(ctx), spent, now.Add(opts.GraphRetryDelay))
		}
		// This dispatch actually executed an earlier agent. Preserve its run and
		// allocate a fresh continuation for the next node, which has not begun.
		_, err = opts.DB.ParkCapturedGraphInstanceAfter(context.WithoutCancel(ctx), instance, node, now.Add(opts.GraphRetryDelay), now, spent, launch)
		return err
	}
	_, err := opts.DB.ParkCapturedGraphInstanceAfter(context.WithoutCancel(ctx), instance, node, now.Add(opts.GraphRetryDelay), now, 0, launch)
	return err
}
