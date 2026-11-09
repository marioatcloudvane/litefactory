package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// joinSessionCleanup combines the runner's own outcome with a session
// cleanup fault (session.Close, which recovers only this session's own
// run/attempt per AC-003). Kept as its own seam, rather than inlined in the
// defer, so a later change distinguishing runner failure from
// cleanup/finalization failure can be layered on without re-deriving the
// surrounding defer.
//
// That layering is harvest.go's job (issue #311, AC-005), not this seam's:
// per [agent.RunResult]'s own contract, the *result* returned alongside this
// joined error already says everything a caller needs to tell the two apart
// — a non-nil result means the subprocess actually started and ran, so any
// error joined in here (from session.Close, or from an inner MCP-resource
// close already joined into runnerErr by runMCPBound) is necessarily a
// post-run cleanup/finalization/recovery fault, never a startup failure.
// harvest.go's describeFactoryFailure is what turns that into the phase-aware
// report; this function only combines the faults.
func joinSessionCleanup(runnerErr, cleanupErr error) error {
	return errors.Join(runnerErr, cleanupErr)
}

func runSubmissionBound(ctx context.Context, prep *preparation, req agent.Request) (result *agent.RunResult, err error) {
	if prep.submissionOptions == nil {
		req.Prompt += prep.workerTiming
		return runMCPBound(ctx, prep, req, nil)
	}
	req.Prompt += prep.workerTiming
	session, err := StartSubmissionSession(ctx, *prep.submissionOptions)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return runMCPBound(ctx, prep, req, nil)
	}
	defer func() {
		// Accepted submission recovery retains its own durable publication
		// authority. Bound this attempt's join without cancelling that work or
		// starting a second recovery operation.
		err = joinSessionCleanup(err, closeInvocation(ctx, func() error { return session.Close(context.WithoutCancel(ctx)) }))
	}()
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	descriptor := session.DescriptorPath()
	access, err := json.Marshal(struct{ Executable, Descriptor string }{executable, descriptor})
	if err != nil {
		return nil, err
	}
	// The helper writes its correlation journal and opens its socket beside the
	// descriptor, so a run confined to its worktree needs this one directory
	// admitted as well. Naming the broker's own directory keeps that as narrow
	// as the protocol allows: not LF_HOME, not the submission root shared with
	// every other attempt.
	req.SubmissionRoot = filepath.Dir(descriptor)
	// The transport's rules live in the result-protocol skill (legacy routes);
	// the prompt carries the access paths and the stage as data.
	req.Prompt += "\n\nFactory submission access (data): " + string(access) + "\nContribution stage: " + session.binding.Stage + "\n"
	if session.binding.Stage == "validate" {
		req.Prompt += "Shell helpers: unavailable; final route: terminal contributions JSON\n"
	}
	result, err = runMCPBound(ctx, prep, req, session)
	if err == nil && result != nil && result.Succeeded() && session.binding.Stage == "validate" && !prep.submissionValidated {
		if processErr := session.consumeTerminalValidation(context.WithoutCancel(ctx), result.ResultText); processErr != nil {
			result = result.Clone()
			result.Outcome = agent.OutcomeFailed
			result.Reason = "Typed validation contribution rejected: " + processErr.Error()
		} else {
			prep.submissionValidated = true
			result = result.Clone()
			result.ResultText = "Typed validation findings retained. Remote issue projection remains pending; this retained-output path did not demonstrate live helper acknowledgment."
		}
	}
	return result, err
}
func (s *SubmissionSession) consumeTerminalValidation(ctx context.Context, text string) error {
	if len(text) > agent.MaxContributionBytes {
		return fmt.Errorf("validation metadata exceeds limit")
	}
	var output struct {
		Contributions []json.RawMessage `json:"contributions"`
	}
	decoder := json.NewDecoder(bytes.NewBufferString(text))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("validation output requires one contributions object")
	}
	token, err = decoder.Token()
	if err != nil || token != "contributions" {
		return fmt.Errorf("validation output allows only contributions")
	}
	if err := decoder.Decode(&output.Contributions); err != nil {
		return err
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return fmt.Errorf("validation output has duplicate or unknown fields")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("validation output has trailing data")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(output.Contributions) != len(s.stories) {
		return fmt.Errorf("incomplete validation story coverage")
	}
	if s.options.Progress != nil {
		// Scoped to this session's own run/attempt (AC-003): an unrelated
		// run's broken pending receipt must not abort terminal Validate intake.
		if err := s.options.Progress.RecoverSubmissionsForAttempt(ctx, s.binding.RunID, s.binding.Attempt); err != nil {
			return err
		}
	}
	seen := map[string]bool{}
	for i, payload := range output.Contributions {
		contribution, diagnostics := agent.ParseContribution(payload, s.scope)
		if !diagnostics.Valid || seen[contribution.Story] {
			return fmt.Errorf("invalid or duplicate validation contribution %d", i)
		}
		seen[contribution.Story] = true
		// Check every retained live decision before writing the first terminal
		// decision. The session lock excludes new live checkpoint mutations.
		if _, _, err := s.reconcileValidationCheckpoint(ctx, contribution); err != nil {
			return err
		}
	}
	for i, payload := range output.Contributions {
		result, err := s.checkpointLocked(ctx, submission.Request{Method: "submit", RequestID: fmt.Sprintf("terminal-%s-%d", s.binding.Attempt, i), Payload: payload}, true)
		if err != nil {
			return err
		}
		var receipt store.SubmissionReceipt
		if err := json.Unmarshal(result, &receipt); err != nil {
			return fmt.Errorf("decode authoritative validation receipt: %w", err)
		}
		if receipt.ID == 0 || receipt.RunID != s.binding.RunID || receipt.Attempt != s.binding.Attempt || receipt.InputState != "valid" || receipt.EngineeringState != "not_applicable" || (receipt.AcceptanceState != "accepted" && receipt.AcceptanceState != "unverified") {
			return fmt.Errorf("validation contribution %d lacks a recorded authoritative decision", i)
		}
	}
	return nil
}
