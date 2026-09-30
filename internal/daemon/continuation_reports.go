package daemon

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// replayInterruptionReports discovers report debt independently of the owner
// cursor. It runs synchronously on the tick and never allocates execution work.
func (d *Daemon) replayInterruptionReports(ctx context.Context, repos []store.Repo) error {
	runs, err := d.db.ListPendingInterruptionReports(ctx)
	if err != nil {
		return fmt.Errorf("list pending interruption reports: %w", err)
	}
	registered := make(map[int64]bool, len(repos))
	for _, repo := range repos {
		registered[repo.ID] = repo.Forge == store.ForgeGitHub
	}
	var failures []error
	for _, run := range runs {
		if err := ctx.Err(); err != nil {
			return errors.Join(append(failures, err)...)
		}
		session, err := d.db.GetSession(ctx, run.SessionID)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if !registered[session.RepoID] {
			continue
		}
		if err := d.replayInterruptionReport(ctx, run, session); err != nil {
			if isRecoveryPending(err) {
				continue
			}
			failures = append(failures, err)
			var rate *github.RateLimitError
			if errors.As(err, &rate) {
				break
			}
		}
	}
	return errors.Join(failures...)
}

func (d *Daemon) replayInterruptionReport(ctx context.Context, run store.Run, session store.Session) (err error) {
	allowed, gateErr := d.guardedRecoveryAllowed(ctx, store.ReportRecovery{Key: recoveryKey(session.ID, run.TriggerCommentID), SessionID: session.ID, TriggerID: run.TriggerCommentID}, recoveryTime(ctx))
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
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return err
	}
	exit := agent.SignalExitCode
	if run.ExitCode != nil {
		exit = int(*run.ExitCode)
	}
	_, err = d.reportRun(ctx, turn.RunReport{
		Run: report.Run{Ref: ref, Ordinal: run.Ordinal, Agent: run.Agent, Branch: session.Branch, WorktreePath: session.WorktreePath, LogPath: run.LogPath,
			Result: &agent.RunResult{Outcome: agent.OutcomeInterrupted, Reason: run.Error, ExitCode: exit, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt}},
		SessionID: session.ID, CommentID: run.TriggerCommentID, RunID: run.ID,
	})
	if err != nil {
		return fmt.Errorf("replay interruption report for run %d: %w", run.ID, err)
	}
	return nil
}
