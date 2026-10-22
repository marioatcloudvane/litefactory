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

// GraphDispatchSlotCost reads the retained definition, never a mutable team
// workflow. A missing or unreadable retained graph refuses admission.
func GraphDispatchSlotCost(ctx context.Context, db *store.DB, dispatch store.GraphDispatch) (int, error) {
	instance, err := db.GetGraphInstance(ctx, dispatch.InstanceID)
	if err != nil {
		return 0, err
	}
	pin, err := db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return 0, err
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
		return 0, err
	}
	if capture.InstanceID != instance.ID || capture.SessionID != instance.SessionID {
		return 0, fmt.Errorf("retained slot-cost capture belongs to another workflow instance")
	}
	raw := capture.Files
	var files map[string][]byte
	if err = json.Unmarshal(raw, &files); err != nil {
		return 0, err
	}
	graphFile := capture.Provenance.GraphFile
	if graphFile == "" {
		graphFile, err = retainedGraphFile(ctx, db, dispatch.InstanceID)
		if err != nil {
			return 0, err
		}
	}
	dot, found := files[graphFile]
	if !found {
		return 0, fmt.Errorf("retained graph %q is absent", graphFile)
	}
	profile, err := graphexec.ParseProfile(dot)
	if err != nil {
		return 0, err
	}
	return graphexec.SlotCost(profile, dispatch.Node)
}

// admitGraphCapacity runs before preparation and again at the activity boundary.
// The second check adopts the same live grant instead of charging twice.
func admitGraphCapacity(ctx context.Context, opts Options, instance, launch int64, node, workflow string) (bool, string, error) {
	capacity := stepCapacity(ctx)
	if capacity == nil {
		return true, "", nil
	}
	cost, err := GraphDispatchSlotCost(ctx, opts.DB, store.GraphDispatch{InstanceID: instance, LaunchID: launch, Node: node})
	if err != nil {
		return false, "", fmt.Errorf("read pinned node slot cost: %w", err)
	}
	admitted, err := capacity.Replace(ctx, cost)
	if err != nil {
		return false, "", err
	}
	if admitted {
		return true, "", nil
	}
	used, total := capacity.Capacity()
	return false, capacityDiagnostic("shared", workflow, node, cost, used, total), nil
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
