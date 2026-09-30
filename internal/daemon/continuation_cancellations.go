package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// Notices have independent acknowledgements: the original interruption may
// already have announced a deadline before its continuation became unusable.
func (d *Daemon) replayContinuationCancellations(ctx context.Context, repos []store.Repo) error {
	notices, err := d.db.ListUnreportedContinuationCancellations(ctx)
	if err != nil {
		return err
	}
	registered := make(map[int64]bool, len(repos))
	for _, repo := range repos {
		registered[repo.ID] = repo.Forge == store.ForgeGitHub
	}
	var faults []error
	for _, notice := range notices {
		session, err := d.db.GetSession(ctx, notice.SessionID)
		if err != nil {
			faults = append(faults, err)
			continue
		}
		if !registered[session.RepoID] {
			continue
		}
		if err := d.replayContinuationCancellation(ctx, notice, session); err != nil {
			if isRecoveryPending(err) {
				continue
			}
			faults = append(faults, err)
		}
	}
	return errors.Join(faults...)
}

func (d *Daemon) replayContinuationCancellation(ctx context.Context, notice store.Continuation, session store.Session) (err error) {
	allowed, gateErr := d.guardedRecoveryAllowed(ctx, store.ReportRecovery{Key: recoveryKey(session.ID, notice.TriggerID()), SessionID: session.ID, TriggerID: notice.TriggerID()}, recoveryTime(ctx))
	if gateErr != nil {
		return gateErr
	}
	if !allowed {
		return &recoveryPending{}
	}
	if !d.enterDoctorGuarded(ctx, session.ID) {
		return nil
	}
	defer d.inflight.leave(session.ID)
	acquired, err := d.db.AcquireSessionLock(ctx, session.ID, d.instanceID)
	if err != nil || !acquired {
		return err
	}
	defer func() {
		err = errors.Join(err, d.db.ReleaseSessionLock(context.WithoutCancel(ctx), session.ID, d.instanceID))
	}()
	// Re-read under the task lock so a second process cannot acknowledge and
	// release the same notice while this caller waits for ownership.
	pending, err := d.db.ListUnreportedContinuationCancellations(ctx)
	if err != nil {
		return err
	}
	owed := false
	for _, c := range pending {
		if c.ID == notice.ID {
			notice = c
			owed = true
			break
		}
	}
	if !owed {
		return nil
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	source, err := d.db.GetRun(ctx, notice.SourceRunID)
	if err != nil {
		return err
	}
	reason := fmt.Sprintf("Automatic recovery action %d was cancelled: %s", notice.ID, notice.CancellationReason)
	posted, err := d.reportRun(ctx, turn.RunReport{
		SessionID: session.ID, CommentID: notice.TriggerID(),
		Run: report.Run{Ref: ref, Ordinal: source.Ordinal, Agent: source.Agent, Branch: session.Branch, WorktreePath: notice.Context.Worktree, LogPath: source.LogPath,
			Result:   &agent.RunResult{Outcome: agent.OutcomeInterrupted, ExitCode: agent.SignalExitCode, Reason: reason, StartedAt: source.StartedAt, FinishedAt: notice.FinishedAt},
			Recovery: &report.Recovery{ManualReason: reason}},
	})
	if err != nil {
		return fmt.Errorf("report cancellation of automatic action %d: %w", notice.ID, err)
	}
	if posted.CommentID == 0 {
		return nil
	}
	return d.db.MarkContinuationCancellationReported(context.WithoutCancel(ctx), notice.ID, posted.CommentID)
}
