package daemon

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

// DoctorRepairComments is the confirmed forge reply transport.
type DoctorRepairComments interface {
	PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error)
}

// FactoryDoctorRepairEffects binds external repairs to factory-owned dependencies.
type FactoryDoctorRepairEffects struct {
	DB        *store.DB
	Git       *git.Binary
	CloneGate turn.CloneGate
	Author    git.Author
	Advisors  map[string]turn.ConflictAdvisor
	Comments  DoctorRepairComments
	BotID     int64
	Redactor  *redact.Redactor
}

var _ doctor.RepairEffects = (*FactoryDoctorRepairEffects)(nil)

// Execute applies only the exact reserved external step through factory dependencies.
func (f *FactoryDoctorRepairEffects) Execute(ctx context.Context, target naming.Ref, op store.DoctorRepair, step store.DoctorRepairStep, provider string) (store.DoctorRepairStep, error) {
	if f.DB == nil || op.TaskRef != target.String() {
		return step, errors.New("external repair target mismatch")
	}
	steps, e := f.DB.DoctorRepairSteps(ctx, op.ID)
	if e != nil {
		return step, e
	}
	matched := false
	for _, approved := range steps {
		if approved.Number == step.Number && approved.Kind == step.Kind && approved.Target == step.Target && approved.Before == step.Before && approved.After == step.After {
			matched = true
			break
		}
	}
	if !matched {
		return step, errors.New("external repair does not match approved manifest")
	}
	switch step.Kind {
	case "consume_comment":
		return f.consume(ctx, target, op, step)
	case "sync_branch":
		options, e := f.syncOptions(ctx, target, step)
		if e != nil {
			return step, e
		}
		options.Advisor = f.Advisors[provider]
		options.Owner = "local-owner"
		if identity, e := f.DB.GetIdentity(ctx, store.ForgeGitHub, store.RoleOwner); e == nil {
			options.Owner = identity.Handle
		}
		options.OperationID = op.ProposalID
		options.Model = provider
		options.Task = target.String()
		options.RecordBackup = func(ctx context.Context, head string) error {
			return f.DB.BeginDoctorBranchSync(ctx, op.ID, step.Number, head)
		}
		result, e := turn.SyncDoctorBranch(ctx, options)
		if e != nil {
			return step, e
		}
		// Origin already holds result.Head: record it on a short detached
		// context and never reset the local branch away from the remote.
		finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		completed, e := f.DB.FinishDoctorBranchSync(finishCtx, op.ID, step.Number, result.Head)
		if e != nil {
			return step, e
		}
		return completed, nil
	default:
		return step, errors.New("unsupported external repair kind")
	}
}

func (f *FactoryDoctorRepairEffects) syncOptions(ctx context.Context, target naming.Ref, step store.DoctorRepairStep) (turn.DoctorSyncOptions, error) {
	var out turn.DoctorSyncOptions
	if f.Git == nil || f.CloneGate == nil {
		return out, errors.New("factory git repair dependencies unavailable")
	}
	session, e := f.DB.GetSessionByTaskRef(ctx, target.String())
	if e != nil {
		return out, e
	}
	repo, e := f.DB.GetRepo(ctx, session.RepoID)
	if e != nil {
		return out, e
	}
	if step.Target != "branch:"+session.Branch || session.Branch == "" || session.WorktreePath == "" {
		return out, errors.New("approved branch differs from task branch")
	}
	clone, e := f.Git.Open(ctx, repo.Path)
	if e != nil {
		return out, e
	}
	release, e := f.CloneGate.Acquire(ctx, repo.Path)
	if e != nil {
		return out, e
	}
	defer release()
	// Repair may only reuse an existing registered worktree: creation is outside approval.
	entries, e := clone.WorktreeList(ctx)
	if e != nil {
		return out, e
	}
	found := false
	for _, entry := range entries {
		if entry.Path == session.WorktreePath && entry.Branch == "refs/heads/"+session.Branch {
			found = true
		}
	}
	if !found {
		return out, errors.New("task worktree is not registered on approved branch")
	}
	base := repo.BaseBranch
	if story, e := f.DB.ImplementStoryByWorkerSession(ctx, session.ID); e == nil {
		parent, e := f.DB.GetSessionByTaskRef(ctx, story.Parent)
		if e != nil {
			return out, e
		}
		if parent.Branch == "" {
			return out, errors.New("implement parent branch unavailable")
		}
		base = parent.Branch
	}
	result, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: session.WorktreePath, Branch: session.Branch, BaseBranch: base})
	if e != nil {
		return out, e
	}
	author := f.Author
	if author.Name == "" {
		author.Name, e = clone.ConfigGet(ctx, "user.name")
		if e != nil {
			return out, e
		}
	}
	if author.Email == "" {
		author.Email, e = clone.ConfigGet(ctx, "user.email")
		if e != nil {
			return out, e
		}
	}
	out = turn.DoctorSyncOptions{Worktree: result.Worktree, CloneGate: f.CloneGate, CloneRoot: repo.Path, Branch: session.Branch, BeforeHead: step.Before, Base: "origin/" + base, BaseHead: step.After, Author: author}
	return out, nil
}

