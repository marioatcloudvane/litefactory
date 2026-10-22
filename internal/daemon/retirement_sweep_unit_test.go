package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
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

type retirementLinearIngress struct {
	origin ticket.Origin
	state  string
}

func (retirementLinearIngress) Provider() ticket.Provider { return ticket.Linear }
func (i retirementLinearIngress) Resolve(_ context.Context, l ticket.Locator) (ticket.Snapshot, error) {
	return ticket.Snapshot{Ref: ticket.Ref{Locator: l, Identity: ticket.Identity{Provider: ticket.Linear, Origin: i.origin, RemoteID: l.Key}}, Title: "linear task", State: i.state, StateReason: "completed"}, nil
}
func (retirementLinearIngress) QualifyReply(ticket.Ref, github.Comment) (ticket.Reply, error) {
	return ticket.Reply{}, errors.New("unused")
}

// Issue #975: without an injected reader, retirement reads a Linear task's
// state through the ticket connection (d.readIssue), never the raw forge.
func TestRetirementTicketIssues_LinearConnection(t *testing.T) {
	t.Parallel()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	origin := ticket.Origin("https://api.linear.app")
	slot, err := db.LocalTicketSlot(t.Context(), "issue", ticket.Identity{Provider: ticket.Linear, Origin: origin, RemoteID: "linear-issue-1"})
	if err != nil {
		t.Fatal(err)
	}
	filter, err := turn.NewFilter(db, turn.Identities{OwnerUserID: 42, BotUserID: 99, TicketOrigin: origin}, nil)
	if err != nil {
		t.Fatal(err)
	}
	repo := github.Repo{Owner: "owner", Name: "repo"}
	for _, tt := range []struct {
		state  string
		closed bool
	}{{"closed", true}, {"open", false}} {
		d := &Daemon{db: db, tickets: retirementLinearIngress{origin: origin, state: tt.state}, filter: filter}
		issue, err := retirementTicketIssues{d: d}.GetIssue(t.Context(), repo, int(slot))
		if err != nil {
			t.Fatalf("%s: %v", tt.state, err)
		}
		if issue.State != tt.state || issue.QualifiedSnapshot == nil || issue.QualifiedSnapshot.Ref.Identity.Provider != ticket.Linear {
			t.Fatalf("%s: issue not read through Linear connection: %+v", tt.state, issue)
		}
		if got := RetirementIssueClosed(store.Repo{Owner: "owner", Name: "repo"}, int(slot), issue); got != tt.closed {
			t.Fatalf("%s: closed = %v, want %v", tt.state, got, tt.closed)
		}
	}
}

// Issue #1029 C1: with project connections, the shared retirement reader
// addresses the session's Linear ticket container, not the code repository.
// Before the fix every read failed with ErrTicketIdentityConflict.
func TestRetirementTicketIssues_ProjectConnectionLinearContainer(t *testing.T) {
	x := newRetirementLinearFixture(t)
	code := github.Repo{Owner: x.repo.Owner, Name: x.repo.Name}
	for _, tt := range []struct {
		kind, state string
		closed      bool
	}{{"completed", "closed", true}, {"canceled", "closed", true}, {"unstarted", "open", false}} {
		x.setState(tt.kind)
		issue, err := retirementTicketIssues{d: x.d}.GetIssue(x.ctx(t), code, x.session.IssueNumber)
		if err != nil {
			t.Fatalf("%s: %v", tt.kind, err)
		}
		if issue.State != tt.state || issue.QualifiedSnapshot == nil || issue.QualifiedSnapshot.Ref.Identity.Provider != ticket.Linear {
			t.Fatalf("%s: issue not read through Linear connection: %+v", tt.kind, issue)
		}
		if got := RetirementIssueClosed(x.repo, x.session.IssueNumber, issue); got != tt.closed {
			t.Fatalf("%s: closed = %v, want %v", tt.kind, got, tt.closed)
		}
	}
}
