package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ErrNoGrant means the actor has no open grant on the project. Callers stay
// silent for such input.
var ErrNoGrant = errors.New("access: actor has no open grant on this project")

// Plain reasons the gate records itself.
const (
	reasonOwnerOnly = "Only the owner can do this."
	reasonUnchecked = "The request could not be checked."
	reasonTooLarge  = "The request is too large to check."
	reasonHourly    = "Too many requests in the last hour; try again later."
	reasonAllowed   = "Allowed by the grant."
	reasonDenied    = "Not covered by the grant."
)

// Digest is the hex SHA-256 of the classified text's UTF-8 bytes.
func Digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// classifiedText is the text a request is judged on.
func classifiedText(r Request) string {
	if r.Text != "" {
		return r.Text
	}
	return r.IssueBody
}

// RefusalOf maps a stored decision to the gate's refusal.
func RefusalOf(decision string) Refusal {
	switch decision {
	case store.AccessAllow:
		return RefusedNone
	case store.AccessDeny:
		return RefusedDenied
	case store.AccessFloor:
		return RefusedOwnerOnly
	case store.AccessLimited, store.AccessRateLimited:
		return RefusedLimited
	default:
		return RefusedUnchecked
	}
}

func outcomeOf(d store.AccessDecision, replayed bool) Outcome {
	return Outcome{DecisionID: d.ID, Refusal: RefusalOf(d.Decision), Reason: d.Reason, Replayed: replayed}
}

func sameIdentity(a, b ticket.Identity) bool {
	return a.Provider == b.Provider && a.Origin == b.Origin && a.RemoteID == b.RemoteID
}

func (g *Gate) now() time.Time {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now()
}

func validate(g *Gate, r Request) error {
	if g == nil || g.Store == nil {
		return errors.New("access: gate requires a store")
	}
	if r.Repo.ID <= 0 {
		return errors.New("access: request requires a project")
	}
	if r.SessionID <= 0 {
		return errors.New("access: request requires a session")
	}
	if err := r.Actor.Validate(); err != nil {
		return err
	}
	return r.Source.Validate()
}

// Check decides one request. It returns an error only for storage failures
// the caller must retry, ErrNoGrant for an actor without an open grant, or a
// malformed request; every classifier or graph problem is a refusal.
func (g *Gate) Check(ctx context.Context, r Request) (Outcome, error) {
	if err := validate(g, r); err != nil {
		return Outcome{}, err
	}
	var pending *store.AccessDecision
	existing, err := g.Store.AccessDecisionBySource(ctx, r.Repo.ID, r.Source)
	switch {
	case errors.Is(err, store.ErrNotFound):
	case err != nil:
		return Outcome{}, err
	default:
		if !sameIdentity(existing.Actor, r.Actor) {
			return Outcome{Refusal: RefusedUnchecked, Reason: reasonUnchecked}, nil
		}
		if existing.State == store.AccessDecisionFinal {
			return g.replay(existing, r), nil
		}
		pending = &existing
	}

	grant, err := g.Store.OpenAccessGrant(ctx, r.Repo.ID, r.Actor)
	if errors.Is(err, store.ErrNotFound) {
		return Outcome{}, ErrNoGrant
	}
	if err != nil {
		return Outcome{}, err
	}
	if !grant.Open() || grant.Actor.Provider != r.Actor.Provider || grant.Actor.Origin != r.Actor.Origin {
		return Outcome{}, ErrNoGrant
	}

	text := classifiedText(r)
	digest := Digest(text)
	row := store.AccessDecision{
		RepoID: r.Repo.ID, SessionID: r.SessionID, Source: r.Source, CoveredThrough: r.CoveredThrough,
		Actor: r.Actor, Handle: r.Handle, GrantID: grant.ID, GrantRevision: grant.Revision, RequestDigest: digest,
	}
	basic := TargetFacts{Workflow: string(r.Target.Workflow), Entry: r.Target.Entry, Node: r.Target.Node, Nodes: []NodeFact{}, Command: r.Command}

	if pending != nil && (pending.RequestDigest != digest || pending.GrantID != grant.ID || pending.GrantRevision != grant.Revision) {
		return g.finish(ctx, r, *pending, store.AccessDecisionOutcome{Decision: store.AccessError, Reason: reasonUnchecked})
	}

	if r.CodeControl || (r.Target.ApproveReply != "" && store.IsApprovalReply(text, r.Target.ApproveReply)) {
		return g.decide(ctx, r, pending, row, basic, store.AccessDecisionOutcome{Decision: store.AccessFloor, Reason: reasonOwnerOnly})
	}
	if len(r.IssueTitle)+len(r.IssueBody)+len(r.Text) > MaxRequestBytes {
		return g.decide(ctx, r, pending, row, basic, store.AccessDecisionOutcome{Decision: store.AccessLimited, Reason: reasonTooLarge})
	}
	var exclude int64
	if pending != nil {
		exclude = pending.ID
	}
	count, err := g.Store.CountAccessChecks(ctx, r.Repo.ID, r.Actor, g.now().Add(-time.Hour), exclude)
	if err != nil {
		return Outcome{}, err
	}
	if count >= HourlyChecks {
		return g.decide(ctx, r, pending, row, basic, store.AccessDecisionOutcome{Decision: store.AccessRateLimited, Reason: reasonHourly})
	}
	unchecked := store.AccessDecisionOutcome{Decision: store.AccessError, Reason: reasonUnchecked}
	if g.Facts == nil || g.Role == nil || g.Checker == nil {
		return g.decide(ctx, r, pending, row, basic, unchecked)
	}
	facts, err := g.Facts(ctx, r.Repo, r.Target)
	if err != nil {
		return g.decide(ctx, r, pending, row, basic, unchecked)
	}
	facts.Command = r.Command
	if facts.Nodes == nil {
		facts.Nodes = []NodeFact{}
	}
	role, err := g.Role(ctx, r.Repo)
	if err != nil {
		return g.decide(ctx, r, pending, row, basic, unchecked)
	}
	targetJSON, err := json.Marshal(facts)
	if err != nil {
		return g.decide(ctx, r, pending, row, basic, unchecked)
	}

	var current store.AccessDecision
	if pending != nil {
		if pending.TargetJSON != string(targetJSON) {
			return g.finish(ctx, r, *pending, unchecked)
		}
		current = *pending
	} else {
		row.TargetJSON = string(targetJSON)
		begun, created, err := g.Store.BeginAccessDecision(ctx, row)
		if err != nil {
			return Outcome{}, err
		}
		if !created {
			if !sameIdentity(begun.Actor, r.Actor) {
				return Outcome{Refusal: RefusedUnchecked, Reason: reasonUnchecked}, nil
			}
			if begun.State == store.AccessDecisionFinal {
				return g.replay(begun, r), nil
			}
			if begun.RequestDigest != digest || begun.GrantID != grant.ID || begun.GrantRevision != grant.Revision || begun.TargetJSON != row.TargetJSON {
				return g.finish(ctx, r, begun, unchecked)
			}
		}
		current = begun
	}

	checkCtx, cancel := context.WithTimeout(ctx, agent.AccessCheckTimeout)
	result, err := g.Checker.CheckAccess(checkCtx, agent.AccessCheckRequest{
		Role: role, GrantText: grant.Text, Target: json.RawMessage(targetJSON),
		IssueTitle: r.IssueTitle, IssueBody: r.IssueBody, Text: r.Text, Command: r.Command,
	})
	cancel()
	if ctx.Err() != nil {
		// The caller went away; the row stays pending and a retry re-checks it.
		return Outcome{}, ctx.Err()
	}
	out := store.AccessDecisionOutcome{Decision: store.AccessError, Reason: reasonUnchecked, Model: oneLine(result.Model, 200), Classified: true}
	if err == nil {
		switch result.Decision {
		case agent.AccessAllow:
			out.Decision, out.Reason = store.AccessAllow, plainReason(result.Reason, reasonAllowed)
		case agent.AccessDeny:
			out.Decision, out.Reason = store.AccessDeny, plainReason(result.Reason, reasonDenied)
		}
	}
	return g.finish(ctx, r, current, out)
}

