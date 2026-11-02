package turn

import (
	"context"
	"slices"
	"testing"

	"github.com/cloudvane/litefactory/internal/bugchecks"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/coverage"
	"github.com/cloudvane/litefactory/internal/store"
)

// probeMakeTestTarget is the supported make test control flow, so discovery
// reaches the variable facts probe instead of stopping at the recipe check.
const probeMakeTestTarget = "test:\n\t@set -e; \\\n\tselected=\"$$($(GO) list $(PKGS))\"; \\\n\tmixed=\"$$($(GO) list $(MIXED_PACKAGES) | tr '\\n' ' ')\"; \\\n\tfull_race=\"\"; mixed_selected=\"\"; \\\n\tfor pkg in $$selected; do \\\n\t  case \" $$mixed \" in \\\n\t    *\" $$pkg \"*) mixed_selected=\"$$mixed_selected $$pkg\" ;; \\\n\t    *) full_race=\"$$full_race $$pkg\" ;; \\\n\t  esac; \\\n\tdone; \\\n\tif [ -n \"$$full_race\" ]; then $(GO) test -race -timeout=$(TEST_TIMEOUT) $$full_race; fi; \\\n\tif [ -n \"$$mixed_selected\" ]; then $(GO) test -timeout=$(TEST_TIMEOUT) $$mixed_selected; fi; \\\n\tfor pkg in $$mixed_selected; do \\\n\t  case $$pkg in \\\n\t    */turn) tests='$(subst $(space),|,$(strip $(RACE_TESTS_turn)))' ;; \\\n\t    */store) tests='$(subst $(space),|,$(strip $(RACE_TESTS_store)))' ;; \\\n\t    */cli) tests='$(subst $(space),|,$(strip $(RACE_TESTS_cli)))' ;; \\\n\t  esac; \\\n\t  $(GO) test -race -timeout=$(TEST_TIMEOUT) -run \"^($$tests)\"'$$' $$pkg; \\\n\tdone"

// A make that cannot evaluate the facts probe (as GNU Make 3.81 could not
// with --eval) must leave Validate on the unchanged full suite, with the
// failed probe kept as evidence, not turn the suite into a failure.
func TestValidateMakeFactsProbeFailureFallsBackToFullSuite(t *testing.T) {
	f := newSuiteFixture(t)
	recipe := []byte("version: 1\nphases:\n  regression:\n    commands:\n      - argv: [fixture-regression]\n        timeout_seconds: 30\n  focused:\n    commands:\n      - argv: [fixture-focused]\n        timeout_seconds: 30\n  full:\n    commands:\n      - argv: [make, test]\n        cwd: .\n        timeout_seconds: 30\n")
	f.b.Repo = coverageMatrixRepo{recipe}
	request, err := f.db.EnsureValidationRequest(t.Context(), store.ValidationRequest{Lineage: "make-probe", SourceKey: "current", Policy: "auto", Commit: suiteCommit})
	if err != nil {
		t.Fatal(err)
	}
	f.b.Bind = func(context.Context, Options, ActivityRequest) (ValidateSuiteAuthority, error) {
		return ValidateSuiteAuthority{Commit: suiteCommit, RequestID: request.ID, Lineage: "make-probe", Policy: "auto", AssessmentVersion: coverage.Version}, nil
	}
	var calls [][]string
	f.b.Run = func(_ context.Context, _ string, c config.BugCheckCommand, started func(bugchecks.CommandResult) error) bugchecks.CommandResult {
		calls = append(calls, c.Argv)
		code, outcome, output := 0, "passed", "ok  \texample.test/p\t0.1s\n"
		switch c.Argv[0] {
		case "/bin/cat":
			output = probeMakeTestTarget + "\n"
		case "/bin/sh":
			code, outcome, output = 2, "failed", "make: unrecognized option\n"
		}
		r := bugchecks.CommandResult{Started: suiteClock, Ended: suiteClock, Exit: &code, Outcome: outcome, CleanupConfirmed: true, CaptureComplete: true, ProcessIdentity: "make-probe", Output: output}
		if err := started(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	result, out, err := f.exec(950)
	if err != nil || result.Outcome != "passed" {
		t.Fatalf("probe failure changed the suite outcome: result=%+v output=%+v err=%v calls=%q", result, out, err, calls)
	}
	if !slices.ContainsFunc(calls, func(a []string) bool { return a[0] == "/bin/sh" }) {
		t.Fatalf("facts probe never ran: %q", calls)
	}
	if !slices.ContainsFunc(calls, func(a []string) bool { return slices.Equal(a, []string{"make", "test"}) }) {
		t.Fatalf("unchanged full suite never ran: %q", calls)
	}
	m, err := validateRetainedAssessment(t.Context(), Options{DB: f.db}, out)
	if err != nil {
		t.Fatal(err)
	}
	if m.FullReason == "" {
		t.Fatal("full fallback has no retained reason")
	}
	commands, err := f.db.SuiteCommands(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(commands, func(c store.SuiteCommand) bool { return c.Argv[0] == "/bin/sh" && c.State == store.SuiteFailed }) {
		t.Fatalf("failed probe was not kept as evidence: %+v", commands)
	}
}
