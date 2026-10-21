package turn

import (
	"context"
	"fmt"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// captureImplementStandards retains guidance before a mechanical coordinator can
// start workers. A graph launch without an agent run gets a retained negative
// invocation anchor, rather than inventing an executing run.
func captureImplementStandards(ctx context.Context, opts Options, req Request, launchID int64, newLaunch bool) {
	var root int64
	allowInitial := newLaunch && !req.StandardsHandoff
	if req.StandardsHandoff && req.StandardsRootRunID != 0 {
		root = req.StandardsRootRunID
	} else if !req.StandardsHandoff {
		if run, found, err := opts.DB.GetRunByTrigger(ctx, req.SessionID, req.Trigger.CommentID); err == nil && found {
			if run.Status != store.RunPrelaunch {
				allowInitial = false
			}
			if resolved, resolveErr := opts.DB.ResolveStandardsRoot(ctx, run.ID); resolveErr == nil {
				root = resolved
			} else {
				allowInitial = false
			}
		} else if err != nil {
			allowInitial = false
		}
	}
	retainedRoot, err := opts.DB.RecordStandardsGraphInvocation(ctx, launchID, root)
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Warn("standards invocation identity could not be retained")
		}
		return
	}
	runtime := standards.Runtime{Store: opts.DB, RootRunID: fmt.Sprint(retainedRoot), ConsumerKey: standardsSessionConsumerKey(req.SessionID, req.mode(), "coordinator")}
	// Only a genuinely new owner launch resolves current. A private worker,
	// recovery, or already admitted launch can only read the retained binding.
	status, err := runtime.Bind(ctx, allowInitial)
	if (err != nil || status.StatusPersistenceFailure != "") && opts.Logger != nil {
		opts.Logger.Warn("standards invocation status could not be retained")
	}
}
