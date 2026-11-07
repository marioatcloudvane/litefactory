package daemon

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/cloudvane/litefactory/internal/brainstormtasks"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

const (
	bugLinearRepresentation = "linear-markdown/v1"

	bugReasonLinearMoved     = "the Linear connection no longer matches this bug filing"
	bugReasonLinearNothing   = "the Linear connection changed before the create; nothing was filed"
	bugReasonLinearCheck     = "checking whether the issue was created; not found yet"
	bugReasonLinearUnreached = "the check could not reach the Linear project; nothing was filed again"
)

// errBugLinearMoved reports a ticket connection that is missing, disabled or
// no longer the destination and bug label the filing froze.
var errBugLinearMoved = errors.New("daemon: the Linear connection no longer matches the bug filing")

// bugLinearPort is the narrow Linear port of the bug filing processor. The
// real *linear.Client satisfies it. It can create one labelled issue under a
// caller-supplied UUID, read it back, and add the mapped bug label by ID; it
// can never create a label.
type bugLinearPort interface {
	CreateLabelledWithID(ctx context.Context, container, id, labelID string, title, body redact.Body) (ticket.Snapshot, error)
	Resolve(ctx context.Context, l ticket.Locator) (ticket.Snapshot, error)
	AddLabel(ctx context.Context, ref ticket.Ref, label string) error
}

var _ bugLinearPort = (*linear.Client)(nil)

// bugLinearTarget is the Linear port with the verified publisher and origin of
// one connection version.
type bugLinearTarget struct {
	port      bugLinearPort
	publisher ticket.Identity
	origin    ticket.Origin
}

// bugFilingLinear builds the Linear target for a connection version.
func (d *Daemon) bugFilingLinear(ctx context.Context, v store.ConnectionVersion) (bugLinearTarget, error) {
	if d.connections == nil {
		return bugLinearTarget{}, errors.New("bug filing: project connections are unavailable")
	}
	clients, err := d.connections.Clients(ctx, v, 0)
	if err != nil {
		return bugLinearTarget{}, err
	}
	if clients == nil || clients.Linear == nil {
		return bugLinearTarget{}, errors.New("bug filing: the Linear client is unavailable")
	}
	return bugLinearTarget{port: clients.Linear, publisher: clients.Publisher.Identity, origin: v.Origin}, nil
}

// linearTarget returns the client for the filing's current connection, or
// errBugLinearMoved when the connection no longer matches what was frozen.
func (p *bugFilingProcessor) linearTarget(ctx context.Context, f store.BugFiling) (bugLinearTarget, error) {
	v, err := p.db.CurrentConnection(ctx, f.RepositoryID, store.TicketConnection)
	if errors.Is(err, store.ErrNotFound) {
		return bugLinearTarget{}, errBugLinearMoved
	}
	if err != nil {
		return bugLinearTarget{}, err
	}
	if !v.Enabled || v.Provider != ticket.Linear || v.Destination != f.Destination || v.Linear.BugLabelID != f.LabelID {
		return bugLinearTarget{}, errBugLinearMoved
	}
	if p.linear == nil {
		return bugLinearTarget{}, errors.New("bug filing: Linear is unavailable")
	}
	return p.linear(ctx, v)
}

func (t bugLinearTarget) locator(f store.BugFiling, key string) ticket.Locator {
	return ticket.Locator{Provider: ticket.Linear, Origin: t.origin, Container: f.Destination, Key: key}
}

// linearDifference names the first field in which a read-back issue differs
// from the prepared creation, or returns "" when it correlates.
func linearDifference(s ticket.Snapshot, uuid string, f store.BugFiling, representation string, publisher ticket.Identity) string {
	switch {
	case s.Ref.Identity.RemoteID != uuid:
		return "uuid"
	case s.Title != f.Title:
		return "title"
	case !linear.ContentEquivalent(f.Body, s.Body):
		return "body"
	case s.Representation != representation:
		return "representation"
	case !s.Author.Identity.Equal(publisher):
		return "author"
	}
	return ""
}

