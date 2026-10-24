package report

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

// ReplyKind is why litefactory is answering a comment without running
// anything.
//
// It is this package's own enum rather than internal/turn's CommandKind, and
// deliberately so: internal/turn depends on this package to render its replies,
// so the dependency cannot point the other way. Mapping one to the other is
// three lines at the call site and keeps the wording of the reply — which is
// user-visible output — owned here with the rest of it.
type ReplyKind int

const (
	// ReplyInvalid is the zero value and renders nothing. A reply is always
	// sent for a reason, and a reply with no reason is a caller bug rather
	// than a message worth inventing.
	ReplyInvalid ReplyKind = iota

	// ReplyUnknownCommand answers a slash command that is not in the
	// vocabulary at all, which is usually a typo.
	ReplyUnknownCommand

	// ReplyEmptySpike answers a bare "/spike" with nothing after it (AC-2.20).
	ReplyEmptySpike

	// ReplyAmbiguousAgentLabels answers an issue carrying more than one Coding
	// Agent label. It names neither label.
	ReplyAmbiguousAgentLabels

	// ReplyMalformedAgentLabel answers a Coding Agent label whose value is not
	// the shape of an agent name at all, the empty value included — the
	// expected shape. It carries no field for the label text to travel in.
	ReplyMalformedAgentLabel

	// ReplyUnknownAgent answers a Coding Agent label naming a value that is a
	// well-formed agent name but not one litefactory has a [Runner]
	// implementation registered for. Unlike [ReplyMalformedAgentLabel] it may
	// name what was asked for, because a value that passed the agent-name
	// shape check makes it safe to quote back.
	ReplyUnknownAgent

	// ReplyAgentSwitchRefused answers an automatic-recovery continuation whose
	// named segment no longer belongs to the coding-agent tool this comment's
	// labels resolved to. This is narrower than it sounds: an ordinary
	// comment that simply asks for a different coding-agent tool is not
	// refused — internal/turn switches the segment to the newly resolved
	// tool in place and runs it, cold, under the new tool. This reply exists
	// only for automatic recovery, which names the exact segment and run it
	// is continuing; if the label changed between when that continuation was
	// recorded and when it runs, recovery cannot silently follow the label to
	// a tool the run it is recovering never used, so it refuses instead.
	ReplyAgentSwitchRefused

	// ReplyRefusesSync answers a turn whose task branch could not be merged
	// with the base branch cleanly (US-4): [git.Worktree.MergePreview] found
	// conflicting paths and the configured policy says a conflict of this
	// kind stops the run rather than merely disclosing it. Like
	// [ReplyAgentSwitchRefused] it is drawn from the state of the world
	// rather than from the comment's command, and it has its own request
	// shape: [Reply.ConflictPaths] carries the paths that collided, exactly
	// as the merge preview reported them.
	ReplyRefusesSync

	// ReplySuggestsResolution answers the same blocked-merge state of the
	// world [ReplyRefusesSync] does, with one addition: a suggested
	// resolution litefactory asked a tool-free model call for, validated
	// locally, and persisted before this reply was rendered (US-11). It is
	// drawn instead of [ReplyRefusesSync] only when a proposal was actually
	// produced; every other blocked-merge turn — the feature disabled, the
	// advisor unavailable or failed, its answer rejected by local
	// revalidation — still answers with [ReplyRefusesSync], optionally
	// carrying [Reply.ConflictSuggestionFailed]. [Reply.ConflictProposal]
	// carries the suggestion itself: only the paths and the model's own
	// resolution text, never the raw conflicting content that was shown to
	// the model (S-9, S-10).
	ReplySuggestsResolution

	// ReplyRefusesSyncTooFarBehind answers a turn whose task branch is
	// further behind its base than [config.Settings.AutoSyncMaxBehind]
	// allows. No merge was previewed and no conflict is known: distance
	// alone is the refusal.
	//
	// It is deliberately a refusal and not a skip. A skip would let the run
	// proceed against a branch known to be stale, which is the failure
	// [config.Settings.AutoSyncBase] exists to prevent; the cap says this
	// branch is too far gone to merge unattended, and the honest answer to
	// that is to stop and say so. Being switched off is still a skip —
	// an operator who turned sync off asked for the run to go ahead.
	ReplyRefusesSyncTooFarBehind

	// ReplyMalformedModeLabel answers a malformed or unknown Mode label.
	ReplyMalformedModeLabel
	// ReplyBugModeSwitch refuses replacing an admitted standalone task.
	ReplyBugModeSwitch
	// ReplyBugUnavailable retains admission while its execution package is absent.
	ReplyBugUnavailable
	// ReplyAmbiguousModeLabels answers multiple competing Mode labels.
	ReplyAmbiguousModeLabels
	// ReplyConflictingModeSelection answers an owner edit conflicting with a pending transition.
	ReplyConflictingModeSelection
	// ReplyStoryLocked refuses edits to scope already promoted to implementation.
	ReplyStoryLocked
	// ReplyStoryPlanOnly redirects child workflow commands to the parent.
	ReplyStoryPlanOnly
	// ReplyStoryPlanning acknowledges admission into the parent planning session.
	ReplyStoryPlanning

	// ReplyLegacyToolLabel answers an issue or comment still carrying the old
	// lf:tools=<profile> label syntax.
	//
	// It exists so a legacy label is never treated the same as no label at
	// all. Silently ignoring it would resolve the run with no tool profile
	// and no indication anything was wrong — the exact silent permission drop
	// this whole feature exists to prevent, just relocated to the rename. The
	// reply names the new notation so the operator relabels rather than
	// assuming a profile is still active.
	//
	// It carries no field for the label text to travel in (SEC-5): the label is a
	// syntax error regardless of what value follows it, so there is nothing
	// about it worth quoting back.
	ReplyLegacyToolLabel

	// ReplyScheduleInvalid answers a /schedule control whose factory grammar
	// could not be accepted. It is deliberately a factory reply: no agent sees
	// an invalid scheduling request.
	ReplyScheduleInvalid

	// ReplyScheduleConfirmed confirms that Litefactory durably recorded a
	// direct scheduling control before consuming its source comment.
	ReplyScheduleConfirmed

	// ReplyScheduleBlocked tells the owner that a scheduled item will not run
	// because one of its prerequisites failed.
	ReplyScheduleBlocked

	// ReplyScheduleProposalReady acknowledges factory-owned advice without exposing
	// an agent's proposed payload in a signed forge comment.
	ReplyScheduleProposalReady
	// ReplyScheduleProposalAccepted confirms the owner accepted scheduled work.
	ReplyScheduleProposalAccepted
	// ReplyScheduleProposalRejected confirms the owner rejected scheduling advice.
	ReplyScheduleProposalRejected
	// ReplyScheduleProposalFailed reports that no usable scheduling proposal was produced.
	ReplyScheduleProposalFailed

	// ReplyConfirmInvalid answers a /confirm control whose factory grammar
	// could not be accepted: a missing or non-positive proposal id, or
	// anything other than the exact confirmation phrase (US-12, S-14). It
	// never echoes the comment's own text (SEC-5).
	ReplyConfirmInvalid

	// ReplyConfirmNotOwner answers a confirmation from someone other than
	// this task's issue owner (watch-out #4 of US-12). Nothing is applied
	// and the proposal's status does not move.
	ReplyConfirmNotOwner

	// ReplyConfirmUnknownProposal answers a confirmation naming a proposal
	// id this task has no record of — including a real id that belongs to a
	// different task, reported identically so a confirmation can never be
	// used to learn what exists elsewhere.
	ReplyConfirmUnknownProposal

	// ReplyConfirmExpiredProposal answers a confirmation naming a proposal
	// whose TTL already passed.
	ReplyConfirmExpiredProposal

	// ReplyConfirmSupersededProposal answers a confirmation naming a
	// proposal that a later proposal already superseded, or one this
	// package found to no longer match the current base/branch tips at
	// apply time (AC-12.4).
	ReplyConfirmSupersededProposal

	// ReplyConfirmAlreadyApplied answers a second confirmation of a
	// proposal that was already applied (AC-12.9): a no-op, never a second
	// commit.
	ReplyConfirmAlreadyApplied

	// ReplyConfirmFailed answers a confirmation that passed every gate but
	// whose application could not be completed (AC-12.6): the branch was
	// rolled back to exactly where it was, and the proposal is still
	// pending.
	ReplyConfirmFailed

	// ReplyAppliesResolution answers a successful application (AC-12.5): it
	// discloses the model, the confirming handle, the files that were
	// written, and the commit that completed the merge.
	ReplyAppliesResolution
	// ReplyCreationProposal presents exact retained owner-review content.
	ReplyCreationProposal
	// ReplyCreationRejected explains a confirmed creation intake refusal.
	ReplyCreationRejected
	// ReplyCreationAccepted acknowledges durable creation consent.
	ReplyCreationAccepted

	// ReplyGraphReplyBetweenQuestions refuses an owner comment that was written
	// while the Research workflow was working between two questions, before the
	// question that is now open was published. It can never answer that
	// question, so it is refused once, visibly, and consumed rather than
	// retried forever. It is appended last so no existing kind is renumbered.
	ReplyGraphReplyBetweenQuestions
	// ReplyBugCommentNotApplied answers an owner comment the Bug workflow
	// could not apply at its current step. It is posted and the comment is
	// consumed, so a refused comment never stalls the issue's queue.
	ReplyBugCommentNotApplied
	// ReplyGraphOwnerWaitRefused answers an owner comment that arrived while
	// the workflow was holding another owner reply or question, so it was not
	// used. It is posted and the comment is consumed.
	ReplyGraphOwnerWaitRefused
	// ReplyImplementCommentNotApplied answers parent guidance with no parked target.
	ReplyImplementCommentNotApplied
	// ReplyGraphResumeRefused answers an owner reply on work stopped at a node
	// that the reply could not resume, naming the node and the reason. It is
	// posted and the comment is consumed.
	ReplyGraphResumeRefused
	// ReplyMissingModeMapping answers a missing connection label mapping.
	ReplyMissingModeMapping
	// ReplyMissingTeam answers admission without a repository team binding.
	ReplyMissingTeam
	// ReplyUnsupportedWorkflow answers a selected team without the requested workflow.
	ReplyUnsupportedWorkflow
	// ReplyMissingTicketPublisher answers an unavailable verified publishing identity.
	ReplyMissingTicketPublisher
	// ReplyDoctorUnsupported answers Doctor commands on unsupported ticket providers.
	ReplyDoctorUnsupported
	// ReplyCodeControlRefused answers a delivery command (/validate-ship,
	// /validate-push, /validate-merge, /validate-close, /deliver or /merge)
	// from an author without code authority on this task. It is posted and the
	// comment is consumed, so the command is never dropped silently.
	ReplyCodeControlRefused
	// ReplyValidateCommentNotApplied answers a plain parent comment while the
	// parent's latest Validate instance is running, at any step or owner wait.
	// Nothing runs; it is posted and the comment is consumed (#1123).
	ReplyValidateCommentNotApplied
	// ReplyImplementFinished answers a plain parent comment after Implement
	// finished with every story merged, when Validate is started by command.
	ReplyImplementFinished
	// ReplyImplementFinishedAutoValidate is [ReplyImplementFinished] when
	// Validate starts automatically.
	ReplyImplementFinishedAutoValidate
	// ReplyImplementFinishedValidated is [ReplyImplementFinished] when Validate
	// has already finished on the finished Implement.
	ReplyImplementFinishedValidated
)

