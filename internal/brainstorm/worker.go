package brainstorm

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Plain failure reasons kept on a finished scan.
const (
	reasonConsent    = "repository:brainstorm consent was withdrawn, so the scan stopped"
	reasonClosed     = "the brainstorm was closed, so the scan stopped"
	reasonStopped    = "the scan was stopped on request"
	reasonFailed     = "the scan did not produce a valid answer"
	reasonOversized  = "the scan result was larger than 120 KiB"
	reasonDeadline   = "the scan ran out of time; ask again"
	reasonUnsure     = "the scan's completion could not be confirmed; ask again"
	reasonNoReceipt  = "the scan was interrupted with no saved answer and was not rerun; ask again"
	reasonProtection = "the scan result could not be safely kept"
)

// Kind is the shared durable job kind this coordinator executes.
func (s *Service) Kind() string { return AdmissionKind }

// Deadline supplies the finite scan budget to the shared coordinator.
func (s *Service) Deadline() time.Duration { return s.deadline }

// Idle reports whether every launched scan run has returned.
func (s *Service) Idle() bool { return s.active.Load() == 0 }

func (s *Service) runRequest(ctx context.Context, sc store.BrainstormScanRun) (RunRequest, Principal, error) {
	var p Principal
	var team TeamCapture
	if err := json.Unmarshal([]byte(sc.ConsentRevision), &p); err != nil {
		return RunRequest{}, p, err
	}
	if err := json.Unmarshal([]byte(sc.TeamFiles), &team); err != nil {
		return RunRequest{}, p, err
	}
	entries, earlier, err := s.db.BrainstormScanContext(ctx, sc.BrainstormID, sc.ID)
	if err != nil {
		return RunRequest{}, p, err
	}
	digest, files, err := buildScanContext(sc.BrainstormID, sc.RepositoryRef, entries, earlier)
	if err != nil {
		return RunRequest{}, p, err
	}
	return RunRequest{ScanID: sc.ID, JobID: sc.JobID, ExecutionID: sc.ExecutionID, BrainstormID: sc.BrainstormID, RepositoryRef: sc.RepositoryRef, Question: sc.Question, Purpose: sc.Purpose, Team: team, Digest: digest, ContextFiles: files}, p, nil
}

// stopReason reports why a running scan must stop, or "" while it may continue.
func (s *Service) stopReason(ctx context.Context, p Principal, sc store.BrainstormScanRun) (string, error) {
	job, err := s.db.ReadWorkflowJob(ctx, sc.JobID)
	if err != nil {
		return "", err
	}
	if job.State == "stop_requested" {
		return reasonStopped, nil
	}
	state, err := s.db.BrainstormState(ctx, sc.BrainstormID)
	if err != nil {
		return "", err
	}
	if state != "open" {
		return reasonClosed, nil
	}
	if s.consent != nil {
		if withdrawn := s.consent(ctx, p, sc.RepositoryRef); withdrawn != nil {
			return reasonConsent, nil //nolint:nilerr // Withdrawn consent is a stop reason, not a store failure.
		}
	}
	return "", nil
}

// ExecuteClaim runs one fenced brainstorm_scan claim without owner-action authority.
func (s *Service) ExecuteClaim(ctx context.Context, a store.WorkflowAttempt, j store.WorkflowJob) error {
	if a.AdmissionKind != AdmissionKind || s.runner == nil {
		return store.ErrConnectorInvalid
	}
	sc, err := s.db.ReadBrainstormScanForAttempt(ctx, a)
	if err != nil {
		return err
	}
	request, p, err := s.runRequest(ctx, sc)
	if err != nil {
		return err
	}
	if reason, e := s.stopReason(ctx, p, sc); e != nil {
		return e
	} else if reason != "" {
		return s.finish(ctx, a, store.BrainstormScanFinish{State: "cancelled", FailureReason: reason}, false)
	}
	if err = s.db.SetWorkflowAttemptDeadline(ctx, a, a.StartedAt.Add(s.deadline)); err != nil {
		current, readErr := s.db.ReadWorkflowJob(ctx, a.JobID)
		if readErr == nil && current.State == "stop_requested" {
			return s.finish(ctx, a, store.BrainstormScanFinish{State: "cancelled", FailureReason: reasonStopped}, false)
		}
		return err
	}
	a.Deadline = a.StartedAt.Add(s.deadline)
	runCtx, cancel := context.WithDeadline(ctx, a.Deadline)
	defer cancel()
	type completion struct {
		result RunResult
		err    error
	}
	done := make(chan completion, 1)
	// The active count keeps the shared coordinator busy until the runner returns.
	s.active.Add(1)
	go func() {
		defer s.active.Add(-1)
		result, e := s.runner.Run(runCtx, request)
		done <- completion{result, e}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	stop := ""
	for {
		select {
		case c := <-done:
			if !s.now().Before(a.Deadline) {
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonDeadline}, false)
			}
			if c.err != nil {
				if stop != "" {
					return s.finish(ctx, a, store.BrainstormScanFinish{State: "cancelled", FailureReason: stop}, false)
				}
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "failed", FailureReason: reasonFailed}, false)
			}
			// A returned success wins a stop race.
			return s.finishResult(ctx, a, sc, c.result, false)
		case <-ticker.C:
			reason, e := s.stopReason(ctx, p, sc)
			if e != nil {
				cancel()
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonUnsure}, false)
			}
			if reason != "" && stop == "" {
				stop = reason
				cancel()
			}
			if e = s.db.HeartbeatWorkflowAttempt(ctx, a, s.now()); e != nil {
				cancel()
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonUnsure}, false)
			}
		case <-ctx.Done():
			cancel()
			return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonUnsure}, false)
		case <-runCtx.Done():
			// Give the runner a bounded interval to confirm it stopped.
			select {
			case c := <-done:
				if c.err == nil {
					return s.finishResult(ctx, a, sc, c.result, false)
				}
				if stop != "" {
					return s.finish(ctx, a, store.BrainstormScanFinish{State: "cancelled", FailureReason: stop}, false)
				}
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonDeadline}, false)
			case <-time.After(2 * time.Second):
				return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonUnsure}, false)
			}
		}
	}
}

