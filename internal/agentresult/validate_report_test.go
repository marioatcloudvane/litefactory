package agentresult

import (
	"strings"
	"testing"
)

func reportScope() *ReportScope {
	return &ReportScope{
		Items: map[string][]string{
			"story-001:AC-001": {ReportActionCheckPass, ReportActionCheckFail},
			"story-001:plan":   {ReportActionRepair, ReportActionIssueBug, ReportActionIssueStory, ReportActionDefer},
			"failure-1":        {ReportActionSendBack, ReportActionRerun, ReportActionFactoryBug, ReportActionWaitForMain},
		},
		Global:     []string{ReportActionSendBack, ReportActionRerun},
		Evidence:   map[string]bool{"suite-95-1": true},
		Permalinks: []string{"https://github.com/o/r/blob/0123456789012345678901234567890123456789/"},
	}
}

func reportContext(scope *ReportScope) Context {
	return Context{SchemaVersion: SchemaVersion, Mode: "validate", WorkflowVersion: 2, Report: scope}
}

func goodReport() *ValidateReport {
	return &ValidateReport{
		Mode:       ReportModeReport,
		Headline:   "One test fails in the turn package; it also fails on main.",
		WhatFailed: []string{"TestImplementGraphRun in internal/turn"},
		Why:        "The test waits for an owner reply the new code never records.",
		Certainty:  "likely",
		Next:       &ReportItem{Ref: "failure-1", Action: ReportActionWaitForMain, Recommendation: "Wait until main is fixed, then bring main in.", Diagnosis: DiagnosisAlreadyFailing, Evidence: "rerun-1"},
		Items: []ReportItem{
			{Ref: "story-001:AC-001", Action: ReportActionCheckPass, Recommendation: "Open an invoice upload and confirm the dialog shows the result.", Diagnosis: DiagnosisNone},
			{Ref: "story-001:plan", Action: ReportActionDefer, Recommendation: "The staged checks are optional; defer them.", Diagnosis: DiagnosisNone, Evidence: "suite-95-1"},
		},
		Reruns: []ReportRerun{{Command: "go test ./internal/turn -run TestImplementGraphRun", Commit: "base", Result: "failed"}},
	}
}

func reportResult(r *ValidateReport) Result {
	return Result{Outcome: "completed", Comment: "Summary written.", Payload: Payload{Validate: &Validate{Report: r}}}
}

func TestReporterResultAccepted(t *testing.T) {
	if d := ValidateResult(reportResult(goodReport()), reportContext(reportScope())); !d.Valid || !d.Complete {
		t.Fatalf("valid report refused: %+v", d.Diagnostics)
	}
}

