package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"

	"github.com/cloudvane/litefactory/internal/access"
	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// pickedMode is one mode-picker answer kept for the run that follows.
type pickedMode struct {
	text string
	mode agent.Mode
}

// granteeAdmission is what the access gate made of a non-owner's input.
type granteeAdmission int

const (
	// granteeNone: not a grantee (no gate, no grant, the bot); stay silent.
	granteeNone granteeAdmission = iota
	// granteeRefused: one refusal was posted and the input consumed.
	granteeRefused
	// granteeAllowed: the input continues as an authorized instruction.
	granteeAllowed
)

// mayInstruct reports whether c is an authorized instruction: the owner's, or
// a grantee's whose request the access gate allowed. The owner is answered
// from identity alone, with no grant read, no classifier and no ledger write.
func (d *Daemon) mayInstruct(ctx context.Context, sessionID int64, c github.Comment) (bool, error) {
	if d.isOwnerComment(ctx, c) {
		return true, nil
	}
	grantee, err := d.granteeAuthority(ctx, sessionID, c)
	return grantee != nil, err
}

func (d *Daemon) isOwnerComment(ctx context.Context, c github.Comment) bool {
	ids := d.ticketIdentities(ctx)
	return ids.OwnerUserID > 0 && c.Author.ID == ids.OwnerUserID && c.Author.ID != ids.BotUserID && !c.Author.IsBot && !turn.HasMarker(c.Body)
}

// selfComment reports litefactory's own comments, which never reach a grant.
func (d *Daemon) selfComment(ctx context.Context, author github.User, body string) bool {
	return author.IsBot || author.ID == d.ticketIdentities(ctx).BotUserID || turn.HasMarker(body)
}

// commentIdentities is the qualified author and comment identity a grant and
// an access decision are keyed by. It is built the same way for the gate and
// for every later authority check of the same comment.
func (d *Daemon) commentIdentities(ctx context.Context, sessionID int64, c github.Comment) (actor, source ticket.Identity, err error) {
	if c.QualifiedReply != nil {
		return c.QualifiedReply.Author.Identity, c.QualifiedReply.Identity, nil
	}
	provider, origin := ticket.GitHub, d.filter.Identities().TicketOrigin
	if d.connections != nil {
		mapping, e := d.db.GetSessionTicket(ctx, sessionID)
		if e != nil {
			return actor, source, e
		}
		provider, origin = mapping.Locator.Provider, mapping.Locator.Origin
	}
	if origin == "" {
		origin = "https://api.github.com"
	}
	actor = ticket.Identity{Provider: provider, Origin: origin, RemoteID: strconv.FormatInt(c.Author.ID, 10)}
	if provider == ticket.Linear {
		if actor, err = d.db.TicketIdentityForSlot(ctx, "actor", c.Author.ID); err != nil {
			return actor, source, err
		}
	}
	source = ticket.Identity{Provider: provider, Origin: origin, RemoteID: strconv.FormatInt(c.ID, 10)}
	return actor, source, nil
}

// granteeAuthority returns the allowed decision behind a non-owner comment, or
// nil. The decision must be final, allow, for this session, this author and
// exactly this text; anything else is no authority.
func (d *Daemon) granteeAuthority(ctx context.Context, sessionID int64, c github.Comment) (*turn.GranteeAuthority, error) {
	if d.access == nil || c.ID <= 0 || d.selfComment(ctx, c.Author, c.Body) {
		return nil, nil
	}
	session, err := d.db.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	actor, source, err := d.commentIdentities(ctx, sessionID, c)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validIdentities(actor, source) {
		return nil, nil
	}
	decision, err := d.db.AccessDecisionBySource(ctx, session.RepoID, source)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// A batch decision judged the batch text, which the turn verifies against
	// the batched trigger body; a single comment's is its own text.
	if decision.State != store.AccessDecisionFinal || decision.Decision != store.AccessAllow ||
		decision.SessionID != sessionID || !decision.Actor.Equal(actor) || (decision.CoveredThrough == "" && decision.RequestDigest != access.Digest(c.Body)) {
		return nil, nil
	}
	return &turn.GranteeAuthority{Actor: actor, Handle: c.Author.Login, DecisionID: decision.ID}, nil
}