// String renders the kind for logs and test failures.
func (k ReplyKind) String() string {
	switch k {
	case ReplyMissingTicketPublisher:
		return "missing_ticket_publisher"
	case ReplyCreationAccepted:
		return "creation_accepted"
	case ReplyCreationRejected:
		return "creation_rejected"
	case ReplyCreationProposal:
		return "creation_proposal"
	case ReplyUnknownCommand:
		return "unknown_command"
	case ReplyEmptySpike:
		return "empty_spike"
	case ReplyAmbiguousAgentLabels:
		return "ambiguous_agent_labels"
	case ReplyMalformedAgentLabel:
		return "malformed_agent_label"
	case ReplyUnknownAgent:
		return "unknown_agent"
	case ReplyAgentSwitchRefused:
		return "agent_switch_refused"
	case ReplyRefusesSync:
		return "sync_refused"
	case ReplySuggestsResolution:
		return "sync_refused_with_suggestion"
	case ReplyRefusesSyncTooFarBehind:
		return "sync_refused_too_far_behind"
	case ReplyBugModeSwitch:
		return "bug_mode_switch_refused"
	case ReplyBugUnavailable:
		return "bug_execution_unavailable"
	case ReplyMalformedModeLabel:
		return "malformed_mode_label"
	case ReplyAmbiguousModeLabels:
		return "ambiguous_mode_labels"
	case ReplyStoryPlanning:
		return "story_planning"
	case ReplyStoryLocked:
		return "story_locked"
	case ReplyStoryPlanOnly:
		return "story_plan_only"
	case ReplyMissingTeam:
		return "missing_team"
	case ReplyDoctorUnsupported:
		return "doctor_unsupported"
	case ReplyCodeControlRefused:
		return "code_control_refused"
	case ReplyValidateCommentNotApplied:
		return "validate_comment_not_applied"
	case ReplyImplementFinished:
		return "implement_finished"
	case ReplyImplementFinishedAutoValidate:
		return "implement_finished_auto_validate"
	case ReplyImplementFinishedValidated:
		return "implement_finished_validated"
	case ReplyUnsupportedWorkflow:
		return "unsupported_workflow"
	case ReplyMissingModeMapping:
		return "missing_mode_mapping"
	case ReplyConflictingModeSelection:
		return "conflicting_mode_selection"
	case ReplyLegacyToolLabel:
		return "legacy_tool_label"
	case ReplyScheduleInvalid:
		return "schedule_invalid"
	case ReplyScheduleConfirmed:
		return "schedule_confirmed"
	case ReplyScheduleBlocked:
		return "schedule_blocked"
	case ReplyScheduleProposalReady:
		return "schedule_proposal_ready"
	case ReplyScheduleProposalAccepted:
		return "schedule_proposal_accepted"
	case ReplyScheduleProposalRejected:
		return "schedule_proposal_rejected"
	case ReplyScheduleProposalFailed:
		return "schedule_proposal_failed"
	case ReplyConfirmInvalid:
		return "confirm_invalid"
	case ReplyConfirmNotOwner:
		return "confirm_not_owner"
	case ReplyConfirmUnknownProposal:
		return "confirm_unknown_proposal"
	case ReplyConfirmExpiredProposal:
		return "confirm_expired_proposal"
	case ReplyConfirmSupersededProposal:
		return "confirm_superseded_proposal"
	case ReplyConfirmAlreadyApplied:
		return "confirm_already_applied"
	case ReplyConfirmFailed:
		return "confirm_failed"
	case ReplyAppliesResolution:
		return "applies_resolution"
	case ReplyGraphReplyBetweenQuestions:
		return "graph_reply_between_questions"
	case ReplyBugCommentNotApplied:
		return "bug_comment_not_applied"
	case ReplyGraphOwnerWaitRefused:
		return "graph_owner_wait_refused"
	case ReplyImplementCommentNotApplied:
		return "implement_comment_not_applied"
	case ReplyGraphResumeRefused:
		return "graph_resume_refused"
	case ReplyInvalid:
		return "invalid"
	default:
		return "invalid"
	}
}

