package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
)

type retirementSweepIssueFunc func(context.Context, github.Repo, int) (github.Issue, error)

func (f retirementSweepIssueFunc) GetIssue(ctx context.Context, repo github.Repo, number int) (github.Issue, error) {
	return f(ctx, repo, number)
}

// US-003 AC-002/005: a final issue read must respect backoff armed by another
// worker, while the exact elapsed boundary allows observations again.
func TestRetirementBudgetIssues_BackoffBoundary(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	for _, tt := range []struct {
		name     string
		elapsed  time.Duration
		deferred bool
	}{
		{"inside hold/unknown issue", time.Minute - time.Nanosecond, true},
		{"exact boundary/observe issue", time.Minute, false},
		{"after boundary/observe issue", time.Minute + time.Nanosecond, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := &Daemon{}
			reader := retirementBudgetIssues{d: d, now: start.Add(tt.elapsed)}
			calls := 0
			reader.issues = retirementSweepIssueFunc(func(_ context.Context, _ github.Repo, n int) (github.Issue, error) {
				calls++
				return github.Issue{Number: n, State: "closed"}, nil
			})
			d.backoff.fail(start, time.Minute)
			got, err := reader.GetIssue(t.Context(), github.Repo{}, 42)
			if tt.deferred {
				if !errors.Is(err, ErrDiscoveryDeferred) || got.State != "" || calls != 0 {
					t.Fatalf("held read = %+v, %v, calls=%d", got, err, calls)
				}
			} else if err != nil || got.Number != 42 || got.State != "closed" || calls != 1 {
				t.Fatalf("eligible read = %+v, %v, calls=%d", got, err, calls)
			}
		})
	}
}

// US-003 AC-002: cancellation and verification failures remain distinguishable
// from a successful closed issue; the wrapper must preserve the caller context.
func TestRetirementBudgetIssues_PreservesFailures(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded, errors.New("forge unavailable")} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if errors.Is(failure, context.Canceled) {
				cancel()
			}
			reader := retirementBudgetIssues{d: &Daemon{}, issues: retirementSweepIssueFunc(func(gotCtx context.Context, _ github.Repo, _ int) (github.Issue, error) {
				if gotCtx != ctx {
					t.Error("caller context was replaced")
				}
				return github.Issue{}, failure
			})}
			got, err := reader.GetIssue(ctx, github.Repo{}, 42)
			if !errors.Is(err, failure) || got.State != "" {
				t.Fatalf("failed read = %+v, %v; want %v", got, err, failure)
			}
		})
	}
}