// hasOpenGrant reports whether a non-owner, non-bot author holds an open grant
// on the task's project: one indexed lookup, nothing else.
func (d *Daemon) hasOpenGrant(ctx context.Context, t *task, c github.Comment) (bool, ticket.Identity, ticket.Identity, error) {
	if d.access == nil || d.selfComment(ctx, c.Author, c.Body) {
		return false, ticket.Identity{}, ticket.Identity{}, nil
	}
	actor, source, err := d.commentIdentities(ctx, t.session.ID, c)
	if errors.Is(err, store.ErrNotFound) {
		return false, actor, source, nil
	}
	if err != nil {
		return false, actor, source, err
	}
	if !validIdentities(actor, source) {
		return false, actor, source, nil
	}
	_, err = d.db.OpenAccessGrant(ctx, t.session.RepoID, actor)
	if errors.Is(err, store.ErrNotFound) {
		return false, actor, source, nil
	}
	return err == nil, actor, source, err
}

// admitGranteeComment runs a third party's comment through the access gate
// when the author holds a grant. A refusal is answered once and consumed.
func (d *Daemon) admitGranteeComment(ctx context.Context, t *task, c github.Comment, log *slog.Logger) (granteeAdmission, error) {
	granted, actor, source, err := d.hasOpenGrant(ctx, t, c)
	if err != nil || !granted {
		return granteeNone, err
	}
	command := turn.ParseCommand(c.Body)
	if command.Kind == turn.KindNone {
		// Nothing to act on, as for the owner: consumed without a reply.
		return granteeNone, nil
	}
	// A decided source replays without its target, so a replay never asks the
	// mode picker again.
	var target access.Target
	text, covered := c.Body, ""
	if decided, err := d.db.AccessDecisionBySource(ctx, t.session.RepoID, source); errors.Is(err, store.ErrNotFound) || (err == nil && decided.State != store.AccessDecisionFinal) {
		if target, err = d.granteeTarget(ctx, t, c.ID, command); err != nil {
			return granteeNone, err
		}
		if target.Entry == access.EntryAnswer {
			// An answer is judged as the whole batch it opens, exactly as the
			// run will read it; the decision covers the batch's last comment.
			if text, covered, err = d.granteeBatch(ctx, t, c, ""); err != nil {
				return granteeNone, err
			}
		}
	} else if err != nil {
		return granteeNone, err
	} else {
		replayPickedMode(t, decided, command)
		if decided.CoveredThrough != "" {
			// A replay rebuilds the batch its decision covers, through the same end.
			if text, covered, err = d.granteeBatch(ctx, t, c, decided.CoveredThrough); err != nil {
				return granteeNone, err
			}
		}
	}
	out, err := d.access.Check(ctx, access.Request{
		Repo: t.repo, SessionID: t.session.ID, Actor: actor, Handle: c.Author.Login, Source: source,
		CoveredThrough: covered, IssueTitle: t.issue.Title, IssueBody: t.issue.Body, Text: text, Command: command.Name,
		CodeControl: isCodeAuthorityBody(c.Body), Target: target,
	})
	if errors.Is(err, access.ErrNoGrant) {
		return granteeNone, nil
	}
	if err != nil {
		return granteeNone, err
	}
	if out.Allowed() {
		log.InfoContext(ctx, "grantee request allowed by the access gate", slog.Int64("access_decision_id", out.DecisionID), slog.Bool("replayed", out.Replayed))
		return granteeAllowed, nil
	}
	log.InfoContext(ctx, "grantee request refused by the access gate", slog.String("refusal", string(out.Refusal)), slog.Int64("access_decision_id", out.DecisionID))
	d.commentFate(ctx, t, c.ID, fateIgnored, "refused by the access gate")
	if err := d.refuseAccess(ctx, t, c.ID, c.Author.Login, out); err != nil {
		return granteeRefused, d.leave(ctx, t, log, "could not answer a refused grantee request", err)
	}
	return granteeRefused, nil
}

