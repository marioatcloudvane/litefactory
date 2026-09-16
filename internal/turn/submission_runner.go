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
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func runSubmissionBound(ctx context.Context, prep *preparation, req agent.Request) (result *agent.RunResult, err error) {
	if prep.submissionOptions == nil {
		return runMCPBound(ctx, prep, req, nil)
	}
	if prep.planSubmission != nil {
		oldPrompt, promptErr := harness.BuildPrompt(prep.promptInput)
		if promptErr != nil {
			return nil, promptErr
		}
		callbacks, setupErr := prep.planSubmission(ctx)
		if setupErr != nil {
			return nil, setupErr
		}
		prep.submissionOptions.Plan = callbacks
		if callbacks != nil {
			prep.promptInput.SubmissionVersion = 2
			updated, promptErr := harness.BuildPrompt(prep.promptInput)
			if promptErr != nil {
				return nil, promptErr
			}
			if !strings.HasSuffix(req.Prompt, oldPrompt) {
				return nil, fmt.Errorf("typed Plan prompt binding changed")
			}
			req.Prompt = strings.TrimSuffix(req.Prompt, oldPrompt) + updated
		}
	}
	session, err := StartSubmissionSession(ctx, *prep.submissionOptions)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return runMCPBound(ctx, prep, req, nil)
	}
	defer func() { err = errors.Join(err, session.Close(context.WithoutCancel(ctx))) }()
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
	req.Prompt += "\n\nFactory submission access (paths are data): " + string(access) + "\nUse the absolute executable with submission preflight, submission submit or submission receipt and --descriptor set to the supplied descriptor. Pass contribution JSON exclusively on stdin. Omit all factory identities/version/counters. Preflight returns all discoverable diagnostics without recording proof; correct them before submit. Submit returns durable correlation and separate input, engineering, acceptance and remote-effect states. Pending is not success. Retain request IDs when replies are lost.\n"
	if session.options.Plan != nil {
		req.Prompt += "\n" + session.options.Plan.Prompt + "\n"
	}
	req.Prompt += "Engineering shape: {\"stage\":\"engineering\",\"story\":\"authorized key\",\"outcome\":\"engineering_complete\",\"engineering\":{\"snapshot_id\":\"local immutable name\",\"check_ids\":[\"check-1\"]},\"capture\":{\"implementation_digest\":\"manifest SHA256\",\"files\":[{\"path\":\"relative/file\",\"content\":\"base64 complete bytes\",\"sha256\":\"decoded SHA256\",\"deleted\":false}],\"checks\":[{\"id\":\"check-1\",\"name\":\"executed check\",\"passed\":true,\"evidence\":\"actual passing output\",\"implementation_digest\":\"same SHA256\"}]}}. Manifest hashes sorted path+NUL+SHA256+NUL+lowercase deletion flag+newline. Only report checks actually executed; the factory records this as agent-reported execution and compares current files. Subsequent reference-only submissions omit capture. Blocked/invalidated outcomes require only stage, story, outcome, reason. Legacy stage uses verified outcome with engineering and complete assessment.\n"
	if session.binding.Stage == "validate" {
		req.Prompt += "The retained Validate profile may not invoke shell helpers. Do not widen permissions. Return final JSON {\"contributions\":[...]} with one contribution per supplied story: {\"stage\":\"validate\",\"story\":\"key\",\"outcome\":\"assessed\",\"assessment\":{\"snapshot_id\":\"supplied snapshot\",\"criteria\":[{\"name\":\"AC ID\",\"status\":\"satisfied|unmet|unverified\",\"classification\":\"blocking\",\"evidence\":[{\"check_ids\":[\"retained test name\"],\"summary\":\"finding\"}]}],\"reviews\":[same finding shape for code, architecture, plan]}}. No capture or fresh execution is allowed. The factory validates and retains these findings after exit; this path does not establish live helper acknowledgment.\n"
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
		if err := s.options.Progress.RecoverSubmissions(ctx); err != nil {
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