// adoptLinear records the correlated issue on next and retains its
// representation. The numeric alias is the factory's local slot, the same one
// project ticket admission derives later.
func (p *bugFilingProcessor) adoptLinear(ctx context.Context, next *store.BugFiling, s ticket.Snapshot, prep store.TicketEffectPreparation) error {
	slot, err := p.db.LocalTicketSlot(ctx, "issue", s.Ref.Identity)
	if err != nil {
		return err
	}
	if err = p.db.ConfirmTicketEffectRepresentation(ctx, prep.Key, s.Title+"\n"+s.ProviderBody); err != nil {
		return err
	}
	next.State = store.BugFilingCreated
	next.IssueNodeID, next.IssueID, next.IssueNumber, next.IssueLink = prep.CreationUUID, slot, int(slot), s.BrowserURL
	next.Reason = ""
	next.NextCheckAt = time.Time{}
	return nil
}

func (p *bugFilingProcessor) notCreated(ctx context.Context, f store.BugFiling, reason string) (bool, error) {
	next := f
	next.State = store.BugFilingNotCreated
	next.Reason = reason
	_, err := p.save(ctx, next, store.BugFilingAdmitted)
	return true, err
}

// createLinear prepares the creation UUID, saves the durable begin, checks
// authority and sends at most one labelled create under that UUID.
func (p *bugFilingProcessor) createLinear(ctx context.Context, f store.BugFiling) (bool, error) {
	at := p.clock()
	t, err := p.linearTarget(ctx, f)
	if errors.Is(err, errBugLinearMoved) {
		return p.notCreated(ctx, f, bugReasonLinearNothing)
	}
	if err != nil {
		return false, err
	}
	if p.redactor == nil {
		return false, errors.New("bug filing: protection unavailable")
	}
	title, terr := p.redactor.Apply(ctx, f.Title, 64<<10)
	body, berr := p.redactor.Apply(ctx, f.Body, 64<<10)
	if terr != nil || berr != nil || title.String() != f.Title || body.String() != f.Body {
		return p.notCreated(ctx, f, "refused before any write (protection changed the title or body); nothing was filed")
	}
	locator := t.locator(f, "creation:"+f.EffectKey)
	if _, err = p.db.BindTicketEffect(ctx, f.EffectKey, locator, t.publisher); err != nil {
		return false, err
	}
	fresh := false
	prep, err := p.db.GetTicketEffectPreparation(ctx, f.EffectKey)
	if errors.Is(err, store.ErrNotFound) {
		fresh = true
		prep, err = p.db.PrepareTicketEffect(ctx, f.EffectKey, bugLinearRepresentation, f.Title+"\n"+f.Body, true)
	}
	if err != nil {
		return false, err
	}
	// A begin that was ever saved means an earlier attempt may have sent the
	// create: only a read by the retained UUID may follow.
	mayHaveSent := !fresh && !f.AttemptAt.IsZero()

	begun := f
	begun.State = store.BugFilingCreating
	begun.AttemptAt = at
	begun.ChecksDone = 0
	begun.NextCheckAt = time.Time{}
	begun.Reason = ""
	saved, err := p.save(ctx, begun, store.BugFilingAdmitted)
	if err != nil || !saved {
		return false, err
	}
	if err = p.authority(ctx, begun); err != nil {
		back := begun
		if errors.Is(err, errBugFilingRevoked) {
			back.State = store.BugFilingNotCreated
			back.Reason = bugReasonRevokedNothing
			_, e := p.save(ctx, back, store.BugFilingCreating)
			return true, e
		}
		back.State = store.BugFilingAdmitted
		back.AttemptAt = f.AttemptAt
		back.Reason = f.Reason
		_, e := p.save(ctx, back, store.BugFilingCreating)
		return true, errors.Join(err, e)
	}
	next := begun
	uncertain := func(reason string) {
		next.State = store.BugFilingUncertain
		next.Reason = reason
		next.NextCheckAt = at.Add(brainstormtasks.CheckSchedule[0])
	}
	if mayHaveSent {
		uncertain(bugReasonRestart)
		_, err = p.save(ctx, next, store.BugFilingCreating)
		return true, err
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	snapshot, createErr := t.port.CreateLabelledWithID(call, f.Destination, prep.CreationUUID, f.LabelID, title, body)
	cancel()
	var failure *ticket.Failure
	switch {
	case createErr == nil:
		if field := linearDifference(snapshot, prep.CreationUUID, f, prep.Representation, t.publisher); field != "" {
			uncertain(fmt.Sprintf("%s (%s)", bugReasonMismatch, field))
		} else if e := p.adoptLinear(ctx, &next, snapshot, prep); e != nil {
			uncertain(bugReasonMismatch)
		}
	case errors.As(createErr, &failure) && failure.Kind == ticket.Refused:
		next.State = store.BugFilingNotCreated
		next.Reason = "refused before any write; nothing was filed"
	default:
		uncertain(bugReasonUnclear)
	}
	_, err = p.save(ctx, next, store.BugFilingCreating)
	return true, err
}

// checkLinear reads the retained UUID once. It only reads and never creates.
func (p *bugFilingProcessor) checkLinear(ctx context.Context, f store.BugFiling, next *store.BugFiling) {
	t, err := p.linearTarget(ctx, f)
	if err != nil {
		next.Reason = bugReasonLinearUnreached
		return
	}
	prep, err := p.db.GetTicketEffectPreparation(ctx, f.EffectKey)
	if err != nil || prep.CreationUUID == "" {
		next.Reason = bugReasonLinearUnreached
		return
	}
	locator := t.locator(f, prep.CreationUUID)
	call, cancel := context.WithTimeout(ctx, time.Minute)
	s, err := t.port.Resolve(call, locator)
	cancel()
	switch {
	case err != nil:
		next.Reason = bugReasonLinearCheck
	case s.Ref.Locator != locator:
		next.Reason = "an issue carries this bug's id but is not in the expected place"
	default:
		if field := linearDifference(s, prep.CreationUUID, f, prep.Representation, t.publisher); field != "" {
			next.Reason = "an issue carries this bug's id but differs (" + field + ")"
		} else if e := p.adoptLinear(ctx, next, s, prep); e != nil {
			next.Reason = bugReasonLinearUnreached
		}
	}
}

func linearLabelled(s ticket.Snapshot) bool { return slices.Contains(s.Labels, bugLabel) }

// labelLinear confirms the bug label set at creation, or adds the mapped
// label by ID with readback. It never creates a label and never refiles.
func (p *bugFilingProcessor) labelLinear(ctx context.Context, f store.BugFiling) error {
	if !f.NextCheckAt.IsZero() && p.clock().Before(f.NextCheckAt) {
		return nil
	}
	if f.IssueNodeID == "" {
		return errors.New("bug filing: created without an issue identity")
	}
	t, err := p.linearTarget(ctx, f)
	if err != nil {
		if errors.Is(err, errBugLinearMoved) {
			if f.LabelRetries+1 >= bugLabelAttempts {
				return p.labelFailed(ctx, f, "the Linear connection no longer matches this bug filing; filed as "+f.IssueLink+", not labelled; add the bug label to the issue")
			}
			return p.labelLater(ctx, f, bugReasonLinearMoved+"; filed but not yet labelled", bugLabelRetry, true, nil)
		}
		return p.labelLater(ctx, f, bugReasonLabelUnclear, bugLabelRetry, false, err)
	}
	locator := t.locator(f, f.IssueNodeID)
	read := func(_ store.BugFiling) (ticket.Snapshot, error) {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		s, e := t.port.Resolve(call, locator)
		if e == nil && s.Ref.Locator != locator {
			e = errors.New("bug filing: the re-read issue is not the filed one")
		}
		return s, e
	}
	s, err := read(f)
	if err != nil {
		return p.labelLater(ctx, f, bugReasonLabelUnclear, bugLabelRetry, false, err)
	}
	if linearLabelled(s) {
		return p.markLabelled(ctx, f, bugIssue{NodeID: f.IssueNodeID})
	}
	begun := f
	begun.LabelRetries++
	begun.NextCheckAt = p.clock().Add(bugLabelRetry)
	begun.Reason = bugReasonLabelling
	saved, err := p.save(ctx, begun, store.BugFilingCreated)
	if err != nil || !saved {
		return err
	}
	if err = p.authority(ctx, begun); err != nil {
		if !errors.Is(err, errBugFilingRevoked) {
			return err
		}
		return p.labelFailed(ctx, begun, "authority was revoked; filed as "+f.IssueLink+", not labelled")
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	writeErr := t.port.AddLabel(call, s.Ref, bugLabel)
	cancel()
	if writeErr == nil {
		return p.markLabelled(ctx, begun, bugIssue{NodeID: begun.IssueNodeID})
	}
	if again, readErr := read(begun); readErr == nil && linearLabelled(again) {
		return p.markLabelled(ctx, begun, bugIssue{NodeID: begun.IssueNodeID})
	}
	if begun.LabelRetries >= bugLabelAttempts {
		return p.labelFailed(ctx, begun, "could not add the bug label; filed as "+begun.IssueLink+", not labelled; add the bug label to the issue")
	}
	return p.labelLater(ctx, begun, bugReasonLabelUnclear, bugLabelRetry, false, nil)
}
