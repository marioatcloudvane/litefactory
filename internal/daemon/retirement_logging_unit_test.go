package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
)

// US-002 AC-007: operator-visible decisions retain structured evidence without
// including raw provider/Git errors, which can contain credentials or contents.
func TestLogRetirement_SeverityAndEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, action, reason, phase     string
		preview                         bool
		issueError                      string
		failure                         bool
		observationLevel, resourceLevel string
	}{
		{name: "preview candidate", action: "would_remove", reason: "expired_and_preserved", phase: "initial", preview: true, observationLevel: "INFO", resourceLevel: "INFO"},
		{name: "confirmed removal", action: "removed", reason: "confirmed_absent", phase: "outcome", observationLevel: "INFO", resourceLevel: "INFO"},
		{name: "kept open", action: "kept", reason: "issue_open", phase: "final", observationLevel: "INFO", resourceLevel: "INFO"},
		{name: "already absent", action: "already_absent", reason: "already_absent", phase: "outcome", observationLevel: "INFO", resourceLevel: "INFO"},
		{name: "unknown preservation", action: "unknown", reason: "git_verification_failed", phase: "initial", failure: true, observationLevel: "WARN", resourceLevel: "WARN"},
		{name: "unavailable issue", action: "unknown", reason: "issue_lookup_failed", phase: "final", issueError: "issue_lookup_failed", observationLevel: "WARN", resourceLevel: "WARN"},
		{name: "mutation failure", action: "failed", reason: "delete_failed", phase: "outcome", observationLevel: "INFO", resourceLevel: "ERROR"},
		{name: "outcome write failure", action: "unknown", reason: "outcome_write_failed", phase: "outcome", observationLevel: "INFO", resourceLevel: "ERROR"},
		{name: "uncertain mutation", action: "unknown", reason: "mutation_failed_or_uncertain", phase: "outcome", observationLevel: "INFO", resourceLevel: "ERROR"},
		{name: "confirmation failure", action: "unknown", reason: "confirmation_failed", phase: "outcome", observationLevel: "INFO", resourceLevel: "ERROR"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, opts, gitObservation := retirementUnitFixture()
			e := daemon.RetirementEvaluation{Owner: owner, ObservedAt: opts.Now, IssueState: "closed", IssueReason: "completed", IssueError: tt.issueError, RetentionSource: "last_completed_run", RetentionAt: owner.LastFinishedAt, ExpiresAt: owner.LastFinishedAt.Add(14 * 24 * time.Hour), Git: gitObservation, Decisions: []daemon.RetirementDecision{{Resource: owner.Session.WorktreePath, Kind: "worktree", Action: tt.action, Reason: tt.reason, ExpectedSHA: "local"}}}
			if tt.failure {
				e.Error = errors.New("https://token-secret@example.test raw-private-file-content")
			}
			var buf bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&buf, nil))

			daemon.LogRetirement(context.Background(), logger, "sweep-42", tt.phase, tt.preview, e)

			if strings.Contains(buf.String(), "token-secret") || strings.Contains(buf.String(), "raw-private-file-content") {
				t.Fatalf("sensitive raw error logged: %s", buf.String())
			}
			decoder := json.NewDecoder(&buf)
			for i, wantLevel := range []string{tt.observationLevel, tt.resourceLevel} {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatalf("default-visible record %d absent: %v", i, err)
				}
				want := map[string]any{"level": wantLevel, "sweep": "sweep-42", "task": owner.Session.TaskRef, "repository": "owner/repo", "issue": float64(42), "observed_at": opts.Now.Format(time.RFC3339Nano), "source": "github_get_issue", "phase": tt.phase, "preview": tt.preview, "issue_state": "closed", "issue_reason": "completed", "issue_error": tt.issueError, "retention_source": "last_completed_run", "retention_at": owner.LastFinishedAt.Format(time.RFC3339Nano), "expires_at": e.ExpiresAt.Format(time.RFC3339Nano), "base_sha": "base", "local_sha": "local", "remote_sha": "remote"}
				if i == 0 {
					want["msg"] = "retirement observation"
				} else {
					want["msg"] = "retirement resource"
					want["resource"] = owner.Session.WorktreePath
					want["kind"] = "worktree"
					want["expected_sha"] = "local"
					want["action"] = tt.action
					want["reason"] = tt.reason
					want["outcome"] = tt.action
				}
				for key, value := range want {
					if record[key] != value {
						t.Errorf("record %d %s = %#v, want %#v", i, key, record[key], value)
					}
				}
			}
		})
	}
}

func TestLogRetirement_ObservationWithoutResources(t *testing.T) {
	t.Parallel()
	owner, opts, _ := retirementUnitFixture()
	var buf bytes.Buffer
	e := daemon.RetirementEvaluation{Owner: owner, ObservedAt: opts.Now, IssueState: "open"}
	daemon.LogRetirement(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), "empty", "final", false, e)
	var record map[string]any
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("expected one observation: %v", err)
	}
	if record["msg"] != "retirement observation" || record["issue_state"] != "open" || record["level"] != "INFO" {
		t.Fatalf("observation = %v", record)
	}
}