// decide begins the row, unless it is already pending, and finishes it at once.
func (g *Gate) decide(ctx context.Context, r Request, pending *store.AccessDecision, row store.AccessDecision, facts TargetFacts, out store.AccessDecisionOutcome) (Outcome, error) {
	if pending != nil {
		return g.finish(ctx, r, *pending, out)
	}
	raw, err := json.Marshal(facts)
	if err != nil {
		return Outcome{}, err
	}
	row.TargetJSON = string(raw)
	begun, created, err := g.Store.BeginAccessDecision(ctx, row)
	if err != nil {
		return Outcome{}, err
	}
	if !created {
		if !sameIdentity(begun.Actor, r.Actor) {
			return Outcome{Refusal: RefusedUnchecked, Reason: reasonUnchecked}, nil
		}
		if begun.State == store.AccessDecisionFinal {
			return g.replay(begun, r), nil
		}
	}
	return g.finish(ctx, r, begun, out)
}

// finish records the outcome; when another check finished the row first, that
// decision is replayed.
func (g *Gate) finish(ctx context.Context, r Request, row store.AccessDecision, out store.AccessDecisionOutcome) (Outcome, error) {
	done, err := g.Store.FinishAccessDecision(ctx, row.ID, out)
	if errors.Is(err, store.ErrAccessDecisionFinal) {
		final, readErr := g.Store.AccessDecisionBySource(ctx, r.Repo.ID, r.Source)
		if readErr != nil {
			return Outcome{}, readErr
		}
		return g.replay(final, r), nil
	}
	if err != nil {
		return Outcome{}, err
	}
	return outcomeOf(done, false), nil
}

// replay returns a final decision. A decision made on other text is not
// reused: the request now differs from what was checked.
func (g *Gate) replay(d store.AccessDecision, r Request) Outcome {
	if !sameIdentity(d.Actor, r.Actor) || d.State != store.AccessDecisionFinal {
		return Outcome{Refusal: RefusedUnchecked, Reason: reasonUnchecked, Replayed: true}
	}
	if d.RequestDigest != Digest(classifiedText(r)) {
		return Outcome{DecisionID: d.ID, Refusal: RefusedUnchecked, Reason: reasonUnchecked, Replayed: true}
	}
	return outcomeOf(d, true)
}

// plainReason makes the classifier's reason one line the store accepts.
func plainReason(reason, fallback string) string {
	line := oneLine(reason, store.MaxAccessDecisionReason)
	if line == "" {
		return fallback
	}
	return line
}

func oneLine(s string, limit int) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > limit {
		s = strings.TrimSpace(string([]rune(s)[:limit]))
	}
	return s
}