func TestReporterResultRefusals(t *testing.T) {
	cases := map[string]func(r *ValidateReport){
		"bad action":         func(r *ValidateReport) { r.Items[0].Action = "approve" },
		"action not allowed": func(r *ValidateReport) { r.Items[0].Action = ReportActionShip },
		"unknown item":       func(r *ValidateReport) { r.Items[0].Ref = "story-009:AC-001" },
		"duplicate item":     func(r *ValidateReport) { r.Items[1] = r.Items[0] },
		"bad diagnosis":      func(r *ValidateReport) { r.Items[0].Diagnosis = "guess" },
		"embedded command": func(r *ValidateReport) {
			r.Items[0].Recommendation = "Comment `/validate-check 4b5a1a1a8da3 story-001:AC-001 pass` when done."
		},
		"bare command":       func(r *ValidateReport) { r.Headline = "Run /validate-ship now" },
		"decision reference": func(r *ValidateReport) { r.Why = "Use d-0123456789ab to ship." },
		"link":               func(r *ValidateReport) { r.Why = "See https://example.com/x for details." },
		"foreign permalink":  func(r *ValidateReport) { r.Items[0].Evidence = "https://example.com/blob/x" },
		"marker":             func(r *ValidateReport) { r.Headline = "<!-- lf:delivery v=1 -->" },
		"oversized headline": func(r *ValidateReport) { r.Headline = strings.Repeat("a", MaxReportHeadline+1) },
		"multiline headline": func(r *ValidateReport) { r.Headline = "one\ntwo" },
		"too many failures":  func(r *ValidateReport) { r.WhatFailed = []string{"a", "b", "c", "d"} },
		"oversized recommendation": func(r *ValidateReport) {
			r.Items[0].Recommendation = strings.Repeat("word ", MaxReportRecommendation)
		},
		"too many reruns": func(r *ValidateReport) {
			r.Reruns = append(r.Reruns, ReportRerun{Command: "go test ./a", Commit: "tested", Result: "passed"}, ReportRerun{Command: "go test ./b", Commit: "tested", Result: "passed"}, ReportRerun{Command: "go test ./c", Commit: "tested", Result: "passed"})
		},
		"two base reruns": func(r *ValidateReport) {
			r.Reruns = append(r.Reruns, ReportRerun{Command: "go test ./a", Commit: "base", Result: "passed"})
		},
		"unknown evidence":   func(r *ValidateReport) { r.Items[0].Evidence = "suite-1-1" },
		"rerun not made":     func(r *ValidateReport) { r.Items[0].Evidence = "rerun-2" },
		"missing next":       func(r *ValidateReport) { r.Next = nil },
		"global not allowed": func(r *ValidateReport) { r.Next.Ref, r.Next.Action = "", ReportActionShip },
		"why without certainty": func(r *ValidateReport) {
			r.Certainty = ""
		},
		"answer in report mode": func(r *ValidateReport) { r.Answer = "Yes." },
		"wrong mode":            func(r *ValidateReport) { r.Mode = ReportModeFollowUp },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := goodReport()
			mutate(r)
			if d := ValidateResult(reportResult(r), reportContext(reportScope())); d.Valid {
				t.Fatalf("refusal expected for %s", name)
			}
		})
	}
}

func TestReporterTextAllowsPathsAndPermalinks(t *testing.T) {
	r := goodReport()
	r.Why = "The loop in internal/turn/validate.go:42 never ends; it compares against /tmp/state."
	r.Items[1].Evidence = "https://github.com/o/r/blob/0123456789012345678901234567890123456789/internal/turn/x.go#L4"
	if d := ValidateResult(reportResult(r), reportContext(reportScope())); !d.Valid {
		t.Fatalf("paths and permalinks refused: %+v", d.Diagnostics)
	}
}

func TestReporterFollowUp(t *testing.T) {
	scope := reportScope()
	scope.FollowUp = true
	answer := &ValidateReport{Mode: ReportModeFollowUp, Answer: "The failure is on main too, so waiting for main is the safest step."}
	if d := ValidateResult(reportResult(answer), reportContext(scope)); !d.Valid {
		t.Fatalf("plain answer refused: %+v", d.Diagnostics)
	}
	revised := goodReport()
	revised.Mode, revised.Answer = ReportModeFollowUp, "Yes: send it back instead."
	if d := ValidateResult(reportResult(revised), reportContext(scope)); !d.Valid {
		t.Fatalf("revised recommendation refused: %+v", d.Diagnostics)
	}
	blank := &ValidateReport{Mode: ReportModeFollowUp}
	if d := ValidateResult(reportResult(blank), reportContext(scope)); d.Valid {
		t.Fatal("follow-up without an answer accepted")
	}
}

func TestReportPayloadOnlyForReporter(t *testing.T) {
	ctx := reportContext(nil)
	ctx.ReviewRole = "code"
	if d := ValidateResult(reportResult(goodReport()), ctx); d.Valid {
		t.Fatal("a reviewer was allowed to send a report")
	}
	repair := reportContext(nil)
	repair.RepairRole = true
	if d := ValidateResult(reportResult(goodReport()), repair); d.Valid {
		t.Fatal("the repair agent was allowed to send a report")
	}
}
