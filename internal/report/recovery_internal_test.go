package report

import (
	"strings"
	"testing"
	"time"
)

func TestRecoveryUsesCurrentActionDisposition(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		status, want string
		scheduled    bool
	}{
		{"pending", "Please wait", true}, {"reserved", "Please wait", true}, {"running", "completion is not yet confirmed", false}, {"succeeded", "succeeded", false}, {"failed", "factory operator", false}, {"cancelled", "changed context", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			text := recoverySection(&Recovery{ActionID: 42, Status: tc.status, DueAt: time.Date(2026, 10, 25, 5, 0, 0, 0, time.UTC), ManualReason: "changed context"})
			if !strings.Contains(text, tc.want) || strings.Contains(text, "/schedule") {
				t.Fatalf("incorrect guidance: %s", text)
			}
			if strings.Contains(text, "eligible at") != tc.scheduled {
				t.Fatalf("incorrect scheduling claim: %s", text)
			}
		})
	}
}

func TestRecoveryAbsentContinuationReasons(t *testing.T) {
	for _, cause := range []string{"Automatic session-limit recovery is disabled", "No reliable future reset time was supplied", "No usable conversation identity was recorded", "The three automatic invocation limit is exhausted", ""} {
		text := recoverySection(&Recovery{NoContinuation: true, ManualReason: cause + "; resume manually", InspectionCommand: "litefactory logs 'github:owner/repo#1' --run 7"})
		if !strings.Contains(text, "Nothing is scheduled") || !strings.Contains(text, "--run 7") || strings.Contains(text, "resume manually") {
			t.Fatal(text)
		}
		if cause != "" && !strings.Contains(text, cause) {
			t.Fatal(text)
		}
	}
	unknown := recoverySection(&Recovery{})
	if strings.Contains(unknown, "Nothing is scheduled") || !strings.Contains(unknown, "unknown") {
		t.Fatal(unknown)
	}
	pending := recoverySection(&Recovery{ActionID: 17, Status: "pending", InspectionCommand: "logs"})
	if !strings.Contains(pending, "17 is pending") || !strings.Contains(pending, "eligibility time is unknown") {
		t.Fatal(pending)
	}
}

func TestRecoveryCausePreservesRetainedDiagnosticClauses(t *testing.T) {
	t.Parallel()
	reason := "automatic recovery could not start; resume manually: launch refused; saved context differs"
	text := recoverySection(&Recovery{ActionID: 42, Status: "cancelled", ManualReason: reason, InspectionCommand: "litefactory logs 'github:owner/repo#1' --run 7"})
	if !strings.Contains(text, "automatic recovery could not start: launch refused; saved context differs") || strings.Contains(text, "resume manually") || strings.Count(text, "litefactory logs") != 1 {
		t.Fatalf("retained diagnostic lost or advice duplicated: %s", text)
	}
	if got := recoveryCause("known cause; additional retained fact"); got != "known cause; additional retained fact" {
		t.Fatalf("arbitrary retained clause discarded: %s", got)
	}
}
