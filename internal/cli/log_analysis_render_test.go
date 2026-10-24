package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestShowGroupsFindingsByWhoCanFixThem(t *testing.T) {
	finding := func(key, kind string, file bool) loganalysis.Finding {
		return loganalysis.Finding{Key: key, Kind: kind, Target: key, Title: "Title " + key, Expected: "e", Actual: "a", Cause: "cause " + key, Suggestion: "s", IncidentIDs: []string{"i1"}, File: file}
	}
	report := loganalysis.Report{Summary: "s", Findings: []loganalysis.Finding{finding("setup", loganalysis.FindingProjectSetup, true), finding("defect", loganalysis.FindingFactoryDefect, true), finding("role", loganalysis.FindingRolePrompt, false)}}
	for _, dry := range []bool{false, true} {
		scope := loganalysis.Scope{Request: loganalysis.Request{DryRun: dry}, Repositories: []loganalysis.Repository{{ID: 1, Name: "acme/app", Team: "mine@1.0.0", TeamOwned: true}}}
		raw, _ := json.Marshal(report)
		view := loganalysis.Inspection{Invocation: store.LogAnalysis{ID: "A1", State: "completed", Scope: mustJSON(t, scope)}, Report: raw}
		if dry {
			view.Filings = []store.LogAnalysisFiling{{FindingKey: "setup", Destination: "github:acme/app", State: store.FilingWouldFile, Title: "Title setup", Body: "would-be body"}, {FindingKey: "defect", Destination: "upstream", State: store.FilingWouldFile, Reason: "no upstream tracker is configured", Title: "Title defect", Body: "upstream body"}}
		} else {
			view.Filings = []store.LogAnalysisFiling{{FindingKey: "setup", Destination: "github:acme/app", State: store.FilingFiled, Reference: "acme/app#5", URL: "https://github.test/acme/app/issues/5"}, {FindingKey: "defect", Destination: "upstream", State: store.FilingNotFiled, Reason: "no upstream tracker is configured", Title: "Title defect", Body: "upstream body"}}
		}
		text := renderLogAnalysis(view, nil)
		order := []string{"Filed Bugs", "Upstream reports", "Suggested team changes", "suggested change for your own team"}
		if dry {
			order = append(order, "would file into github:acme/app", "would-be body", "would file, but could not: no upstream tracker is configured", "dry run: nothing was filed")
		} else {
			order = append(order, "filed as acme/app#5", "not filed: no upstream tracker is configured", "upstream body")
		}
		for _, want := range order {
			if !strings.Contains(text, want) {
				t.Fatalf("dry=%v: show lacks %q:\n%s", dry, want, text)
			}
		}
		if strings.Index(text, "Filed Bugs") > strings.Index(text, "Upstream reports") || strings.Index(text, "Upstream reports") > strings.Index(text, "Suggested team changes") {
			t.Fatalf("groups out of order:\n%s", text)
		}
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