// valid reports whether k is one of the real kinds.
func (k ReplyKind) valid() bool {
	switch k {
	case ReplyCreationRejected, ReplyCreationProposal, ReplyCreationAccepted:
		return true
	case ReplyUnknownCommand, ReplyEmptySpike:
		return true
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	case ReplyAgentSwitchRefused:
		return true
	case ReplyRefusesSync, ReplySuggestsResolution, ReplyRefusesSyncTooFarBehind:
		return true
	case ReplyMissingTicketPublisher, ReplyDoctorUnsupported, ReplyMissingTeam, ReplyUnsupportedWorkflow, ReplyBugModeSwitch, ReplyBugUnavailable, ReplyMalformedModeLabel, ReplyAmbiguousModeLabels, ReplyMissingModeMapping, ReplyConflictingModeSelection, ReplyStoryLocked, ReplyStoryPlanOnly, ReplyStoryPlanning:
		return true
	case ReplyGraphReplyBetweenQuestions, ReplyBugCommentNotApplied, ReplyGraphOwnerWaitRefused, ReplyImplementCommentNotApplied, ReplyGraphResumeRefused, ReplyCodeControlRefused,
		ReplyValidateCommentNotApplied, ReplyImplementFinished, ReplyImplementFinishedAutoValidate, ReplyImplementFinishedValidated:
		return true
	case ReplyLegacyToolLabel:
		return true
	case ReplyScheduleInvalid, ReplyScheduleConfirmed, ReplyScheduleBlocked,
		ReplyScheduleProposalReady, ReplyScheduleProposalAccepted,
		ReplyScheduleProposalRejected, ReplyScheduleProposalFailed:
		return true
	case ReplyConfirmInvalid, ReplyConfirmNotOwner, ReplyConfirmUnknownProposal,
		ReplyConfirmExpiredProposal, ReplyConfirmSupersededProposal,
		ReplyConfirmAlreadyApplied, ReplyConfirmFailed, ReplyAppliesResolution:
		return true
	case ReplyInvalid:
		return false
	default:
		return false
	}
}

// IsConfirmationReply reports whether k is one of the eight replies a
// /confirm control draws (US-12): the same closed set
// [ReplyRequest.kind]/[ReplyRequest.validate] check membership in, the way
// they already do for ScheduleKind's own eight replies.
func (k ReplyKind) IsConfirmationReply() bool {
	switch k {
	case ReplyConfirmInvalid, ReplyConfirmNotOwner, ReplyConfirmUnknownProposal,
		ReplyConfirmExpiredProposal, ReplyConfirmSupersededProposal,
		ReplyConfirmAlreadyApplied, ReplyConfirmFailed, ReplyAppliesResolution:
		return true
	default:
		return false
	}
}

// RefusesMode reports whether k is a mode-selection refusal: a workflow
// admission refusal delivered through the daemon's refuseMode path, which
// consumes the comment only after the reply is confirmed. That includes
// [ReplyGraphReplyBetweenQuestions], a Research reply-admission refusal.
func (k ReplyKind) RefusesMode() bool {
	switch k {
	case ReplyMissingTicketPublisher, ReplyMissingTeam, ReplyUnsupportedWorkflow, ReplyBugModeSwitch, ReplyBugUnavailable, ReplyMalformedModeLabel, ReplyAmbiguousModeLabels, ReplyMissingModeMapping, ReplyConflictingModeSelection, ReplyStoryLocked, ReplyStoryPlanOnly, ReplyGraphReplyBetweenQuestions, ReplyBugCommentNotApplied, ReplyGraphOwnerWaitRefused, ReplyImplementCommentNotApplied, ReplyGraphResumeRefused, ReplyCodeControlRefused,
		ReplyValidateCommentNotApplied, ReplyImplementFinished, ReplyImplementFinishedAutoValidate, ReplyImplementFinishedValidated:
		return true
	default:
		return false
	}
}

// RefusesTools reports whether k refuses an obsolete permission label rather
// than answering the command in the comment.
//
// It is exported because internal/turn has to tell the two apart — a refusal is
// triggered by a command that *would* have spawned a run, so every guard that
// asks "does this command draw a reply" gives the wrong answer for one — and
// this package owns what its own kinds mean.
func (k ReplyKind) RefusesTools() bool {
	switch k {
	case ReplyLegacyToolLabel:
		return true
	case ReplyInvalid, ReplyUnknownCommand, ReplyEmptySpike:
		return false
	default:
		return false
	}
}

// RefusesAgent reports whether k is one of the three agent-selection
// refusals a resolved lf:agent= label ladder draws: a reply drawn by the
// labels on the issue rather than by the command in the comment.
//
// It mirrors [ReplyKind.RefusesTools] exactly and exists for the same
// reason: internal/turn has to tell "this reply answers a label" from "this
// reply answers a command" before it can decide what [ReplyRequest] shape is
// valid, and this package owns what its own kinds mean.
//
// [ReplyAgentSwitchRefused] is also agent-related, but it is drawn from
// segment ownership and has its own request shape.
func (k ReplyKind) RefusesAgent() bool {
	switch k {
	case ReplyAmbiguousAgentLabels, ReplyMalformedAgentLabel, ReplyUnknownAgent:
		return true
	default:
		return false
	}
}

// RefusesSwitch reports whether k is [ReplyAgentSwitchRefused]: a reply
// drawn when a session's resolved agent does not own the segment its
// conversation already belongs to (US-3). It has its own request shape, read
// from the store rather than from the issue's labels alone, which is exactly
// why [RefusesAgent]'s own doc excludes it.
func (k ReplyKind) RefusesSwitch() bool {
	return k == ReplyAgentSwitchRefused
}

// RefusesSync reports whether k is [ReplyRefusesSync] or
// [ReplySuggestsResolution]: a reply drawn when a task branch could not be
// merged with the base branch cleanly (US-4), the second being the same
// refusal with a suggested resolution attached (US-11). Both share one
// request shape — [Reply.ConflictPaths], and for the second,
// [Reply.ConflictProposal] — the same reason [RefusesSwitch] does, and for
// the same reason [RefusesAgent]'s own doc excludes [ReplyAgentSwitchRefused].
func (k ReplyKind) RefusesSync() bool {
	return k == ReplyRefusesSync || k == ReplySuggestsResolution || k == ReplyRefusesSyncTooFarBehind
}

