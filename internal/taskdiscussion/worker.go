package taskdiscussion

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Deadline supplies the installed finite provider budget to the shared coordinator.
func (s *Service) Deadline() time.Duration { return s.deadline }
func (s *Service) runRequest(a store.AdvisoryAdmission) (RunRequest, Principal, error) {
	var b Basis
	var p Principal
	err := json.Unmarshal([]byte(a.Basis), &b)
	if err == nil {
		err = json.Unmarshal([]byte(a.ConsentRevision), &p)
	}
	return RunRequest{AdmissionID: a.ID, JobID: a.JobID, ExecutionID: a.ExecutionID, Operation: a.Operation, Question: a.Question, Basis: b, Context: json.RawMessage(a.Context)}, p, err
}

// ExecuteClaim consumes a fenced shared-worker claim without owner-action authority.
func (s *Service) ExecuteClaim(ctx context.Context, a store.WorkflowAttempt, j store.WorkflowJob) error {
	admission, err := s.db.ReadAdvisoryForAttempt(ctx, a)
	if err != nil {
		return err
	}
	request, p, err := s.runRequest(admission)
	if err != nil {
		return err
	}
	finish := func(state string, answer Answer) error { return s.finish(ctx, a, state, answer, false) }
	budget := s.deadline
	var capture struct {
		Settings struct {
			Deadline time.Duration `json:"deadline"`
		} `json:"settings"`
	}
	if json.Unmarshal(request.Basis.Guidance, &capture) == nil && capture.Settings.Deadline > 0 {
		budget = min(budget, capture.Settings.Deadline)
	}
	if _, err = s.authorize(ctx, p, admission.TaskRef, AdvisoryScope); err != nil {
		return finish("failed", Answer{Answer: "Advisory consent is no longer available."})
	}
	current, err := s.db.ReadWorkflowJob(ctx, a.JobID)
	if err != nil {
		return err
	}
	if current.State == "stop_requested" {
		return finish("cancelled", Answer{Answer: "The advisory job stopped before provider launch."})
	}
	if err = s.db.SetWorkflowAttemptDeadline(ctx, a, a.StartedAt.Add(budget)); err != nil {
		current, readErr := s.db.ReadWorkflowJob(ctx, a.JobID)
		if readErr == nil && current.State == "stop_requested" {
			return finish("cancelled", Answer{Answer: "The advisory job stopped before provider launch."})
		}
		return err
	}
	a.Deadline = a.StartedAt.Add(budget)
	runCtx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type completion struct {
		answer Answer
		err    error
	}
	done := make(chan completion, 1)
	// One provider goroutine uses the finite claim context. The active count
	// keeps the shared coordinator idle until provider termination is observed.
	s.active.Add(1)
	go func() {
		defer s.active.Add(-1)
		answer, e := s.runner.Run(runCtx, request)
		done <- completion{answer, e}
	}()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	stopping := false
	for {
		select {
		case result := <-done:
			state := "succeeded"
			if result.err != nil {
				state = "failed"
				if stopping {
					state = "cancelled"
				}
				if result.answer.Answer == "" {
					result.answer.Answer = "The advisory invocation did not complete."
				}
			}
			// A returned success wins a stop race; a cancellation is confirmed only after return.
			if !s.now().Before(a.Deadline) {
				state = "uncertain"
			}
			return finish(state, result.answer)
		case <-ticker.C:
			current, e := s.db.ReadWorkflowJob(ctx, j.ID)
			if e != nil {
				cancel()
				return finish("uncertain", Answer{Answer: "Execution status could not be maintained."})
			}
			if current.State == "stop_requested" {
				stopping = true
				cancel()
			}
			if _, e = s.authorize(ctx, p, admission.TaskRef, AdvisoryScope); e != nil {
				stopping = true
				cancel()
			}
			if e = s.db.HeartbeatWorkflowAttempt(ctx, a, s.now()); e != nil {
				cancel()
				return finish("uncertain", Answer{Answer: "Execution claim could not be maintained."})
			}
		case <-ctx.Done():
			cancel()
			return finish("uncertain", Answer{Answer: "The worker stopped; execution completion is unconfirmed."})
		case <-runCtx.Done():
			// Give the isolated runner a bounded interval to confirm process termination.
			select {
			case result := <-done:
				if result.err == nil {
					return finish("succeeded", result.answer)
				}
				if stopping {
					return finish("cancelled", Answer{Answer: "The advisory runner confirmed it stopped."})
				}
				return finish("uncertain", Answer{Answer: "The advisory deadline ended execution."})
			case <-time.After(2 * time.Second):
				return finish("uncertain", Answer{Answer: "Stop requested; provider termination is unconfirmed."})
			}
		}
	}
}
func (s *Service) finish(ctx context.Context, a store.WorkflowAttempt, state string, answer Answer, recovery bool) error {
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	raw, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	if len(raw) > 120<<10 {
		state = "uncertain"
		answer = Answer{Answer: "The result exceeded retention limits.", Gaps: []string{"oversized_result"}}
		raw, _ = json.Marshal(answer)
	}
	// Protect the serialized payload before retention; field names are fixed schema.
	safe, err := s.protect(save, string(raw))
	if err != nil || !json.Valid([]byte(safe)) {
		state = "uncertain"
		safe = `{"answer":"The result could not be safely retained.","sources":[],"gaps":["redaction_failed"]}`
	}
	f := store.WorkflowFinish{State: state, Result: safe, Questions: "[]", Summary: state}
	if recovery || !s.now().Before(a.Deadline) {
		if !recovery {
			f.State = "uncertain"
		}
		return s.db.ReconcileWorkflowAttempt(save, a, f, s.now())
	}
	return s.db.FinishAdvisory(save, a, f, s.now())
}

// RecoverClaim reads receipts only; an interrupted paid invocation is never relaunched.
func (s *Service) RecoverClaim(ctx context.Context, a store.WorkflowAttempt) error {
	admission, err := s.db.ReadAdvisoryForAttempt(ctx, a)
	if err != nil {
		return err
	}
	request, _, err := s.runRequest(admission)
	if err != nil {
		return err
	}
	answer, found, e := s.runner.Recover(ctx, request)
	state := "uncertain"
	if found && e == nil {
		state = "succeeded"
	} else {
		answer = Answer{Answer: "The prior invocation has no conclusive receipt; it was not rerun."}
	}
	return s.finish(ctx, a, state, answer, true)
}

// RunOnce is useful for installations with only the advisory coordinator. The
// database claim serializes it with every other durable workflow coordinator.
func (s *Service) RunOnce(ctx context.Context) (bool, error) {
	if !s.Idle() {
		return false, nil
	}
	a, j, err := s.db.ClaimWorkflowAttempt(ctx, s.now(), s.deadline)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if a.AdmissionKind != "advisory" {
		return true, errors.New("shared workflow coordinator required for workflow jobs")
	}
	return true, s.ExecuteClaim(ctx, a, j)
}

// Idle reports whether all previously launched provider processes have returned.
func (s *Service) Idle() bool { return s.active.Load() == 0 }
