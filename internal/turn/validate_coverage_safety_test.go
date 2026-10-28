package turn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

type coverageGuardScratch struct {
	recipe     []byte
	tree       string
	closed     bool
	closeErr   error
	closeCalls int
}

func (s *coverageGuardScratch) Path() string                            { return "/scratch/coverage-guard" }
func (s *coverageGuardScratch) Recipe(context.Context) ([]byte, error)  { return s.recipe, nil }
func (s *coverageGuardScratch) Observe(context.Context) (string, error) { return s.tree, nil }
func (s *coverageGuardScratch) Close(context.Context) error {
	s.closed = true
	s.closeCalls++
	return s.closeErr
}

type coverageGuardRepo struct{ scratch *coverageGuardScratch }

func (r coverageGuardRepo) Begin(context.Context, string) (ValidateSuiteScratch, error) {
	return r.scratch, nil
}

func coverageGuardFixture(t *testing.T) (ValidateSuiteBindings, *coverageGuardScratch) {
	t.Helper()
	f := newSuiteFixture(t)
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture]\n  focused:\n    commands:\n      - argv: [fixture]\n  full:\n    commands:\n      - argv: [go, test, ./...]\n")
	scratch := &coverageGuardScratch{recipe: recipe, tree: "original-tree"}
	f.b.Repo = coverageGuardRepo{scratch}
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "guard", SourceKey: "request", Policy: "full", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "guard", Policy: "full", AssessmentVersion: coverage.Version}, nil
	}
	facts := map[int]coverage.Facts{0: {Runner: coverage.RunnerGo, Version: "fixture-v1", Structured: true, Dependencies: true, Fresh: true}}
	f.b.Coverage = &ValidateCoverageBindings{Conditions: "guarded", Discover: func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{Commit: suiteCommit, Complete: true, Units: []coverage.Unit{{Key: "unit", Command: 0, Runner: coverage.RunnerGo, Selector: "example.test/unit", Files: []string{"unit.go"}}}}, facts, nil
	}}
	f.b.Run = func(_ context.Context, _ string, _ config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		exit := 0
		result := bugchecks.CommandResult{Outcome: store.SuitePassed, Exit: &exit, Started: suiteClock, CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "fixture-process", Output: guardGoReport(map[string]string{"TestA": "pass"})}
		if err := started(result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	return f.b, scratch
}

func guardGoReport(cases map[string]string) string {
	var out strings.Builder
	out.WriteString("{\"Action\":\"start\",\"Package\":\"example.test/unit\"}\n")
	failed := false
	for _, name := range []string{"TestA", "TestB"} {
		status := cases[name]
		if status == "" {
			continue
		}
		fmt.Fprintf(&out, "{\"Action\":\"run\",\"Package\":\"example.test/unit\",\"Test\":%q}\n", name)
		if status == "fail" {
			failed = true
			fmt.Fprintf(&out, "{\"Action\":\"output\",\"Package\":\"example.test/unit\",\"Test\":%q,\"Output\":\"assertion at unit_test.go:12\\n\"}\n", name)
		}
		fmt.Fprintf(&out, "{\"Action\":%q,\"Package\":\"example.test/unit\",\"Test\":%q}\n", status, name)
	}
	status := "pass"
	if failed {
		status = "fail"
	}
	fmt.Fprintf(&out, "{\"Action\":%q,\"Package\":\"example.test/unit\"}\n", status)
	return out.String()
}

func guardExecute(t *testing.T, b ValidateSuiteBindings, instance int64) (ActivityResult, validateSuiteOutput, error) {
	t.Helper()
	res, err := (ValidateSuiteActivity{b}).Execute(t.Context(), Options{}, ActivityRequest{Mode: agent.ModeValidate, InstanceID: instance, Node: "suite"})
	var out validateSuiteOutput
	if len(res.Output) > 0 {
		if e := json.Unmarshal(res.Output, &out); e != nil {
			t.Fatal(e)
		}
	}
	return res, out, err
}

func TestValidateCoverageSafetyInfrastructureCannotEarnPassingAssessment(t *testing.T) {
	b, _ := coverageGuardFixture(t)
	inner := b.Run
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		r := inner(ctx, path, c, started)
		exit := 2
		r.Exit = &exit
		r.Outcome = store.SuiteBlocked
		r.Reason = "fixture infrastructure failure"
		return r
	}
	res, out, err := guardExecute(t, b, 701)
	if err != nil || res.Outcome != "blocked" || out.Assessment == nil || out.Assessment.Complete {
		t.Fatalf("infrastructure settled passing: %+v %+v %v", res, out, err)
	}
	if !strings.Contains(out.Reason, "fixture infrastructure failure") {
		t.Fatalf("blocked command reason was lost: %+v", out)
	}
}

