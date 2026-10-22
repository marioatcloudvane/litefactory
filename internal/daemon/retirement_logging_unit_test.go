package daemon_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
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

// Observation failures name the failing Git step from structured fields only.
func TestLogRetirement_ObservationErrorStep(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want map[string]any
	}{
		{name: "git", err: fmt.Errorf("observe: %w", &git.CommandError{Args: []string{"ls-remote", "--refs", "--", "origin"}, ExitCode: 128, Stderr: "fatal: https://token-secret@example.test raw-private-file-content"}), want: map[string]any{"git_step": "ls-remote", "git_exit_code": float64(128), "git_timed_out": false}},
		{name: "worktree", err: &git.CommandError{Args: []string{"worktree", "list", "--porcelain"}, ExitCode: -1, TimedOut: true}, want: map[string]any{"git_step": "worktree list", "git_exit_code": float64(-1), "git_timed_out": true}},
		{name: "non_git", err: errors.New("https://token-secret@example.test raw-private-file-content"), want: map[string]any{"git_step": "non_git"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, opts, gitObservation := retirementUnitFixture()
			e := daemon.RetirementEvaluation{Owner: owner, ObservedAt: opts.Now, IssueState: "closed", Git: gitObservation, Error: tt.err}
			var buf bytes.Buffer
			daemon.LogRetirement(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), "sweep-42", "revalidate", false, e)
			if strings.Contains(buf.String(), "token-secret") || strings.Contains(buf.String(), "raw-private-file-content") {
				t.Fatalf("sensitive raw error logged: %s", buf.String())
			}
			var record map[string]any
			if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["level"] != "WARN" || record["msg"] != "retirement observation" {
				t.Fatalf("record = %v", record)
			}
			for key, value := range tt.want {
				if record[key] != value {
					t.Errorf("%s = %#v, want %#v", key, record[key], value)
				}
			}
			if _, ok := record["git_exit_code"]; tt.name == "non_git" && ok {
				t.Errorf("non-git error has exit code: %v", record)
			}
		})
	}
}

// Issue #1029 C3: both line types name the source actually used and carry the
// bounded error class of a lookup failure, never its raw text.
func TestLogRetirement_SourceAndErrorClass(t *testing.T) {
	t.Parallel()
	const secret = "distinctive-raw-lookup-detail"
	cases := []struct {
		name, taskRef, source, class string
		err                          error
	}{
		{name: "linear identity conflict", taskRef: "linear:team-uuid/project-uuid#42", source: "linear_ticket", class: "ticket_identity_conflict", err: fmt.Errorf("%s: %w", secret, store.ErrTicketIdentityConflict)},
		{name: "linear not found", taskRef: "linear:team-uuid/project-uuid#42", source: "linear_ticket", class: "not_found", err: fmt.Errorf("%s: %w", secret, store.ErrNotFound)},
		{name: "deferred", taskRef: "linear:team-uuid/project-uuid#42", source: "linear_ticket", class: "deferred", err: fmt.Errorf("%s: %w", secret, daemon.ErrDiscoveryDeferred)},
		{name: "ticket rate limit", taskRef: "linear:team-uuid/project-uuid#42", source: "linear_ticket", class: "rate_limited", err: fmt.Errorf("%s: %w", secret, &ticket.RateLimitError{})},
		{name: "github rate limit", taskRef: "github:owner/repo#42", source: "github_get_issue", class: "rate_limited", err: fmt.Errorf("%s: %w", secret, &github.RateLimitError{})},
		{name: "github other", taskRef: "github:owner/repo#42", source: "github_get_issue", class: "other", err: errors.New(secret)},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			owner, opts, _ := retirementUnitFixture()
			owner.Session.TaskRef = tt.taskRef
			e := daemon.RetirementEvaluation{Owner: owner, ObservedAt: opts.Now, IssueError: "issue_lookup_failed", Error: tt.err, Decisions: []daemon.RetirementDecision{
				{Resource: owner.Session.WorktreePath, Kind: "worktree", Action: "unknown", Reason: "issue_lookup_failed"},
				{Resource: "refs/heads/" + owner.Session.Branch, Kind: "local_branch", Action: "unknown", Reason: "issue_lookup_failed"},
			}}
			var buf bytes.Buffer
			daemon.LogRetirement(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), "sweep-1029", "initial", false, e)
			if strings.Contains(buf.String(), secret) {
				t.Fatalf("raw error text logged: %s", buf.String())
			}
			decoder := json.NewDecoder(&buf)
			for i, msg := range []string{"retirement observation", "retirement resource", "retirement resource"} {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatalf("record %d absent: %v", i, err)
				}
				for key, value := range map[string]any{"msg": msg, "source": tt.source, "error_class": tt.class, "issue_error": "issue_lookup_failed", "git_step": "non_git"} {
					if record[key] != value {
						t.Errorf("record %d %s = %#v, want %#v", i, key, record[key], value)
					}
				}
			}
		})
	}
}