func (f *FactoryDoctorRepairEffects) consume(ctx context.Context, target naming.Ref, op store.DoctorRepair, step store.DoctorRepairStep) (store.DoctorRepairStep, error) {
	if f.Comments == nil || f.Redactor == nil || f.BotID <= 0 {
		return step, errors.New("confirmed doctor reply transport unavailable")
	}
	session, e := f.DB.PrepareDoctorConsumeComment(ctx, op.ID, step.Number)
	if e != nil {
		return step, consumeStateFailure(e)
	}
	source, e := strconv.ParseInt(step.After, 10, 64)
	if e != nil {
		return step, e
	}
	key := fmt.Sprintf("doctor-consume:%s:%d", op.ID, step.Number)
	marker := fmt.Sprintf("<!-- lf:doctor-consume operation=%s step=%d -->", op.ID, step.Number)
	body, e := f.Redactor.Apply(ctx, "The owner approved consuming this comment through /doctor apply. It will not be offered as new work.\n\n"+marker, 65536)
	if e != nil {
		return step, e
	}
	if body.Truncated() || !strings.HasSuffix(body.String(), marker) {
		return step, errors.New("doctor reply exceeds complete-content bound")
	}
	delivery, claimed, e := f.DB.ClaimDoctorConsumeDelivery(ctx, op.ID, step.Number, store.ReportDelivery{Key: key, Version: 1, SessionID: session, Task: target.String(), Destination: target.String(), TriggerID: source, TriggerNamespace: "comment", Purpose: "reply", BotUserID: f.BotID, Body: body.String(), Marker: marker})
	if e != nil {
		return step, consumeStateFailure(e)
	}
	if !claimed || !delivery.PossiblySent {
		return step, doctor.ErrRepairReplyUncertain
	}
	comment, e := f.Comments.PostComment(ctx, github.Repo{Owner: target.Owner, Name: target.Repo}, target.Issue, body)
	if e != nil {
		// A typed rate-limit refusal proves nothing was posted: the retained
		// reply is no longer possibly sent, so it can be settled and dropped.
		if errors.Is(e, github.ErrRateLimited) {
			e = errors.Join(e, f.DB.UnmarkDoctorConsumeDelivery(context.WithoutCancel(ctx), op.ID, step.Number, key))
		}
		return step, fmt.Errorf("%w: %w", replyFailure(e), e)
	}
	if comment.ID <= 0 || comment.Author.ID != f.BotID {
		return step, fmt.Errorf("%w: doctor reply lacks trusted bot receipt", doctor.ErrRepairReplyUncertain)
	}
	if e = f.DB.ConfirmDoctorConsumeDelivery(ctx, op.ID, step.Number, key, comment.ID, comment.HTMLURL); e != nil {
		return step, consumeStateFailure(e)
	}
	applied, e := f.DB.ApplyDoctorConsumeComment(ctx, op.ID, step.Number, key)
	if e != nil {
		return applied, consumeStateFailure(e)
	}
	return applied, nil
}

// replyFailure chooses the fixed category of a failed reply POST from the
// forge failure's kind: a rate limit or a local hold deferral and a definite
// refusal prove nothing was posted; anything else leaves the outcome uncertain.
func replyFailure(e error) error {
	var githubRate *github.RateLimitError
	var ticketRate *ticket.RateLimitError
	var failure *ticket.Failure
	switch {
	case errors.Is(e, ticket.ErrDiscoveryDeferred), errors.Is(e, github.ErrRateLimited), errors.As(e, &githubRate), errors.As(e, &ticketRate):
		return doctor.ErrRepairReplyRateLimited
	case errors.As(e, &failure) && failure.Kind != ticket.Uncertain:
		return doctor.ErrRepairReplyRefused
	}
	return doctor.ErrRepairReplyOutcomeUncertain
}

// consumeStateFailure gives a store refusal of the consume step its fixed
// category; any other error is returned unchanged.
func consumeStateFailure(e error) error {
	switch {
	case errors.Is(e, store.ErrDoctorWatermarkDrift):
		return fmt.Errorf("%w: %w", doctor.ErrRepairCommentDrifted, e)
	case errors.Is(e, store.ErrDoctorFenced):
		return fmt.Errorf("%w: %w", doctor.ErrRepairStateChanged, e)
	}
	return e
}

// Verify rereads the external after-state without repeating an effect.
func (f *FactoryDoctorRepairEffects) Verify(ctx context.Context, target naming.Ref, step store.DoctorRepairStep) error {
	if step.Kind == "consume_comment" {
		session, e := f.DB.GetSessionByTaskRef(ctx, target.String())
		if e != nil {
			return e
		}
		watermark, ok, e := f.DB.GetWatermark(ctx, session.ID)
		if e != nil {
			return e
		}
		if !ok || strconv.FormatInt(watermark.LastCommentID, 10) != step.After {
			return errors.New("doctor consumption recheck drifted")
		}
		return nil
	}
	if step.Kind != "sync_branch" {
		return errors.New("unsupported external repair verification")
	}
	options, e := f.syncOptions(ctx, target, step)
	if e != nil {
		return e
	}
	release, e := f.CloneGate.Acquire(ctx, options.CloneRoot)
	if e != nil {
		return e
	}
	defer release()
	divergence, e := options.Worktree.Divergence(ctx, step.After)
	if e != nil {
		return e
	}
	changes, e := options.Worktree.Status(ctx)
	if e != nil {
		return e
	}
	head, e := options.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		return e
	}
	if divergence.Behind != 0 || len(changes) != 0 || head != step.ObservedAfter {
		return errors.New("branch synchronization recheck drifted")
	}
	return nil
}

// NewDoctorRepairCloneGate supplies the same clone serialization used by workflow turns.
func NewDoctorRepairCloneGate() turn.CloneGate { return newCloneGate() }