// Reply is a run-less answer to a comment: the reply forms from LF-2, which
// carry the lf:reply marker and no run ordinal because no run happened.
type Reply struct {
	// MissingMode names the canonical mode lacking a connection label mapping.
	MissingMode agent.Mode

	CreationAcceptance              *CreationAcceptance
	CreationRejection               *CreationRejection
	Creation                        *CreationReply
	SourceGeneration                int
	SourceStoryIssue                int
	SourceCommentID                 int64
	SourceParentURL, SourceReplyURL string
	// Ref is the task the comment was on. It appears in the marker, so it must
	// validate.
	Ref naming.Ref

	// Kind is why litefactory is replying instead of running.
	Kind ReplyKind

	// Command is the command token as parsed, including its leading slash —
	// "/plan", "/frobnicate". It is quoted back so the reply names what it is
	// answering. Empty is allowed and produces slightly vaguer wording.
	Command string

	// Agent is the coding-agent-tool name an lf:agent= label asked for, or
	// the one a resolved segment is already committed to. It is read by
	// [ReplyUnknownAgent] alone, and
	// like Profile it is checked against [agentNamePattern] here before it
	// is rendered — see [agentSpan].
	Agent string

	// OwningAgent and RequestedAgent are read by [ReplyAgentSwitchRefused]
	// alone (US-3): OwningAgent is the coding-agent tool a session's segment
	// already belongs to, and RequestedAgent is the one this run's labels
	// resolved to instead. Both are checked against [agentNamePattern] here
	// before they are rendered, the same as [Agent].
	OwningAgent    string
	RequestedAgent string

	// SyncBehind, SyncBase and SyncMaxBehind are read by
	// [ReplyRefusesSyncTooFarBehind] alone: how far the branch had fallen
	// behind, the ref it was measured against, and the cap it exceeded.
	// Zero and empty for every other kind.
	SyncBehind    int
	SyncBase      string
	SyncDetail    string
	SyncPhase     string
	SyncMaxBehind int

	// ConflictPaths is read by [ReplyRefusesSync] alone (US-4): the paths
	// [git.Worktree.MergePreview] reported as conflicting, carried through
	// exactly as syncBase recorded them. It is rendered inside a fenced block
	// the same way [Run.Sync]'s Conflicts list is (S-2), so a path cannot
	// become markup in the comment no matter what it contains — this package
	// does not sanitise or decide which paths are safe to show at all, which
	// is US-5's job; it shows whatever list it is handed.
	ConflictPaths []string

	// ConflictProposal is read by [ReplySuggestsResolution] alone (US-11):
	// the suggested resolution litefactory persisted for this blocked
	// merge. Nil for every other kind, including [ReplyRefusesSync] when no
	// suggestion was produced.
	ConflictProposal *ConflictProposal

	// ConflictSuggestionFailed is read by [ReplyRefusesSync] alone (US-11,
	// AC-11.2): true when suggestions were enabled for this turn but no
	// usable proposal resulted — the advisor was unavailable, failed, timed
	// out, or its answer was rejected by local revalidation. It renders
	// exactly one added line; the feature-disabled path leaves this false,
	// which is what keeps that path's rendering byte-identical to the
	// no-suggestion behaviour (AC-11.3).
	ConflictSuggestionFailed bool
	ConflictSuggestionReason string

	// Schedule carries the safe, structured facts of a scheduling reply. It is
	// set only for ReplyScheduleInvalid and ReplyScheduleConfirmed.
	Schedule ScheduleReply

	// Confirm carries the safe, structured facts of a /confirm reply
	// (US-12). It is set only for [ReplyKind.IsConfirmationReply] kinds.
	Confirm ConfirmReply

	// ResumeNode and ResumeReason are read by [ReplyGraphResumeRefused]
	// alone: the node the work stopped at and the factory-owned reason the
	// reply could not resume it. Either is omitted when it is not plain text.
	ResumeNode   string
	ResumeReason string
}

// ConfirmReply is the factory-owned disclosure for a /confirm control
// (US-12). ProposalID is set on every confirmation reply; the rest are set
// only where the specific kind needs them — see each field's own comment.
type ConfirmReply struct {
	// Reason is an actionable failure disclosure, sanitized and fenced locally.
	Reason string
	// ProposalID is the proposal the confirmation named. Set whenever it is
	// known, which is every kind except [ReplyConfirmInvalid] (a grammar
	// failure may have no id to name at all).
	ProposalID int64
	// Model names the coding-agent tool whose suggestion was applied.
	// Read by [ReplyAppliesResolution] alone.
	Model string
	// ConfirmedBy is the forge handle that confirmed the proposal. Read by
	// [ReplyAppliesResolution] alone, and checked against
	// [confirmedByPattern] before it is rendered (S-16): a malformed handle
	// is rejected whole, never truncated or sanitised into something
	// printable (reject-don't-salvage).
	ConfirmedBy string
	// Files carries only the confirmed paths and the resolution text that
	// was written for each — the same shape [ConflictProposalFile] already
	// gives [ReplySuggestsResolution]. Read by [ReplyAppliesResolution]
	// alone.
	Files []ConflictProposalFile
	// CommitSHA is the merge commit that completed the application. Read by
	// [ReplyAppliesResolution] alone. Combined with the enclosing [Reply]'s
	// own Ref, it is what builds the commit permalink.
	CommitSHA string
}

// ConflictProposal carries raw statistics and sanitized prepared diffs for a
// region proposal. Legacy values remain displayable but cannot be confirmed.
type ConflictProposal struct {
	TargetPhase, TargetRef, TargetSHA  string
	BaseRef, BaseSHA, TaskRef, TaskSHA string
	Format                             int
	BranchSHA                          string
	ID                                 int64
	Model                              string
	Files                              []ConflictProposalFile
}

// ConflictProposalFile carries one path's raw statistics and prepared diff.
// Resolution is retained only for displaying legacy proposals.
type ConflictProposalFile struct {
	Diff       string
	Metrics    ConflictPreviewMetrics
	Path       string
	Resolution string
}

// ConflictPreviewMetrics are raw factory-derived values, never display-derived.
type ConflictPreviewMetrics struct{ BeforeBytes, AfterBytes, BeforeLines, AfterLines, Additions, Deletions int }

// ScheduleReply is the factory-owned information rendered for a scheduling
// control. It deliberately carries no payload: an instruction is agent input,
// not text Litefactory should echo into a signed comment.
type ScheduleReply struct {
	ID          int64
	DueAt       time.Time
	Delay       time.Duration
	After       []int64
	Outstanding int
	ProposalID  int64
	Count       int
}

// agentNamePattern is the shape of a coding-agent-tool name — deliberately
// restricted to the config-key shape. It is checked again here, at the one
// place a value that reached this package from a
// forge label is echoed into a comment litefactory signs its own name to
// (SEC-5), rather than trusted from whatever validated it upstream.
// resumeNodePattern and resumeReasonPattern keep [ReplyGraphResumeRefused]
// to an identifier-shaped node and a plain lowercase sentence fragment.
var (
	resumeNodePattern   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,63}$`)
	resumeReasonPattern = regexp.MustCompile(`^[a-z][a-z ,']{0,119}$`)
)

var agentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,39}$`)

// agentSpan renders a coding-agent-tool name for quoting back, the same way
// the same way as other untrusted names: no salvaged prefix, nothing rendered
// at all for anything that fails the pattern.
func agentSpan(name string) (string, bool) {
	if !agentNamePattern.MatchString(name) {
		return "", false
	}
	return "`" + name + "`", true
}

// confirmedByPattern is the shape of a GitHub handle: alphanumeric segments
// joined by single hyphens, 1 to 39 characters, matching what the forge
// itself allows a login to be.
//
// It is checked here, at the one place a confirming human's handle is
// echoed into a comment litefactory signs its own name to (S-16), the same
// discipline [agentNamePattern] and [conflictPathPattern] already apply to
// their own untrusted strings. A handle failing it is rejected whole —
// reject-don't-salvage — never truncated or stripped down to whatever
// prefix looks safe.
var confirmedByPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9]|-[a-zA-Z0-9]){0,38}$`)

// confirmedBySpan renders a confirming handle for quoting back, the same
// discipline [agentSpan] and [commandSpan] apply to their own untrusted
// strings: nothing is rendered at all for anything that fails the pattern.
func confirmedBySpan(handle string) (string, bool) {
	if !confirmedByPattern.MatchString(handle) {
		return "", false
	}
	return "`" + handle + "`", true
}