// granteeBatch is the text and covered-through source of the batch a
// grantee's comment opens; a comment alone is its own text with no cover. A
// non-empty through ends the batch at the comment with that source, as a
// replay of its decision requires.
func (d *Daemon) granteeBatch(ctx context.Context, t *task, c github.Comment, through string) (string, string, error) {
	sources := map[int64]string{}
	var upTo int64
	for _, m := range t.queue {
		if m.ID <= c.ID || m.Author.ID != c.Author.ID {
			continue
		}
		_, source, err := d.commentIdentities(ctx, t.session.ID, m)
		if err != nil {
			return c.Body, "", err
		}
		sources[m.ID] = source.RemoteID
		if through != "" && source.RemoteID == through {
			upTo = m.ID
			break
		}
	}
	if through != "" && upTo == 0 {
		return c.Body, "", nil
	}
	trigger := turn.Trigger{ReplyToCommentID: c.ReplyToCommentID, CommentID: c.ID, Body: c.Body, Author: c.Author.Login, AuthorID: c.Author.ID, CreatedAt: c.CreatedAt}
	batch, reached, err := d.authorBatch(ctx, t, trigger, c.Author.ID, upTo)
	if err != nil || !reached || batch.CommentID == c.ID {
		return c.Body, "", err
	}
	return batch.Body, sources[batch.CommentID], nil
}

// refuseAccess posts the one refusal for a gate outcome. The reply path dedupes
// per source and consumes a comment only after delivery.
func (d *Daemon) refuseAccess(ctx context.Context, t *task, commentID int64, handle string, out access.Outcome) error {
	_, err := d.reportReply(ctx, turn.ReplyRequest{
		Ref: t.ref, SessionID: t.session.ID, CommentID: commentID,
		Refusal: accessReplyKind(out.Refusal), AccessActor: handle, AccessReason: out.Reason,
	})
	return err
}

func accessReplyKind(r access.Refusal) report.ReplyKind {
	switch r {
	case access.RefusedDenied:
		return report.ReplyAccessDenied
	case access.RefusedOwnerOnly:
		return report.ReplyAccessOwnerOnly
	case access.RefusedLimited:
		return report.ReplyAccessLimited
	default:
		return report.ReplyAccessUnchecked
	}
}

// granteeTarget is what the input would act on: an answer at the node the
// session's graph waits on, the workflow a command names, or the workflow the
// mode picker chooses for free text. An unknown target leaves the workflow
// empty, and the gate's facts then refuse it as unchecked.
func (d *Daemon) granteeTarget(ctx context.Context, t *task, triggerID int64, command turn.Command) (access.Target, error) {
	if mode, commanded := command.Mode(); commanded {
		workflow, _ := agent.WorkflowForMode(mode)
		return access.Target{Entry: access.EntryStart, Workflow: workflow}, nil
	}
	if command.Kind != turn.KindFreeText {
		return access.Target{Entry: access.EntryStart}, nil
	}
	if triggerID > 0 {
		target, waiting, err := d.waitingTarget(ctx, t)
		if err != nil || waiting {
			return target, err
		}
	}
	mode, err := d.granteeMode(ctx, t, triggerID, command)
	if err != nil {
		return access.Target{}, err
	}
	workflow, _ := agent.WorkflowForMode(mode)
	return access.Target{Entry: access.EntryStart, Workflow: workflow}, nil
}