func TestValidateCoverageSafetyUnknownCleanupPreservesRunningClaim(t *testing.T) {
	b, scratch := coverageGuardFixture(t)
	inner := b.Run
	calls := 0
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls++
		r := inner(ctx, path, c, started)
		r.CleanupConfirmed = false
		r.Outcome = store.SuiteBlocked
		return r
	}
	b.Stopped = func(context.Context, string) (bool, error) { return false, nil }
	res, _, err := guardExecute(t, b, 702)
	if err != nil || res.Outcome != "blocked" || scratch.closed {
		t.Fatalf("uncertain process scratch discarded: %s closed=%v %v", res.Outcome, scratch.closed, err)
	}
	run, err := b.DB.RunningSuiteRun(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	commands, err := b.DB.SuiteCommands(t.Context(), run.ID)
	if err != nil || len(commands) != 1 || commands[0].State != store.SuiteRunning {
		t.Fatalf("process claim lost: %+v %v", commands, err)
	}
	again, _, err := guardExecute(t, b, 703)
	if err != nil || again.Outcome != "blocked" || calls != 1 {
		t.Fatalf("overlapping work admitted: %s calls=%d %v", again.Outcome, calls, err)
	}
}

func TestValidateCoverageSafetyDiscoveryDriftStopsBeforeTests(t *testing.T) {
	b, scratch := coverageGuardFixture(t)
	discover := b.Coverage.Discover
	b.Coverage.Discover = func(ctx context.Context, s ValidateSuiteScratch, c []config.BugCheckCommand, run coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		inv, facts, err := discover(ctx, s, c, run)
		scratch.tree = "changed-tree"
		return inv, facts, err
	}
	calls := 0
	inner := b.Run
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls++
		return inner(ctx, path, c, started)
	}
	res, _, err := guardExecute(t, b, 704)
	if err != nil || res.Outcome != "blocked" || calls != 0 {
		t.Fatalf("tests ran after discovery drift: %s calls=%d %v", res.Outcome, calls, err)
	}
}

func TestValidateCoverageSafetyRetriesNewlyDiscoveredFailureOnce(t *testing.T) {
	b, _ := coverageGuardFixture(t)
	inner := b.Run
	calls := 0
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls++
		r := inner(ctx, path, c, started)
		exit := 1
		r.Exit = &exit
		r.Outcome = store.SuiteFailed
		switch calls {
		case 1:
			r.Output = guardGoReport(map[string]string{"TestA": "fail"})
		case 2:
			r.Output = guardGoReport(map[string]string{"TestA": "pass", "TestB": "fail"})
		case 3:
			r.Output = guardGoReport(map[string]string{"TestB": "pass"})
			exit = 0
			r.Outcome = store.SuitePassed
		default:
			t.Fatalf("extra retry %d", calls)
		}
		return r
	}
	res, out, err := guardExecute(t, b, 705)
	if err != nil || res.Outcome != "covered" || calls != 3 || out.Assessment == nil || !out.Assessment.Complete || len(out.Assessment.Comparisons) != 2 {
		t.Fatalf("new failure retry: outcome=%s calls=%d assessment=%+v %v", res.Outcome, calls, out.Assessment, err)
	}
}

func TestValidateCoverageSafetyCheckoutCleanupFailureCannotReplayPass(t *testing.T) {
	b, scratch := coverageGuardFixture(t)
	scratch.closeErr = errors.New("fixture checkout cleanup refused")
	calls := 0
	inner := b.Run
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls++
		return inner(ctx, path, c, started)
	}
	res, out, err := guardExecute(t, b, 706)
	if err != nil || res.Outcome != "blocked" || scratch.closeCalls != 1 || out.Assessment == nil || out.Assessment.Complete {
		t.Fatalf("checkout cleanup passed: %+v closes=%d %v", out, scratch.closeCalls, err)
	}
	run, err := b.DB.GetSuiteRun(t.Context(), out.RunID)
	if err != nil || run.State != store.SuiteBlocked {
		t.Fatalf("cleanup result not retained: %+v %v", run, err)
	}
	again, _, err := guardExecute(t, b, 706)
	if err != nil || again.Outcome != "blocked" || calls != 1 || scratch.closeCalls != 1 {
		t.Fatalf("cleanup replay: outcome=%s calls=%d closes=%d %v", again.Outcome, calls, scratch.closeCalls, err)
	}
}

