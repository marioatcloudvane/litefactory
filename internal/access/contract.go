// Package access is the one gate a grantee's input passes before it runs.
// It applies the owner-only floor, the size and hourly caps, reads target facts
// from the installed team, asks the classifier and records the decision. It
// knows no workflow, node or stage names: every name it handles is data.
package access

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// Starting values; the technical stage may tune them.
const (
	// MaxRequestBytes caps title, body and comment or batch text together.
	MaxRequestBytes = 64 << 10
	// HourlyChecks is the classifier calls one grantee may cause per project
	// in the last hour.
	HourlyChecks = 30
)

// Refusal is why a request was not run.
type Refusal string

// Refusal kinds; RefusedNone means the request is allowed.
const (
	RefusedNone      Refusal = ""
	RefusedDenied    Refusal = "denied"     // the classifier said deny
	RefusedOwnerOnly Refusal = "owner_only" // the owner-only floor
	RefusedUnchecked Refusal = "unchecked"  // the check could not be made
	RefusedLimited   Refusal = "limited"    // size cap or hourly cap
)

// Target is what the input would act on. Workflow and Node come from the
// command parser, the waiting instance or the mode picker; they are data.
type Target struct {
	// Entry is "start" for input that starts a run, "answer" for a reply at a
	// waiting node.
	Entry    string
	Workflow agent.Workflow
	// Node is the waiting node an answer goes to; empty for a start.
	Node string
	// ApproveReply is the waiting node's lf_approve_reply attribute, empty
	// when it has none. A reply matching it is owner-only.
	ApproveReply string
}

// Request is one grantee input.
type Request struct {
	Repo      store.Repo
	SessionID int64
	Actor     ticket.Identity
	Handle    string
	// Source is the comment, the first comment of a batch, or the issue for an
	// issue body. It keys the decision: one row per source.
	Source ticket.Identity
	// CoveredThrough is the last batch comment's remote id, empty otherwise.
	CoveredThrough string
	IssueTitle     string
	IssueBody      string
	// Text is the comment or batch text; empty for an issue body.
	Text string
	// Command is the parsed command name with its slash, empty for free text.
	Command string
	// CodeControl is true when the body is a code-control command.
	CodeControl bool
	Target      Target
}

// Outcome is the gate's single answer.
type Outcome struct {
	DecisionID int64
	Refusal    Refusal // RefusedNone means allowed
	Reason     string  // one plain line
	// Replayed is true when the decision was read back, not made now.
	Replayed bool
}

// Allowed reports whether the request may run.
func (o Outcome) Allowed() bool { return o.DecisionID > 0 && o.Refusal == RefusedNone }

// Store is the slice of the store the gate needs.
type Store interface {
	OpenAccessGrant(ctx context.Context, repoID int64, actor ticket.Identity) (store.AccessGrant, error)
	BeginAccessDecision(ctx context.Context, d store.AccessDecision) (store.AccessDecision, bool, error)
	FinishAccessDecision(ctx context.Context, id int64, out store.AccessDecisionOutcome) (store.AccessDecision, error)
	CountAccessChecks(ctx context.Context, repoID int64, actor ticket.Identity, since time.Time, excludeID int64) (int, error)
	AccessDecisionBySource(ctx context.Context, repoID int64, source ticket.Identity) (store.AccessDecision, error)
}

// Facts reads target facts from the repository's installed team. Any error is
// a "could not be checked" refusal.
type Facts func(ctx context.Context, repo store.Repo, target Target) (TargetFacts, error)

// Role returns the installed team's access-classifier role text, or "" when
// the team has none, so the runner falls back to the factory default.
type Role func(ctx context.Context, repo store.Repo) (string, error)

// Gate is the access check. Build it with every field set.
type Gate struct {
	Store   Store
	Facts   Facts
	Role    Role
	Checker agent.AccessChecker
	Now     func() time.Time
}

var _ Store = (*store.DB)(nil)