// waitingTarget is the node the session's latest graph waits on for a reply.
func (d *Daemon) waitingTarget(ctx context.Context, t *task) (access.Target, bool, error) {
	instance, err := d.db.LatestSessionGraphInstance(ctx, t.session.ID)
	if errors.Is(err, store.ErrNotFound) {
		return access.Target{}, false, nil
	}
	if err != nil {
		return access.Target{}, false, err
	}
	if instance.Status != store.GraphRunning {
		return access.Target{}, false, nil
	}
	wait, err := d.db.GetGraphOwnerWait(ctx, instance.ID)
	if errors.Is(err, store.ErrNotFound) {
		return access.Target{}, false, nil
	}
	if err != nil {
		return access.Target{}, false, err
	}
	if wait.State != "waiting" {
		return access.Target{}, false, nil
	}
	definition, err := d.db.GetGraphDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return access.Target{}, false, err
	}
	node := instance.CurrentNode
	if node == "" {
		node = wait.OriginNode
	}
	target := access.Target{Entry: access.EntryAnswer, Node: node}
	// A mode-owned definition pins only its mode; the workflow follows from it.
	workflow, err := agent.ResolveWorkflow(agent.Workflow(definition.Workflow), agent.Mode(definition.Mode))
	if err != nil {
		return target, true, nil //nolint:nilerr // Fail closed: an unknown workflow is refused as unchecked.
	}
	target.Workflow = workflow
	if d.accessWorkflows == nil {
		// Without the definition the approve reply is unknown: fail closed.
		target.Workflow = ""
		return target, true, nil
	}
	specs, err := turn.SpecsForWorkflow(target.Workflow)
	if err != nil {
		target.Workflow = ""
		return target, true, nil //nolint:nilerr // Fail closed: an unknown workflow is refused as unchecked.
	}
	def, err := d.accessWorkflows(ctx, t.repo, target.Workflow, specs)
	if err != nil {
		target.Workflow = ""
		return target, true, nil //nolint:nilerr // Fail closed: an unreadable graph is refused as unchecked.
	}
	target.ApproveReply = access.ApproveReplyOf(def, node)
	return target, true, nil
}

// granteeMode is the mode the existing picker would choose for this free
// text, read without the selection's effects; the classifier's answer is kept
// so the run that follows uses the same mode.
func (d *Daemon) granteeMode(ctx context.Context, t *task, triggerID int64, command turn.Command) (agent.Mode, error) {
	if t.continuation != nil {
		return agent.Mode(t.continuation.Context.Mode), nil
	}
	if selection, err := d.db.GetModeSelection(ctx, t.session.ID, triggerID); err == nil {
		mode, err := storedMode(selection)
		if err != nil {
			return "", nil //nolint:nilerr // Fail closed: no mode leaves the target unknown, refused as unchecked.
		}
		return mode, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return "", err
	}
	observed, labelled, err := resolveModeLabel(t.issue.Labels)
	if err != nil {
		return "", nil //nolint:nilerr // Fail closed: no mode leaves the target unknown, refused as unchecked.
	}
	feature, featureErr := d.db.GetManagedFeature(ctx, t.ref.String())
	if featureErr != nil && !errors.Is(featureErr, store.ErrNotFound) {
		return "", featureErr
	}
	switch {
	case t.implementing:
		return agent.ModeImplement, nil
	case t.refining:
		return agent.ModePlan, nil
	case t.validating:
		return agent.ModeValidate, nil
	}
	if featureErr == nil && feature.State != "planning" && command.Name == "" && !turn.ManagedReplan(command) {
		discussion, err := turn.ValidateDiscussionOpen(ctx, d.db, t.session.ID)
		if err != nil {
			return "", err
		}
		if discussion {
			return agent.ModeValidate, nil
		}
	}
	switch {
	case featureErr == nil && feature.State != "planning":
		if turn.ManagedReplan(command) {
			return agent.ModePlan, nil
		}
		return agent.ModeImplement, nil
	case labelled:
		return observed, nil
	}
	mode, err := d.classifyMode(ctx, t, command.Text)
	if err != nil {
		// The picker could not choose: the target stays unknown.
		return "", nil //nolint:nilerr // Fail closed: refused as unchecked.
	}
	t.accessPicked = &pickedMode{text: command.Text, mode: mode}
	return mode, nil
}