// maxCommandLen bounds how much of a command token is quoted back.
const maxCommandLen = 32

// commandSpan renders the command token for quoting back to its author.
//
// The token reaches this package from a third party's comment. internal/turn
// already restricts it to a slash followed by a plain word, but this is the one
// place where attacker-controlled text is echoed into a comment litefactory
// signs its own name to, so the restriction is re-applied here rather than
// assumed: a reply cannot be made to carry markup, a second lf: marker, or a
// mention that pings somebody.
//
// The token stops at the first character outside that set rather than dropping
// it and carrying on. Skipping characters would let "/ev`il <!-- lf:status -->"
// be reassembled into something that looks like a real command; stopping means
// unexpected input can only ever produce a shorter, plainer token than was
// typed.
func commandSpan(command string) (string, bool) {
	var b strings.Builder
	for i, r := range command {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		interiorPunctuation := i > 0 && (r == '-' || r == '_')
		leadingSlash := i == 0 && r == '/'
		if !alphanumeric && !interiorPunctuation && !leadingSlash {
			break
		}
		if b.Len() >= maxCommandLen {
			break
		}
		b.WriteRune(r)
	}
	token := b.String()
	if token == "" || token == "/" {
		return "", false
	}
	return "`" + token + "`", true
}

// maxConflictPathEntries caps how many accepted conflicting paths
// [conflictsBlock] quotes in a comment. It is a display rule, not a security
// rule (US-5): the full list, after rejection, is trimmed with a remainder
// count, and does not substitute for [conflictPathPattern]'s rejection check,
// which runs first and independently of it.
const maxConflictPathEntries = 20

// conflictPathPattern is the shape a conflicting path must have to be quoted
// back verbatim into a comment litefactory signs its own name to (US-5).
//
// The paths [conflictsBlock] renders come from [git.Worktree.MergePreview],
// reading the base branch's own file tree — which, on most repositories,
// anyone who can land a commit can influence. A file name is just a string,
// and nothing stops one from being chosen to look like an HTML comment shaped
// exactly like [MarkerPrefix] — which [turn.HasMarker] matches on any line of
// a body, not only the first — or to hold a Unicode bidirectional-override
// rune that makes a name display in an order its bytes do not have (the
// "Trojan Source" class of attack). Landing either as its own line inside a
// comment litefactory signs its own name to would hand an attacker a way to
// poison it: a person who later copies that text back — the exact scenario
// [ReplyPlainlyNotice] warns about — could have their own reply silently read
// as litefactory's own and dropped, or simply be shown a name that is not
// what it appears to be.
//
// The pattern is a denylist rather than a narrow allowlist, checked again
// here at the point of echo rather than trusted from [git.Worktree.MergePreview]
// (SEC-5), the same discipline [agentNamePattern] holds for its own untrusted
// string: any character is permitted except the ASCII control range (which
// includes the newline that would otherwise let a single path plant an extra
// line of its own), '<' and '>' (which an HTML comment needs on both ends),
// and the Unicode bidirectional control characters. It is deliberately not
// stricter than that — a run of backticks is still accepted, because
// [fenceFor] already defeats a fence widened to break out of its own block,
// and this pattern exists for the threats that survive fencing rather than to
// duplicate it. A path failing it is dropped and counted rather than shown, so
// over-rejecting an ordinary file name costs nothing but a slightly vaguer
// list, while under-rejecting costs a poisoned comment.
var conflictPathPattern = regexp.MustCompile(
	`^[^\x00-\x1f\x7f<>\x{061c}\x{200e}\x{200f}\x{202a}-\x{202e}\x{2066}-\x{2069}]+$`,
)

// conflictPathSpan renders a single conflicting path for quoting back, the
// same discipline [agentSpan] and [commandSpan] apply to their own untrusted
// strings.
//
// Unlike [commandSpan] it does not stop at the first disallowed rune and keep
// what came before: a path is a single, meaningful string, and a truncated
// prefix of it is a different, real path — one that might not even be among
// the ones that conflicted. So the whole path is checked at once, and a path
// that fails is rejected outright: nothing of it is rendered, not even a
// shortened form (AC-5.1).
func conflictPathSpan(path string) (string, bool) {
	if !conflictPathPattern.MatchString(path) {
		return "", false
	}
	return path, true
}

// classifyConflictPaths splits paths into the ones safe to quote back and a
// count of the ones that are not.
//
// Every path is classified independently — one path failing
// [conflictPathPattern] says nothing about any other — so this walks the
// whole list rather than stopping at the first rejection, matching this
// package's other list-of-independent-values checks.
func classifyConflictPaths(paths []string) (accepted []string, rejected int) {
	accepted = make([]string, 0, len(paths))
	for _, p := range paths {
		if span, ok := conflictPathSpan(p); ok {
			accepted = append(accepted, span)
			continue
		}
		rejected++
	}
	return accepted, rejected
}