// finishResult revalidates and protects a returned result before keeping it.
func (s *Service) finishResult(ctx context.Context, a store.WorkflowAttempt, sc store.BrainstormScanRun, r RunResult, recovery bool) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(raw) > MaxResultBytes {
		return s.finish(ctx, a, store.BrainstormScanFinish{State: "failed", FailureReason: reasonOversized}, recovery)
	}
	drafts := sc.Purpose == PurposeDrafts
	if len(AnswerDiagnostics(r.Answer)) != 0 || (!drafts && !json.Valid(r.Research)) || (drafts && len(r.Drafts) == 0) {
		return s.finish(ctx, a, store.BrainstormScanFinish{State: "failed", FailureReason: reasonFailed}, recovery)
	}
	f := store.BrainstormScanFinish{State: "succeeded", BasisCommit: r.Commit, Cost: r.Cost}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	protect := func(text string) (string, error) {
		if s.protect == nil || text == "" {
			return text, nil
		}
		return s.protect(save, text)
	}
	if f.Answer, err = protect(r.Answer); err == nil {
		f.Research, err = protect(string(r.Research))
	}
	if drafts && err == nil {
		if f.Drafts, err = draftSetInput(r, protect); err == nil {
			f.Drafts.Summary = f.Answer
		}
	}
	if err == nil && !drafts && !json.Valid([]byte(f.Research)) {
		err = errors.New("protected research is not JSON")
	}
	for _, q := range r.Questions {
		if err != nil {
			break
		}
		var safe string
		if safe, err = protect(q); err == nil {
			f.Questions = append(f.Questions, safe)
		}
	}
	if err != nil {
		return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonProtection}, recovery)
	}
	if len(r.Usage) > 0 {
		f.Usage = string(r.Usage)
	}
	if f.Cost == "" {
		f.Cost = CostUnknown
	}
	if err = s.finish(ctx, a, f, recovery); err != nil || !drafts {
		return err
	}
	// The duplicate search is advice only: its failure never undoes the set.
	search, cancelSearch := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
	defer cancelSearch()
	_ = s.searchDuplicates(search, sc.ID, sc.RepositoryRef)
	return nil
}

func (s *Service) finish(ctx context.Context, a store.WorkflowAttempt, f store.BrainstormScanFinish, recovery bool) error {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if !recovery && !s.now().Before(a.Deadline) && !a.Deadline.IsZero() {
		f = store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonDeadline}
		recovery = true
	}
	if f.Cost == "" {
		f.Cost = CostUnknown
	}
	return s.db.FinishBrainstormScan(save, a, f, s.now(), recovery)
}

// RecoverClaim reads receipts only; an interrupted scan is never relaunched.
func (s *Service) RecoverClaim(ctx context.Context, a store.WorkflowAttempt) error {
	if a.AdmissionKind != AdmissionKind || s.runner == nil {
		return store.ErrConnectorInvalid
	}
	sc, err := s.db.ReadBrainstormScanForAttempt(ctx, a)
	if err != nil {
		return err
	}
	request, _, err := s.runRequest(ctx, sc)
	if err != nil {
		return err
	}
	result, found, err := s.runner.Recover(ctx, request)
	if err != nil {
		return err
	}
	if !found {
		return s.finish(ctx, a, store.BrainstormScanFinish{State: "uncertain", FailureReason: reasonNoReceipt}, true)
	}
	return s.finishResult(ctx, a, sc, result, true)
}