// issueIdentities is the qualified author and issue identity an issue body's
// grant and access decision are keyed by.
func (d *Daemon) issueIdentities(ctx context.Context, t *task) (actor, source ticket.Identity, err error) {
	provider, origin, remote := ticket.GitHub, d.filter.Identities().TicketOrigin, ""
	if t.issue.ID > 0 {
		remote = strconv.FormatInt(t.issue.ID, 10)
	}
	if d.connections != nil {
		mapping, e := d.db.GetSessionTicket(ctx, t.session.ID)
		if e != nil {
			return actor, source, e
		}
		provider, origin, remote = mapping.Locator.Provider, mapping.Locator.Origin, mapping.RemoteID
	}
	if origin == "" {
		origin = "https://api.github.com"
	}
	actor = ticket.Identity{Provider: provider, Origin: origin, RemoteID: strconv.FormatInt(t.issue.Author.ID, 10)}
	if t.issue.QualifiedSnapshot != nil {
		actor = t.issue.QualifiedSnapshot.Author.Identity
	} else if provider == ticket.Linear {
		if actor, err = d.db.TicketIdentityForSlot(ctx, "actor", t.issue.Author.ID); err != nil {
			return actor, source, err
		}
	}
	return actor, ticket.Identity{Provider: provider, Origin: origin, RemoteID: remote}, nil
}

// issueGrantee reports whether the issue's non-owner author holds an open grant.
func (d *Daemon) issueGrantee(ctx context.Context, t *task) (bool, error) {
	actor, source, err := d.issueIdentities(ctx, t)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !validIdentities(actor, source) {
		return false, nil
	}
	_, err = d.db.OpenAccessGrant(ctx, t.session.RepoID, actor)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

// admitGranteeBody runs a grantee's issue body through the access gate. It
// returns the authority when allowed; a refusal is posted once on the issue and
// nil returned, and the decision row replays it on any later sight.
func (d *Daemon) admitGranteeBody(ctx context.Context, t *task, command turn.Command, log *slog.Logger) (*turn.GranteeAuthority, error) {
	actor, source, err := d.issueIdentities(ctx, t)
	if err != nil {
		return nil, err
	}
	var target access.Target
	if decided, err := d.db.AccessDecisionBySource(ctx, t.session.RepoID, source); errors.Is(err, store.ErrNotFound) || (err == nil && decided.State != store.AccessDecisionFinal) {
		if target, err = d.granteeTarget(ctx, t, 0, command); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	out, err := d.access.Check(ctx, access.Request{
		Repo: t.repo, SessionID: t.session.ID, Actor: actor, Handle: t.issue.Author.Login, Source: source,
		IssueTitle: t.issue.Title, IssueBody: t.issue.Body, Command: command.Name,
		CodeControl: isCodeAuthorityBody(t.issue.Body), Target: target,
	})
	if errors.Is(err, access.ErrNoGrant) {
		log.DebugContext(ctx, "not running the issue body; its author holds no grant")
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if out.Allowed() {
		log.InfoContext(ctx, "grantee issue body allowed by the access gate", slog.Int64("access_decision_id", out.DecisionID), slog.Bool("replayed", out.Replayed))
		return &turn.GranteeAuthority{Actor: actor, Handle: t.issue.Author.Login, DecisionID: out.DecisionID}, nil
	}
	log.InfoContext(ctx, "grantee issue body refused by the access gate", slog.String("refusal", string(out.Refusal)), slog.Int64("access_decision_id", out.DecisionID))
	return nil, d.refuseAccess(ctx, t, 0, t.issue.Author.Login, out)
}

// validIdentities reports whether both qualified identities are resolved.
func validIdentities(actor, source ticket.Identity) bool {
	return actor.Validate() == nil && source.Validate() == nil
}

// replayPickedMode keeps a replayed free-text start on the workflow its
// decision judged, so a restart never lets the mode picker choose another.
func replayPickedMode(t *task, decided store.AccessDecision, command turn.Command) {
	if command.Kind != turn.KindFreeText || decided.Decision != store.AccessAllow {
		return
	}
	var facts access.TargetFacts
	if json.Unmarshal([]byte(decided.TargetJSON), &facts) != nil || facts.Entry != access.EntryStart {
		return
	}
	for _, mode := range []agent.Mode{agent.ModeResearch, agent.ModePlan, agent.ModeImplement, agent.ModeValidate, agent.ModeSpike} {
		if workflow, ok := agent.WorkflowForMode(mode); ok && string(workflow) == facts.Workflow {
			t.accessPicked = &pickedMode{text: command.Text, mode: mode}
			return
		}
	}
}