func TestValidateCoverageSafetyOversizedAssessmentRetainsBoundedSummary(t *testing.T) {
	b, _ := coverageGuardFixture(t)
	inner := b.Run
	diagnostic := strings.Repeat("a", 1500<<10)
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		r := inner(ctx, path, c, started)
		r.Output = strings.Replace(guardGoReport(map[string]string{"TestA": "fail"}), "assertion at unit_test.go:12", diagnostic, 1)
		exit := 1
		r.Exit = &exit
		r.Outcome = store.SuiteFailed
		return r
	}
	res, out, err := guardExecute(t, b, 707)
	if err != nil || res.Outcome != "blocked" || out.Assessment == nil || out.Assessment.Complete || !strings.Contains(out.Assessment.FullReason, "assessment exceeded") {
		t.Fatalf("oversized assessment not bounded: outcome=%s assessment=%+v %v", res.Outcome, out.Assessment, err)
	}
	retained, err := b.DB.ValidationEvidence(t.Context(), out.AssessmentID)
	if err != nil || len(retained.Payload) > store.MaxValidationEvidenceBytes || len(retained.References) != 4 {
		t.Fatalf("bounded summary sources: bytes=%d refs=%d %v", len(retained.Payload), len(retained.References), err)
	}
	for _, id := range retained.References {
		artifact, err := b.DB.ValidationEvidence(t.Context(), id)
		if err != nil || len(artifact.Payload) > store.MaxValidationEvidenceBytes {
			t.Fatalf("raw or normalized artifact missing: %v", err)
		}
	}
}

func TestValidateCoverageSafetyRejectsDetachedRequestAuthority(t *testing.T) {
	for _, fault := range []string{"commit", "lineage", "policy"} {
		t.Run(fault, func(t *testing.T) {
			b, _ := coverageGuardFixture(t)
			bind := b.Bind
			b.Bind = func(ctx context.Context, opts Options, req ActivityRequest) (ValidateSuiteAuthority, error) {
				auth, err := bind(ctx, opts, req)
				switch fault {
				case "commit":
					auth.Commit = strings.Repeat("f", 40)
				case "lineage":
					auth.Lineage = "different-lineage"
				case "policy":
					auth.Policy = "auto"
				}
				return auth, err
			}
			calls := 0
			inner := b.Run
			b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
				calls++
				return inner(ctx, path, c, started)
			}
			_, _, err := guardExecute(t, b, 708)
			if err == nil || calls != 0 {
				t.Fatalf("detached request admitted: calls=%d %v", calls, err)
			}
		})
	}
}

func TestValidateCoverageSafetyMarksContractBeforeExecutionAndBlocksLegacyReuse(t *testing.T) {
	b, _ := coverageGuardFixture(t)
	inner := b.Run
	b.Run = func(ctx context.Context, path string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		marker, err := b.DB.LatestValidationEvidence(ctx, "guard", "manifest")
		if err != nil || marker.Complete || marker.RunID < 1 {
			t.Fatalf("coverage execution started without durable contract marker: %+v %v", marker, err)
		}
		return inner(ctx, path, c, started)
	}
	res, _, err := guardExecute(t, b, 709)
	if err != nil || res.Outcome != "passed" {
		t.Fatalf("coverage result: %s %v", res.Outcome, err)
	}
	if _, err := b.DB.ReuseSuiteRun(t.Context(), 710, "suite", suiteCommit, suiteClock); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("coverage run reused through legacy path: %v", err)
	}
}

func TestValidateCoverageSafetyMetadataErrorFinishesRawSuiteBlocked(t *testing.T) {
	b, _ := coverageGuardFixture(t)
	b.Coverage.Discover = func(context.Context, ValidateSuiteScratch, []config.BugCheckCommand, coverage.DiscoveryRunner) (coverage.Inventory, map[int]coverage.Facts, error) {
		return coverage.Inventory{}, nil, errors.New("fixture metadata retention failure")
	}
	res, out, err := guardExecute(t, b, 711)
	if err != nil || res.Outcome != "blocked" {
		t.Fatalf("metadata failure: %s %v", res.Outcome, err)
	}
	run, err := b.DB.GetSuiteRun(t.Context(), out.RunID)
	if err != nil || run.State != store.SuiteBlocked {
		t.Fatalf("metadata failure raw suite: %+v %v", run, err)
	}
}