// conflictProposalBlock renders a proposal's per-file suggested resolutions,
// reusing exactly the denylist and fencing discipline [conflictsBlock]
// already applies to conflicting paths (US-5), rather than a second copy of
// it: [conflictPathSpan] screens the path, [sanitize] and [fenceFor] make
// the resolution text safe to quote verbatim (S-2).
//
// A file whose path fails the denylist renders nothing for that file and is
// silently dropped, the same per-item behaviour [classifyConflictPaths]
// already gives every other conflicting-path list in this package — it is
// not the reject-the-whole-response discipline internal/turn's local
// revalidation applies before a proposal is ever persisted (T-11.8); by the
// time a proposal reaches this renderer it has already passed that gate; a
// dropped file here is defence in depth, not the expected case.
func conflictProposalBlock(files []ConflictProposalFile) string {
	var b strings.Builder
	first := true
	for _, f := range files {
		span, ok := conflictPathSpan(f.Path)
		if !ok {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		fmt.Fprintf(&b, "For `%s`:\n", span)
		content := sanitize(f.Resolution)
		fence := fenceFor(content)
		b.WriteString(fence)
		b.WriteString("\n")
		b.WriteString(content)
		b.WriteString("\n")
		b.WriteString(fence)
		b.WriteString("\n")
	}
	return b.String()
}

// body renders the reply. It is small on purpose: a reply is an answer to
// something litefactory will not do, and the useful part is the sentence that
// says what it will do instead.
func (r Reply) body() string {
	var b strings.Builder
	b.WriteString(ReplyMarker(r.Ref))
	if r.Kind == ReplyStoryPlanning {
		b.WriteString(StoryAcknowledgementMarker(r.Ref, r.SourceGeneration, r.SourceStoryIssue, r.SourceCommentID))
	}
	b.WriteString(storySource(r.Ref, r.SourceStoryIssue, r.SourceCommentID, r.SourceParentURL, r.SourceReplyURL))
	b.WriteString(sectionGap)

	span, named := commandSpan(r.Command)
	switch r.Kind {
	case ReplyCreationAccepted:
		if r.CreationAcceptance != nil {
			b.WriteString(r.CreationAcceptance.body())
		}
	case ReplyCreationRejected:
		if r.CreationRejection != nil {
			text, _ := r.CreationRejection.guidance()
			b.WriteString(text)
		}
	case ReplyCreationProposal:
		if r.Creation != nil {
			b.WriteString("Retained creation proposal. Review the complete JSON below; preparation grants no publication.\n\n")
			fence := fenceFor(r.Creation.Envelope)
			b.WriteString(fence + "json\n" + r.Creation.Envelope + "\n" + fence + "\n\n")
			fmt.Fprintf(&b, "To approve these exact bytes, comment:\n\n`/mcp-create %s %s`", r.Creation.Hash, r.Creation.Relationship)
			b.WriteString("\n\n" + CreationGuidance(r.Creation.Publication))
		}
	case ReplyEmptySpike:
		if named && span != "`/spike`" {
			b.WriteString(span + " needs something to work on. Say what you want done, on the same line or the ones after it:")
			b.WriteString(sectionGap)
			b.WriteString("```text\n" + strings.Trim(span, "`") + " <what you want done>\n```")
			break
		}
		b.WriteString("`/spike` needs something to work on. Say what you want tried, on the same line or the ones after it:")
		b.WriteString(sectionGap)
		b.WriteString("```text\n/spike see whether the flaky checkout test is a race in the fixture\n```")

	case ReplyUnknownCommand:
		if named {
			fmt.Fprintf(&b, "I do not recognise %s.", span)
		} else {
			b.WriteString("I do not recognise that command.")
		}
		b.WriteString(" ")
		b.WriteString(onlyCommandSentence)

	case ReplyLegacyToolLabel:
		b.WriteString("This issue carries the old `lf:tools=` label, which litefactory no longer reads. ")
		b.WriteString(toolsRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Remove every `lf:tools=` label and comment again. Litefactory now selects " +
			"`research`, `plan`, `implement`, or `validate` permissions automatically from the " +
			"resolved run mode, so no permission label is needed. The label text is not repeated " +
			"here on purpose.")

	case ReplyAmbiguousAgentLabels:
		b.WriteString("This issue carries more than one `Coding Agent: …` label, so there is no single " +
			"answer to which coding-agent tool this run should have used. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Neither label was chosen and they were not combined. Leave exactly one " +
			"`Coding Agent: Claude` or `Coding Agent: Codex` label on the issue and comment again.")

	case ReplyMalformedAgentLabel:
		b.WriteString("The `Coding Agent: …` label on this issue does not name a usable coding-agent tool. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Use exactly `Coding Agent: Claude` or `Coding Agent: Codex`. The label " +
			"text is not repeated here on purpose.")

	case ReplyBugModeSwitch:
		b.WriteString("This task is already admitted to the standalone Bug workflow. A mode command cannot replace or cancel it. Its workflow and retained progress are unchanged.")
	case ReplyDoctorUnsupported:
		b.WriteString("Doctor is currently supported only on GitHub. No Doctor job or repair was started. Other commands on this ticket can continue.")

	case ReplyBugUnavailable:
		b.WriteString("Bug admission is retained, but execution is unavailable: the registered Bug policy, graph package, result decoder, shared-service preparation and factory role permission bindings are not installed. No agent was launched. The task remains resumable in Bug.")
	case ReplyMissingTicketPublisher:
		fmt.Fprintf(&b, "The retained %s ticket connection for %s/%s has no usable verified publisher. Implement is blocked.", r.Ref.Forge, r.Ref.Owner, r.Ref.Repo)
		b.WriteString(sectionGap)
		b.WriteString("Restore and verify the ticket connection's original publishing account, then send a fresh `/implement <instruction>` command. The configured project approver remains unchanged.")
	case ReplyMissingTeam:
		b.WriteString("This repository has no linked installed team for the requested workflow. Nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Link a suitable installed team to the repository, then comment again.")
	case ReplyUnsupportedWorkflow:
		b.WriteString("The linked team does not provide the requested workflow. Nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Select a workflow supported by the linked team, or link a suitable installed team, then comment again.")
	case ReplyMissingModeMapping:
		b.WriteString("The selected mode has no label mapping on this Linear connection. Nothing was run.")
		b.WriteString(sectionGap)
		if r.MissingMode.Valid() {
			fmt.Fprintf(&b, "Add a label ID for `%s` to the connection's `--mode-labels`, or label the issue with a mapped mode, then comment again.", r.MissingMode)
		} else {
			b.WriteString("Add the selected mode's label ID to the connection's `--mode-labels`, or label the issue with a mapped mode, then comment again.")
		}
	case ReplyMalformedModeLabel:
		b.WriteString("The `Mode: …` label on this issue is malformed or unknown. Nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Use exactly `Mode: Research`, `Mode: Plan`, `Mode: Implement`, `Mode: Validate`, or `Mode: Spike`.")

	case ReplyAmbiguousModeLabels:
		b.WriteString("This issue carries more than one `Mode: …` label, so no mode was chosen and nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Leave exactly one canonical Mode label and comment again.")

	case ReplyStoryPlanning:
		b.WriteString("This comment has been admitted to the parent plan. The parent task will refine this story and report the parent run here.")
	case ReplyStoryLocked:
		b.WriteString("This user story is locked after promotion to Implement. The approved scope is retained. Comment `replan` on the parent after the active run finishes to reopen Plan for unfinished stories. Completed stories remain completed; new scope requires a successor.")
	case ReplyStoryPlanOnly:
		b.WriteString("Story comments refine the parent plan only. Use the parent issue for workflow commands; nothing was run.")
	case ReplyGraphReplyBetweenQuestions:
		b.WriteString(graphReplyBetweenQuestionsSentence)
		b.WriteString(sectionGap)
		b.WriteString("If it still applies, please post it again as a reply to the open question or review.")
	case ReplyBugCommentNotApplied:
		b.WriteString("This comment could not be applied to the bug work at its current step, so it was set aside and nothing was run for it.")
		b.WriteString(sectionGap)
		b.WriteString("Reply to the latest question or review instead. To approve a review, put `/approve-bug analysis <revision>` on the first line, using the revision that review names.")
	case ReplyImplementCommentNotApplied:
		b.WriteString("This Implement comment did not select any parked stories, so no stories were resumed.")
		b.WriteString(sectionGap)
		b.WriteString("Name a parked story key to resume it with your guidance, or name no existing story key to resume all parked stories. Running and merged stories are unchanged.")
	case ReplyGraphResumeRefused:
		b.WriteString("This comment could not resume the stopped work")
		if resumeNodePattern.MatchString(r.ResumeNode) {
			fmt.Fprintf(&b, " at `%s`", r.ResumeNode)
		}
		if resumeReasonPattern.MatchString(r.ResumeReason) {
			b.WriteString(" because ")
			b.WriteString(r.ResumeReason)
		}
		b.WriteString(", so it was set aside and nothing was run for it.")
		b.WriteString(sectionGap)
		b.WriteString("If it still applies, please reply again once that has changed.")
	case ReplyGraphOwnerWaitRefused:
		b.WriteString("This comment could not be used because the workflow is already handling another reply or question, so it was set aside and nothing was run for it.")
		b.WriteString(sectionGap)
		b.WriteString("A question that is already answered cannot accept another answer. ")
		b.WriteString("If it still applies, please post it again after the workflow has posted its next update.")
	case ReplyCodeControlRefused:
		b.WriteString("This delivery command was set aside and nothing was recorded, pushed, merged or closed, because only the task's owner can give it.")
	case ReplyValidateCommentNotApplied:
		b.WriteString("Validate is running or waiting for you; nothing was run. The Validate report lists what blocks and the commands. Give each command as its own comment on this issue.")
	case ReplyImplementFinished:
		b.WriteString("Implement is finished: every story is merged. Nothing was run. Next step: start Validate by commenting `/validate`.")
	case ReplyImplementFinishedAutoValidate:
		b.WriteString("Implement is finished: every story is merged. Nothing was run. Validate starts automatically; its report will appear here.")
	case ReplyImplementFinishedValidated:
		b.WriteString("Implement is finished and Validate has already finished on it. Nothing was run. The Validate report shows the result.")
	case ReplyConflictingModeSelection:
		b.WriteString("The issue's mode changed while its recorded mode transition was pending, so nothing was run.")
		b.WriteString(sectionGap)
		b.WriteString("Leave exactly one canonical Mode label for the stage you want, and comment again. A new explicit mode command can replace that selection.")

	case ReplyUnknownAgent:
		if name, named := agentSpan(r.Agent); named {
			fmt.Fprintf(&b, "This issue asks for the coding-agent tool %s, which litefactory has no "+
				"implementation registered for.", name)
		} else {
			b.WriteString("This issue asks for a coding-agent tool litefactory has no implementation registered for.")
		}
		b.WriteString(" ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Take the `Coding Agent: …` label off the issue, or change it to a tool litefactory " +
			"has running, and comment again.")

	case ReplyAgentSwitchRefused:
		b.WriteString("This automatic recovery attempt was continuing a run under a different " +
			"coding-agent tool than the issue's `Coding Agent:` label names now. ")
		b.WriteString(agentRefusalSentence)
		b.WriteString(sectionGap)
		b.WriteString("Automatic recovery has to continue the exact run it recorded, not one under a " +
			"newly switched tool, so it stops here rather than guessing. An ordinary comment does not " +
			"have this problem — litefactory switches the running coding-agent tool to match the " +
			"current label automatically and starts a fresh conversation under it. Comment again to " +
			"run under the current label.")

	case ReplyRefusesSyncTooFarBehind:
		fmt.Fprintf(&b, "This task's branch is %d commit(s) behind %s, more than the configured cap of %d, "+
			"so litefactory stopped before starting the agent. ",
			r.SyncBehind, codeSpan(r.SyncBase), r.SyncMaxBehind)
		b.WriteString(syncTooFarBehindSentence)
		if r.SyncDetail != "" {
			b.WriteString(sectionGap + codeSpan(r.SyncDetail))
		}

	case ReplyRefusesSync:
		if r.SyncPhase == "task" {
			b.WriteString("This task's branch could not be reconciled with its remote task branch, so litefactory stopped before starting the agent. ")
		} else {
			b.WriteString("This task's branch could not be merged with the base branch cleanly, so litefactory " +
				"stopped before starting the agent. ")
		}
		if r.SyncDetail != "" {
			b.WriteString(codeSpan(r.SyncDetail) + " ")
		}
		b.WriteString(syncRefusalSentence)
		if len(r.ConflictPaths) > 0 {
			b.WriteString(sectionGap)
			fmt.Fprintf(&b, "Conflicting path(s) (%d):\n", len(r.ConflictPaths))
			b.WriteString(conflictsBlock(r.ConflictPaths))
		}
		if r.ConflictSuggestionFailed {
			b.WriteString(sectionGap)
			b.WriteString(conflictSuggestionFailedSentence)
			if r.ConflictSuggestionReason != "" {
				reason := sanitize(r.ConflictSuggestionReason)
				fence := fenceFor(reason)
				b.WriteString("\n" + fence + "\n" + reason + "\n" + fence)
			}
		}
		b.WriteString(sectionGap)
		b.WriteString("Resolve the conflict on the base branch or on this task's branch, then comment again.")

	case ReplySuggestsResolution:
		if r.SyncPhase == "task" {
			b.WriteString("This task's branch could not be reconciled with its remote task branch, so litefactory stopped before starting the agent. ")
		} else {
			b.WriteString("This task's branch could not be merged with the base branch cleanly, so litefactory " +
				"stopped before starting the agent. ")
		}
		if r.SyncDetail != "" {
			b.WriteString(codeSpan(r.SyncDetail) + " ")
		}
		b.WriteString(syncRefusalSentence)
		if len(r.ConflictPaths) > 0 {
			b.WriteString(sectionGap)
			fmt.Fprintf(&b, "Conflicting path(s) (%d):\n", len(r.ConflictPaths))
			b.WriteString(conflictsBlock(r.ConflictPaths))
		}
		if r.ConflictProposal != nil {
			b.WriteString(sectionGap)
			fmt.Fprintf(&b, "Suggested resolution from `%s` (proposal `%d`):\n",
				oneLine(r.ConflictProposal.Model), r.ConflictProposal.ID)
			b.WriteString(conflictProposalBlock(r.ConflictProposal.Files))
		}
		b.WriteString(sectionGap)
		b.WriteString(conflictProposalConfirmationSentence)
		if r.ConflictProposal != nil {
			b.WriteString(" This legacy proposal cannot be confirmed. Request regeneration and confirm the new proposal number.")
		}
		b.WriteString(sectionGap)
		b.WriteString("Resolve the conflict on the base branch or on this task's branch, then comment again.")

	case ReplyScheduleInvalid:
		b.WriteString("I could not schedule that request. Litefactory accepts ")
		b.WriteString("`/schedule -by HH:MM:SS <payload>`, `/schedule -to MM/DD/YYYY HH:MM:SS [ZONE] <payload>`, ")
		b.WriteString("or `/schedule --after <id[,id...]> -by HH:MM:SS <payload>`.")

	case ReplyScheduleConfirmed:
		if r.Schedule.ID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Scheduled work `%d`", r.Schedule.ID)
		if len(r.Schedule.After) > 0 {
			b.WriteString(" after schedules ")
			for i, id := range r.Schedule.After {
				if i > 0 {
					b.WriteString(", ")
				}
				fmt.Fprintf(&b, "`%d`", id)
			}
			fmt.Fprintf(&b, " succeed, then waits %s", r.Schedule.Delay)
		} else {
			fmt.Fprintf(&b, " for %s", r.Schedule.DueAt.UTC().Format(time.RFC3339))
		}
		fmt.Fprintf(&b, ". `scheduled=%d`.", r.Schedule.Outstanding)

	case ReplyScheduleBlocked:
		if r.Schedule.ID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Scheduled work `%d` will not run because one of its prerequisites failed.", r.Schedule.ID)

	case ReplyScheduleProposalReady:
		if r.Schedule.ProposalID <= 0 || r.Schedule.Count <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Schedule proposal `%d` contains %d item(s). Use `/schedule --accept %d` to arm it, or `/schedule --reject %d` to discard it.", r.Schedule.ProposalID, r.Schedule.Count, r.Schedule.ProposalID, r.Schedule.ProposalID)

	case ReplyScheduleProposalAccepted:
		if r.Schedule.ProposalID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Accepted schedule proposal `%d`; %d item(s) are now scheduled. `scheduled=%d`.", r.Schedule.ProposalID, r.Schedule.Count, r.Schedule.Outstanding)

	case ReplyScheduleProposalRejected:
		if r.Schedule.ProposalID <= 0 {
			return ""
		}
		fmt.Fprintf(&b, "Rejected schedule proposal `%d`.", r.Schedule.ProposalID)

	case ReplyScheduleProposalFailed:
		b.WriteString("I could not produce a valid schedule proposal. No work was scheduled.")

	case ReplyConfirmInvalid:
		b.WriteString("I could not accept that confirmation. Litefactory accepts exactly " +
			"`/confirm <id> " + confirmationPhraseText + "`, naming the proposal id and the confirmation " +
			"phrase verbatim. Nothing was applied.")

	case ReplyConfirmNotOwner:
		b.WriteString("Only this task's issue owner may confirm a proposal. Nothing was applied and the " +
			"proposal's status is unchanged.")

	case ReplyConfirmUnknownProposal:
		if r.Confirm.ProposalID > 0 {
			fmt.Fprintf(&b, "I have no pending proposal `%d` for this task. ", r.Confirm.ProposalID)
		} else {
			b.WriteString("I have no pending proposal with that id for this task. ")
		}
		b.WriteString("Nothing was applied.")

	case ReplyConfirmExpiredProposal:
		fmt.Fprintf(&b, "Proposal `%d` has expired and can no longer be confirmed. Nothing was applied.", r.Confirm.ProposalID)
		b.WriteString(sectionGap)
		b.WriteString("Comment again to ask for a fresh suggestion.")

	case ReplyConfirmSupersededProposal:
		fmt.Fprintf(&b, "Proposal `%d` no longer matches this task's current branch or base/task inputs, or lacks verified input binding, and cannot be applied. "+
			"Nothing was applied.", r.Confirm.ProposalID)
		b.WriteString(sectionGap)
		b.WriteString("Comment again to ask for a fresh suggestion.")

	case ReplyConfirmAlreadyApplied:
		fmt.Fprintf(&b, "Proposal `%d` was already applied. Nothing more was done.", r.Confirm.ProposalID)

	case ReplyConfirmFailed:
		fmt.Fprintf(&b, "I could not complete application of proposal `%d`.", r.Confirm.ProposalID)
		if reason := sanitize(r.Confirm.Reason); reason != "" {
			fence := fenceFor(reason)
			b.WriteString("\n" + fence + "\n" + reason + "\n" + fence)
		}
		b.WriteString(sectionGap)
		b.WriteString("Resolve the reported issue manually or request regeneration and confirm the new proposal number. A failed cleanup or push can leave local changes; the reported failure describes the outcome.")

	case ReplyAppliesResolution:
		fmt.Fprintf(&b, "Applied proposal `%d`.", r.Confirm.ProposalID)
		if model, ok := agentSpan(r.Confirm.Model); ok {
			fmt.Fprintf(&b, " Resolution from %s,", model)
		}
		if handle, ok := confirmedBySpan(r.Confirm.ConfirmedBy); ok {
			fmt.Fprintf(&b, " confirmed by %s.", handle)
		} else {
			b.WriteString(" confirmed.")
		}
		if len(r.Confirm.Files) > 0 {
			b.WriteString(sectionGap)
			b.WriteString("Files applied:")
			b.WriteString(sectionGap)
			b.WriteString(conflictProposalBlock(r.Confirm.Files))
		}
		if url, ok := commitURL(r.Ref, r.Confirm.CommitSHA); ok {
			fmt.Fprintf(&b, "%sCommit: [`%s`](%s)", sectionGap, r.Confirm.CommitSHA, url)
		} else if validSHA(r.Confirm.CommitSHA) {
			fmt.Fprintf(&b, "%sCommit: `%s`", sectionGap, r.Confirm.CommitSHA)
		}

	case ReplyInvalid:
		// Unreachable: RenderReply rejects an invalid kind before calling this.
		return ""

	default:
		return ""
	}

	// Every reply above ends by asking for another comment, so every reply is a
	// comment somebody is about to answer — which makes this the last line of
	// all of them rather than a decoration on some (AC-S0.6).
	b.WriteString(sectionGap)
	b.WriteString(ReplyPlainlyNotice)
	b.WriteString("\n")
	return b.String()
}

// onlyCommandSentence is the one thing every reply has to say: what litefactory
// will act on. It is a single constant so the replies cannot drift into
// different accounts of the active command vocabulary.
const onlyCommandSentence = "Litefactory accepts `/research`, `/plan`, `/implement`, `/validate`, `/spike`, and `/schedule`."

// graphReplyBetweenQuestionsSentence is [ReplyGraphReplyBetweenQuestions]'s
// explanation. It is a constant so tests can find the one reply by its words.
const graphReplyBetweenQuestionsSentence = "This comment arrived while the workflow was still working, " +
	"before its current question or review was posted, so it was not used and nothing was run for it."

// confirmationPhraseText mirrors internal/confirm.Phrase verbatim. It is a
// literal here rather than an import of that package, the same way
// [ScheduleCommand]'s literal "/schedule" already is: this package renders
// and does no parsing at all, and importing a parser for one string would
// put a grammar dependency in the one package whose job is prose.
const confirmationPhraseText = "apply this resolution"

// toolsRefusalSentence is the one thing every tool-permission refusal has to
// say, for the same reason [onlyCommandSentence] exists: three refusals must not
// drift into three different accounts of the same rule.
//
// The rule is C9, in the words of the person reading the thread. A label is a
// question about permission, and the only safe answer to one litefactory cannot
// read is no — running anyway, on whatever tools the run would have had without
// the label, would answer a narrowing request by ignoring it.
const toolsRefusalSentence = "Nothing was run: litefactory refuses a run it cannot " +
	"resolve the tools for rather than falling back to its default ones."

// agentRefusalSentence is the one thing every agent-selection refusal has to
// say, for the same reason [toolsRefusalSentence] exists: litefactory refuses
// a run it cannot resolve which coding-agent tool to use for, rather than
// silently falling back to a different one than was asked for.
const agentRefusalSentence = "Nothing was run: litefactory refuses a run it cannot " +
	"resolve the coding-agent tool for rather than falling back to a different one."

// syncRefusalSentence is the one thing every base-branch-sync refusal has to
// say, for the same reason [agentRefusalSentence] exists: litefactory refuses
// to run an agent against a task branch it could not merge the base branch
// into cleanly, rather than guessing at a resolution or running against a
// stale branch.
// syncTooFarBehindSentence is [ReplyRefusesSyncTooFarBehind]'s standing
// explanation, the sibling of [syncRefusalSentence]. It names the two ways
// out, because unlike a conflict this one has no file to fix: the operator
// either merges the base themselves or tells litefactory the distance is
// acceptable.
const syncTooFarBehindSentence = "Nothing was run: a branch this far behind is merged deliberately, not " +
	"automatically before a run. Merge the base branch into this task's branch yourself, or raise " +
	"`auto_sync_max_behind` (0 removes the cap entirely), then comment again."

const syncRefusalSentence = "Nothing was run: litefactory refuses a run it cannot merge the base branch " +
	"into cleanly rather than guessing at a resolution or running against a stale branch."

// conflictSuggestionFailedSentence is [ReplyRefusesSync]'s one added line
// when suggestions were enabled but no usable proposal resulted (US-11,
// AC-11.2). It is a single sentence on purpose: this reply must add exactly
// one line over the plain refusal, never more.
const conflictSuggestionFailedSentence = "No suggested resolution could be produced for this conflict."

// conflictProposalConfirmationSentence is the confirmation phrase
// [ReplySuggestsResolution] carries on every render (AC-11.1): a suggestion
// is advisory only, and this is the one sentence that says so, the same way
// [syncRefusalSentence] is the one account every sync refusal gives of why
// nothing ran. [ReplySuggestsResolution]'s own rendering appends the
// /confirm hint naming this proposal's id right after it (US-12); this
// sentence stays unconditional so it reads the same whether or not a
// proposal id follows.
const conflictProposalConfirmationSentence = "This is a suggestion only: litefactory has not applied it, " +
	"and this task's branch, index, and commits are unchanged."

// conflictPathRejectionSentence is the one thing every hidden-conflict-path
// count has to say, for the same reason [syncRefusalSentence] exists: a path
// failing [conflictPathPattern] is dropped and counted rather than shown —
// never sanitised into something printable, never shown as a truncated
// prefix — and a fourth account of that rule (US-5) must not drift from the
// other three. It takes one %d verb for the count of paths it hid.
const conflictPathRejectionSentence = "%d of the conflicting path(s) could not be shown safely and " +
	"were counted instead of printed."

// CreationReply contains the complete retained JSON and its exact fingerprint.
type CreationReply struct {
	Envelope, Hash, Relationship string
	Publication                  CreationPublication
}
