package doctor

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type retainedResultRunner struct {
	t         *testing.T
	calls     int
	directory string
}

func (r *retainedResultRunner) IsolationEvidence(context.Context, string) (string, error) {
	return "controlled test adapter only", nil
}
func (r *retainedResultRunner) Run(ctx context.Context, in Invocation, results *ResultService) (InvocationOutcome, error) {
	r.calls++
	r.directory = in.WorkingDirectory
	info, err := os.Stat(in.WorkingDirectory)
	if err != nil {
		r.t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 || in.MeterAttempt.RunID != 0 || in.MeterAttempt.DoctorAttemptID != in.ID || in.ConfiguredModel == "" {
		r.t.Fatalf("unsafe invocation: %+v mode=%v", in, info.Mode())
	}
	if !strings.Contains(in.Prompt, testDoctorGuide(r.t)) {
		r.t.Fatal("missing field guide")
	}
	d := Diagnosis{Findings: []Finding{{Explanation: "No workflow session is available.", Uncertainty: "Diagnosis is limited by missing sources."}}, CoverageGaps: append([]string(nil), results.evidence.Gaps...)}
	raw, err := json.Marshal(ResultInput{Diagnosis: d})
	if err != nil {
		return InvocationOutcome{}, err
	}
	pre, err := results.Preflight(ctx, raw)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if !pre.Valid {
		r.t.Fatalf("invalid fixture: %+v", pre)
	}
	id := "controlled-" + in.ID
	receipt, err := results.Submit(ctx, id, raw)
	if err != nil {
		return InvocationOutcome{}, err
	}
	replay, err := results.Receipt(ctx, id)
	if err != nil {
		return InvocationOutcome{}, err
	}
	if receipt.RequestID != replay.RequestID {
		r.t.Fatal("receipt identity changed")
	}
	return InvocationOutcome{RequestID: id, ObservedModel: in.ConfiguredModel, ProviderVersion: "test-only"}, nil
}
func TestDoctorInvocationAdoptsDurableResultWithSharedMeter(t *testing.T) {
	s := doctorIOService(t)
	runner := &retainedResultRunner{t: t}
	s.Runner = runner
	target, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "Explain", SourceID: "controlled-source"})
	if err != nil {
		t.Fatal(err)
	}
	if runner.calls != 1 || got.Diagnosis == nil || len(got.Attempts) != 1 || got.Attempts[0].State != "completed" || got.Attempts[0].ObservedModel != got.Attempts[0].ConfiguredModel {
		t.Fatalf("not durably adopted: %+v", got)
	}
	if _, err := os.Stat(runner.directory); !os.IsNotExist(err) {
		t.Fatalf("private scratch retained: %v", err)
	}
	_, err = s.Diagnose(t.Context(), DiagnoseRequest{Target: target, Instructions: "edited replay", SourceID: "controlled-source"})
	if err == nil || !strings.Contains(err.Error(), "source already admitted") || runner.calls != 1 {
		t.Fatalf("duplicate source not refused: %v", err)
	}
}
