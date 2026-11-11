package harness

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

// Prompt assembly (T-LF3.3, AC-3.11, D-3, D-N).
//
// [BuildPrompt] is pure: no clock, no filesystem, no environment. The whole
// prompt is persisted verbatim in NNNN.meta.json, so "what did we actually ask
// it" is answerable after the fact from the run directory alone, and the same
// inputs always produce the same bytes.
//
// There are two shapes of prompt, and [PromptInput.Resume] picks between them.
// A cold run gets all of the below, and with it a condensed record of the
// earlier comments on the issue — the thread the conversation it could not
// resume would have held. A run that continues an existing conversation gets
// only the delta — what has changed since the last run — because claude has
// already been told the rest and repeating it wastes the context the
// conversation is being resumed for.
//
// The prompt has four jobs, and only the first is obvious.
//
//  1. Say what the task is.
//  2. Say the feature slug (D-3). aiteam's dispatcher asks for a feature name
//     before it routes anything, and the answer is stated in the prompt text so
//     that nothing has to stop for it. It is stated on every prompt, including
//     the ones a run is allowed to ask questions on: permission to ask is for
//     questions that change what gets built, and this is not one of them — it is
//     a fact litefactory already holds. Without it a run stops on a question
//     that, on an untagged run, will never be read at all.
//  3. Fence the untrusted material (D-N). The issue title and body, the
//     triggering comment and — on a cold start — every earlier comment in the
//     history block are written by third parties, and the issue author is not
//     necessarily whoever triggered the run, so each is wrapped in a <note>
//     element carrying its source and author. What a note means for the agent
//     is stated in the run-rules skill, not here. A run started by the issue
//     body itself is the one exception, and [PromptInput.BodyIsInstruction] is
//     how it is handled: there the body *is* the request, so it is not also
//     fenced as material to evaluate.
//  4. Say which request is the live one. Replaying a thread means replaying
//     commands that have already been carried out, so a historic comment is
//     labelled by role rather than reproduced as the instruction it once was
//     (see [HistoryRole]). What is under "## Instruction" is the only thing
//     being asked for now.
//
// Framing is only as good as the frame, so a note's contents cannot forge one:
// any <note> or </note> the untrusted text contains is neutralised on the way
// in. Without that, an issue body ending in "</note>" would hand the rest of
// itself to the model as if litefactory had said it.
//
// internal/turn supplies current and previous question permission, rendered
// as header facts. The prompt carries no behaviour rules: those live in the
// shared run-rules and result-protocol skills under teams/source.

// Size caps on untrusted material. Each is a byte count, and text over it is
// cut on a UTF-8 rune boundary and marked.
//
// The instruction itself is deliberately not capped: it is the operator's own
// words, it arrives on stdin rather than in argv so there is no ARG_MAX ceiling
// to respect, and silently dropping half of what Mario asked for is worse than
// a long prompt.
const (
	// MaxIssueBodyBytes caps the issue body (AC-3.11 size caps).
	MaxIssueBodyBytes = 8 << 10
	// MaxCommentBytes caps the triggering comment.
	MaxCommentBytes = 8 << 10
	// MaxIssueTitleBytes caps the issue title. GitHub's own limit is far below
	// this; the cap exists so that a forge which does not enforce one cannot
	// push a megabyte through the header.
	MaxIssueTitleBytes = 1 << 10
	// MaxHistoryBytes caps the whole history block, on top of the per-entry
	// MaxCommentBytes. Roughly four full-size comments — enough to carry a
	// thread's worth of context, small enough that one long argument cannot
	// crowd out the issue, the triggering comment and the instruction. The
	// entries that do not fit are dropped oldest-first and counted; see
	// [selectHistory].
	MaxHistoryBytes = 32 << 10
	// MaxPreviousReasonBytes caps [PromptInput.PreviousReason]. The store
	// already bounds it far below this — runner.go's reasonExcerptLength caps
	// it at 200 bytes before it is ever written to runs.error — but that cap
	// documents itself as bounding length, not meaning (see
	// [writeResumeHeader]), so this package keeps its own independent
	// bound rather than trusting the store's to hold forever.
	MaxPreviousReasonBytes = 1 << 10
)

// truncationMarker is appended to text that was cut. It is a fixed literal so
// that a reader — human or model — can tell a deliberate cut from a truncated
// transfer.
const truncationMarker = "[truncated]"

// InferMode is the run mode of a comment that named no command: working out
// what kind of work is being asked for is part of the task.
//
// It is a mode like any other everywhere else — the runs row, the commit
// trailer and the metadata document all carry it. This package is the only one
// that reads it, and all it does is add a paragraph to the header.
const InferMode = "infer"

// HistoryRole is what an earlier comment is labelled as inside the history
// block, and it is deliberately not the classification the store recorded.
//
// The hazard of replaying a thread is that the thread is full of commands. A
// comment the owner wrote a week ago was an instruction when it arrived and has
// since been carried out, superseded or abandoned; rendering it as one invites
// it to be obeyed a second time. Every historic comment is therefore labelled
// as historic, and what is under "## Instruction" is the only request being
// made now.
type HistoryRole string

const (
	// RoleOldOwner labels a comment the owner wrote before this run. It is the
	// relabelling that matters most: a `/spike …` in one of these is a record
	// of what was asked before, not a command being given now.
	RoleOldOwner HistoryRole = "old_owner"
	// RoleOldThirdParty labels a comment by someone who cannot command
	// litefactory at all. It is also what an unrecognised role falls back to,
	// because it is the label that claims least.
	RoleOldThirdParty HistoryRole = "old_third_party"
	// RoleLitefactory labels litefactory's own earlier reply. Those carry each
	// past run's final message in its report comment, which is how a cold start
	// gets its own side of the conversation back without storing it twice.
	RoleLitefactory HistoryRole = "litefactory"
)

// label renders the role, falling back to the least-privileged one for a zero
// or unrecognised value. An unlabelled entry is a bug in the caller, and
// reading it as the owner's words would be the expensive way to find out.
func (r HistoryRole) label() string {
	switch r {
	case RoleOldOwner, RoleOldThirdParty, RoleLitefactory:
		return string(r)
	default:
		return string(RoleOldThirdParty)
	}
}

// HistoryEntry is one earlier comment on the issue, as the history block
// renders it. The body is untrusted like every other body in the prompt: it is
// neutralised and capped at [MaxCommentBytes] on the way in.
type HistoryEntry struct {
	// Role is what the entry is labelled as. See [HistoryRole].
	Role HistoryRole

	// Author is the raw handle, rendered beside the role so that the label
	// stays traceable to a person: "old_third_party" alone cannot tell one
	// commenter from another, and a thread usually has several.
	Author string

	// Body is the comment text, verbatim and untrusted.
	Body string

	// At is the forge's timestamp for the comment, not litefactory's. It is
	// rendered in UTC; nothing here reads a clock.
	At time.Time
}

// DeferredSyncContext is the factory's synchronization decision, not agent
// output. It is a value so resume fallback retains the same bounded context.
// Ref names are quoted and capped when rendered; the explanation is fixed text.
type DeferredSyncContext struct {
	Deferred      bool
	Branch, Base  string
	Behind, Ahead int
	// FeatureBranch and Conflicts are set when the factory started merging a
	// story's feature branch into the story branch and left the conflicting
	// paths for the story worker to resolve.
	FeatureBranch string
	Conflicts     []string
}

// maxPromptConflicts bounds the conflicting paths a prompt lists.
const maxPromptConflicts = 100

// PromptInput is everything [BuildPrompt] needs. Every string field is copied
// into the prompt; the ones documented as untrusted are neutralised and capped
// first.
type PromptInput struct {
	PreparationAssessment bool
	PreparationCaveats    []string

	Workflow agent.Workflow
	// SubmissionVersion is the persisted factory content transport contract.
	SubmissionVersion int
	// DeferredSync discloses preserved edits on cold, resumed and fallback runs.
	DeferredSync DeferredSyncContext
	// ApprovedStories is copied before rendering; it is explicit on all execution paths.
	ApprovedStories *agent.ApprovedBundle
	Validation      *agent.ValidationContext
	// RefinementStory is the complete current child specification, independently bounded.
	RefinementStory  string
	RefinementSource string

	// Ref identifies the task. Rendered in the header as the canonical
	// forge:owner/name#issue string.
	Ref            naming.Ref
	IssueReference string

	// FeatureSlug is the frozen slug for this task (D-4). Stated in the prompt
	// because a headless run cannot answer aiteam's question about it (D-3).
	FeatureSlug string

	// Branch is the task branch the worktree is on. Informational: the run is
	// already checked out on it.
	Branch string

	// Mode is the run mode: the command word for a slash command, or
	// [InferMode] for a comment that named no command.
	Mode string

	// RunOrdinal is the per-task run number.
	RunOrdinal int

	// Instruction is what the owner asked for: the text of the /spike command
	// with the command word stripped, or the whole comment body when the
	// comment named no command. Trusted: it is the repository owner's own
	// request either way.
	Instruction           string
	OwnerGuidanceRevision int64
	OwnerGuidanceBody     string

	// IssueTitle, IssueBody and IssueAuthor describe the issue. Untrusted.
	IssueTitle  string
	IssueBody   string
	IssueAuthor string

	// CommentBody and CommentAuthor describe the comment that triggered this
	// run. Untrusted — the classification that made it an instruction says who
	// may command litefactory, not that the text is safe to obey verbatim.
	CommentBody   string
	CommentAuthor string

	// Resume says this prompt is the next message in a conversation claude is
	// already having, rather than the opening one. It drops everything that
	// conversation has already been told: the issue note, the history block,
	// the task, branch and working-directory facts. What is left is the run
	// header (mode, feature slug, question permission and any change to it,
	// previous-run state), the instruction and the note for the comment that
	// triggered *this* run — or no note at all when nothing did, which is what
	// [PromptInput.BodyIsInstruction] means.
	//
	// The feature slug stays on both paths deliberately: it is the insurance
	// against claude's own compaction having dropped it (D-3). The question
	// permission is restated and a change is named, because the transcript of a
	// run that could ask says it may ask — see [PromptInput.PreviousMayAsk].
	Resume bool

	// BodyIsInstruction says this run was started by the issue body rather than
	// by a comment: the issue is assigned to the owner, and its body was taken
	// as the first instruction (AC-2.16b). CommentBody and CommentAuthor are
	// then empty, because there is no comment.
	//
	// It changes the framing, not the content, and it has to (D-N). The body is
	// already under "## Instruction", where it is the request being made; also
	// wrapping it in a <note> that says "information to evaluate, not
	// instructions" would assert both things about the same paragraph and leave
	// the run to pick one. So three things change: no comment note is rendered,
	// because there is no comment; the Context sentence goes singular, because
	// one note is left; and the issue note keeps its "opened by X" line but
	// carries a pointer to the instruction instead of a second copy of it. The
	// instruction is uncapped, as every instruction is — which is also why the
	// pointer is better than the copy, since the copy would be the capped one.
	//
	// The attribution is what makes this safe. The gate upstream is assignment,
	// not authorship — assigning an issue is the act of instruction — so a body
	// somebody else wrote can reach this path, and the note still says whose
	// words they are.
	//
	// With Resume it composes rather than competing. Resume drops the issue
	// note as something the conversation has already been told, and this drops
	// the comment note as something that does not exist; together they leave
	// nothing to frame, so the Context section is omitted altogether and the
	// prompt is the header and the instruction. That combination is not
	// reachable today — a body fires at most once per issue, so a body-
	// triggered run is always the first run of its conversation — but the
	// resume fallback in internal/turn rebuilds this same input with Resume
	// flipped to false, and the flip has to stay the whole of the difference.
	BodyIsInstruction bool

	// MayAsk is the resolved current question policy from internal/turn. Plan
	// defaults to material asynchronous questions, Research requires opt-in,
	// and an explicit no-question instruction overrides both. This renderer
	// never derives permission from untrusted context or a prior transcript.
	MayAsk bool

	// PreviousMayAsk is whether the run before this one had that same
	// permission. It is read only on the resume path, where it is compared with
	// MayAsk and nothing else.
	//
	// It is what the previous run *was allowed* to do, not what it did. Whether
	// it actually asked would be the more precise bit and there is nowhere to
	// read it from: no column records it, and it is not the bit that matters
	// anyway. The dangerous cell is a conversation that could ask and now cannot,
	// where the transcript still says permission exists and a resumed prompt
	// repeats nothing — so unless this is compared and the difference stated, the
	// old permission simply survives. internal/turn re-derives it from the
	// previous run's own trigger comment; there is no new state for it.
	PreviousMayAsk bool

	// PreviousMode is the mode of the run before this one, and is read only on
	// the resume path. Empty means there was no previous run, which only a
	// cold run can be true of.
	//
	// [writeResumeHeader] compares this against the current mode to decide
	// whether to re-emit infer guidance on a mode change mid-conversation, but
	// that comparison is now unreachable: internal/turn's segment rotation
	// (T-1, prepareDeliverySegment) starts a fresh segment — a cold run, never
	// a resume — on any mode change, so a resumed run's mode can no longer
	// disagree with PreviousMode. The field and the comparison stay rather
	// than being deleted, because nothing in this package can prove that
	// invariant holds on its own.
	PreviousMode string

	// History is the earlier comments on this issue, oldest first, without the
	// one that triggered this run — that one is already a note of its own and
	// saying it twice would only make it ambiguous which of the two is live.
	//
	// It is rendered on the cold path only. A resumed conversation has been
	// through all of this once already and re-sending it would spend the very
	// context the resume exists to preserve. The field is still populated on
	// the resume path, because internal/turn's fallback rebuilds this same
	// value with Resume flipped to false when claude refuses to resume, and
	// that rerun is a cold start that needs the history like any other.
	History []HistoryEntry

	// Attachments is the rendered body of the "## Attachments" section: one
	// line per file litefactory fetched for this run, already composed by
	// internal/attach (see attach.PromptSection).
	//
	// It arrives as a finished string rather than as a slice of structs on
	// purpose. This package renders prompts; it does not know what an
	// attachment is, where one landed, or which of them were declined, and
	// giving it a per-attachment type would pull the whole ingestion
	// vocabulary across a boundary that exists to keep it out (US-3's
	// architecture note: exactly one new field, no per-attachment struct
	// reaching internal/harness).
	//
	// Empty means the turn had no attachments, and then no section is
	// written at all — not an empty heading. A turn without attachments must
	// produce byte-identical output to a build of this package from before
	// the feature existed (AC-3.3), which is what makes this field safe to
	// add to a struct whose output is pinned.
	//
	// It is rendered on every path, including a resumed run. Attachments
	// belong to the comment that triggered *this* run, so unlike History
	// there is nothing about them the conversation has already seen.
	Attachments string

	// PreviousFailed is whether the run before this one did not genuinely
	// succeed, and is read only on the resume path. It comes from the store's
	// own derived success predicate on the previous run's stored row —
	// internal/turn negates that result and hands it here already settled, the
	// same way [PreviousMayAsk] and [PreviousMode] arrive pre-derived —
	// because a resumed conversation's own transcript cannot be trusted to say
	// so: the last thing claude said before a failed run ended often reads
	// like a finished, successful report. False means there was no previous
	// run, or the previous run genuinely succeeded, which only a cold run or a
	// successful resume predecessor can be true of.
	PreviousFailed bool

	// PreviousReason is why the previous run failed, and is read only on the
	// resume path alongside [PreviousFailed]. It is the previous run's own
	// stored error reason — an abandonment, a timeout, or claude's own
	// non-zero exit — and is empty whenever PreviousFailed is false.
	PreviousReason string

	// SessionArtifactDir is this task's aiteam session directory, already
	// located by internal/turn — repository-relative and slash-separated,
	// ready to interpolate into the prompt exactly as it is (CFG-8: never an
	// absolute worktree path, never one under $LF_HOME). "" means internal/turn
	// located none, which is the ordinary case for a task's first-ever run.
	//
	// It is populated on every prepared run, cold or resumed, but only ever
	// rendered on the cold path — see [writeExistingSessionArtifacts].
	SessionArtifactDir string

	// SessionArtifactBrief is the file name, inside SessionArtifactDir, of the
	// brief relevant to the admitted mode — internal/turn names one only when
	// that file is actually present there. "" means either SessionArtifactDir
	// is itself "", the admitted mode has no brief of its own, or the file the
	// mode wants is not present.
	SessionArtifactBrief string

	// SessionArtifactTechnical is the verified optional Plan research evidence file.
	SessionArtifactTechnical string
}

// BuildPrompt assembles the prompt sent to claude on stdin.
//
// It states facts and fences untrusted material; it never says what the agent
// should do with them. What every fact and section means for the agent is in
// the run-rules skill under teams/source.
//
// It validates rather than silently producing a prompt with a hole in it: an
// empty instruction or an empty slug is a bug in the caller, and discovering it
// here costs nothing while discovering it from a confused run costs a turn.
func BuildPrompt(in PromptInput) (string, error) {
	if err := in.Ref.Validate(); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPrompt, err)
	}
	if err := naming.ValidateSlug(in.FeatureSlug); err != nil {
		return "", fmt.Errorf("%w: feature slug: %w", ErrInvalidPrompt, err)
	}
	if in.Workflow != "" {
		if _, err := agent.ResolveWorkflow(in.Workflow, agent.Mode(in.Mode)); err != nil {
			return "", fmt.Errorf("%w: prompt workflow: %w", ErrInvalidPrompt, err)
		}
	}
	if strings.TrimSpace(in.Mode) == "" {
		return "", fmt.Errorf("%w: mode is empty", ErrInvalidPrompt)
	}
	if in.RunOrdinal < 1 {
		return "", fmt.Errorf("%w: run ordinal %d is below 1", ErrInvalidPrompt, in.RunOrdinal)
	}
	if strings.TrimSpace(in.Instruction) == "" {
		return "", fmt.Errorf("%w: instruction is empty", ErrInvalidPrompt)
	}

	ordinal, err := naming.FormatRunOrdinal(in.RunOrdinal)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidPrompt, err)
	}

	var b strings.Builder
	if len(in.PreparationCaveats) > 0 {
		b.WriteString(note("preparation observations recorded by the factory", neutraliseNoteTags(strings.Join(in.PreparationCaveats, "\n"))))
		b.WriteString("\n")
	}
	if in.Resume {
		writeResumeHeader(&b, in, ordinal)
	} else {
		writeHeader(&b, in, ordinal)
	}

	if sync := in.DeferredSync; sync.FeatureBranch != "" {
		b.WriteString("\n## Merge conflict to resolve\n\n")
		fmt.Fprintf(&b, "- Feature branch: %q\n- Story branch: %q\n- Conflicting files:\n", capText(sync.FeatureBranch, 256), capText(sync.Branch, 256))
		for i, path := range sync.Conflicts {
			if i == maxPromptConflicts {
				fmt.Fprintf(&b, "  - … and %d more\n", len(sync.Conflicts)-i)
				break
			}
			fmt.Fprintf(&b, "  - %q\n", capText(path, 512))
		}
	} else if sync.Deferred {
		b.WriteString("\n## Base synchronization deferred\n\n")
		fmt.Fprintf(&b, "- Branch: %q\n- Base: %q\n- Behind: %d commit(s)\n- Ahead: %d commit(s)\n- Uncommitted changes: preserved\n",
			capText(sync.Branch, 256), capText(sync.Base, 256), sync.Behind, sync.Ahead)
	}

	b.WriteString(OwnerGuidanceSection(in.OwnerGuidanceRevision, in.OwnerGuidanceBody))
	b.WriteString("\n## Instruction\n\n")
	b.WriteString(strings.TrimSpace(in.Instruction))
	b.WriteString("\n")

	// Above the Context section, because these are files this run may open
	// rather than third-party text to weigh — and before the resume return
	// below, because a resumed run's trigger comment can carry attachments
	// the conversation has never seen.
	writeAttachments(&b, in)
	if !in.Resume {
		writeExistingSessionArtifacts(&b, in)
	}
	if err := writeValidation(&b, in); err != nil {
		return "", err
	}
	if err := writeApprovedStories(&b, in); err != nil {
		return "", err
	}
	if in.RefinementStory != "" {
		if len(in.RefinementStory) > 70*1024 || len(in.RefinementSource) > 1024 {
			return "", fmt.Errorf("%w: refinement context exceeds limit", ErrInvalidPrompt)
		}
		b.WriteString("\n## Current target story\n\n")
		b.WriteString(note(capText(in.RefinementSource, 1024), neutraliseNoteTags(in.RefinementStory)))
	}

	// A resumed run whose trigger was the issue body has nothing to frame at
	// all: the issue note is in the conversation's first prompt and there is no
	// comment. The heading is dropped with its contents rather than left
	// standing over nothing, because an empty section reads as a hole where
	// something was meant to be.
	if in.Resume && in.BodyIsInstruction {
		return b.String(), nil
	}

	b.WriteString("\n## Context\n\n")
	if in.Resume {
		// One note, not two: the issue was in this conversation's first prompt
		// and has not changed. The comment that triggered *this* run has not
		// been seen before, and it is third-party text like any other.
		b.WriteString(commentNote(in))
		return b.String(), nil
	}
	b.WriteString(issueNote(in))
	if history := historyNote(in); history != "" {
		b.WriteString("\n")
		b.WriteString(history)
	}
	if !in.BodyIsInstruction {
		b.WriteString("\n")
		b.WriteString(commentNote(in))
	}

	return b.String(), nil
}

// writeAttachments writes the "## Attachments" section, or nothing at all.
//
// Nothing at all is the important half. A turn with no attachments must
// produce the same bytes this function's absence produced (AC-3.3), so an
// empty [PromptInput.Attachments] writes no heading, no blank line and no
// trailing newline — the section does not exist rather than existing empty.
func writeAttachments(b *strings.Builder, in PromptInput) {
	section := strings.TrimSpace(in.Attachments)
	if section == "" {
		return
	}
	b.WriteString("\n## Attachments\n\n")
	b.WriteString(section)
	b.WriteString("\n")
}

// writeExistingSessionArtifacts writes the "## Existing session artifacts"
// section naming this task's already-located aiteam session directory and,
// when the admitted mode has one, the brief inside it (AC-009).
//
// Nothing at all is the important half, the same discipline [writeAttachments]
// follows (AC-010): when internal/turn located no directory,
// in.SessionArtifactDir is "" and this writes no heading, no section and no
// trailing newline.
//
// BuildPrompt calls this only on the cold path (AC-011): a resumed
// conversation was already pointed at this directory in its first prompt.
func writeExistingSessionArtifacts(b *strings.Builder, in PromptInput) {
	dir := strings.TrimSpace(in.SessionArtifactDir)
	if dir == "" {
		return
	}
	b.WriteString("\n## Existing session artifacts\n\n")
	fmt.Fprintf(b, "- Directory: `%s`\n", dir)
	if brief := strings.TrimSpace(in.SessionArtifactBrief); brief != "" {
		fmt.Fprintf(b, "- Brief: `%s/%s`\n", dir, brief)
	}
	if technical := strings.TrimSpace(in.SessionArtifactTechnical); technical != "" {
		fmt.Fprintf(b, "- Technical findings: `%s/%s`\n", dir, technical)
	}
}

// questionsLine states this run's question permission as a header fact.
func questionsLine(mayAsk bool) string {
	if mayAsk {
		return "- Questions: allowed\n"
	}
	return "- Questions: not allowed\n"
}

// requestLine names what started an infer-mode run. A body-triggered prompt
// carries no comment note, so the fact says which of the two it was.
func requestLine(bodyIsInstruction bool) string {
	if bodyIsInstruction {
		return "- Request: an issue whose body named no command\n"
	}
	return "- Request: a comment that named no command\n"
}

// writeHeader writes the part of the prompt litefactory itself is asserting.
func writeHeader(b *strings.Builder, in PromptInput, ordinal string) {
	fmt.Fprintf(b, "# litefactory run %s of %s\n\n", ordinal, promptTask(in))
	fmt.Fprintf(b, "- Task: %s\n", promptTask(in))
	// AC-9: this exact literal — "- Mode: %s\n" — is what CLAUDE.md's
	// ## Classify section parses to decide whether a run can trust the
	// header and skip re-classification. That consumer lives in a markdown
	// file outside this package's compile boundary, so keep the format.
	fmt.Fprintf(b, "- Mode: %s\n", strings.TrimSpace(in.Mode))
	// D-3: the feature slug is stated on both paths so that a run never stops
	// on a bookkeeping question litefactory already knows the answer to.
	fmt.Fprintf(b, "- Feature slug: %s\n", in.FeatureSlug)
	if in.Branch != "" {
		fmt.Fprintf(b, "- Branch: %s\n", in.Branch)
	}
	b.WriteString("- Working directory: the git worktree you are already in.\n")
	b.WriteString(questionsLine(in.MayAsk))
	if strings.TrimSpace(in.Mode) == InferMode {
		b.WriteString(requestLine(in.BodyIsInstruction))
	}
	if in.PreparationAssessment {
		b.WriteString("- Scope: preparation only (managed scope is not approved and frozen)\n")
	}
}

// writeResumeHeader writes the header of a run that is continuing a
// conversation: the delta, and nothing the conversation has already been told.
//
// The run number and the task ref stay because they are what changed. The
// mode stays because it can change between runs. The feature slug stays as
// insurance (see [PromptInput.Resume]). The question permission is restated
// on every resume, and a change is named, because silence would let the
// transcript's old permission survive (AC-A1.12).
func writeResumeHeader(b *strings.Builder, in PromptInput, ordinal string) {
	fmt.Fprintf(b, "# litefactory run %s of %s\n\n", ordinal, promptTask(in))
	// AC-9/AC-10: same literal as writeHeader above.
	fmt.Fprintf(b, "- Mode: %s\n", strings.TrimSpace(in.Mode))
	fmt.Fprintf(b, "- Feature slug: %s\n", in.FeatureSlug)
	b.WriteString(questionsLine(in.MayAsk))
	if in.MayAsk != in.PreviousMayAsk {
		if in.PreviousMayAsk {
			b.WriteString("- Questions changed: the previous run could ask; this one cannot\n")
		} else {
			b.WriteString("- Questions changed: the previous run could not ask; this one can\n")
		}
	}
	if in.PreviousFailed {
		b.WriteString("- Previous run: failed\n")
	}
	// Unreachable today: segment rotation starts a cold run on any mode
	// change. Kept because nothing in this package can prove that invariant.
	mode := strings.TrimSpace(in.Mode)
	if mode == InferMode && mode != strings.TrimSpace(in.PreviousMode) {
		b.WriteString(requestLine(in.BodyIsInstruction))
	}
	if in.PreparationAssessment {
		b.WriteString("- Scope: preparation only (managed scope is not approved and frozen)\n")
	}
	if reason := strings.TrimSpace(in.PreviousReason); in.PreviousFailed && reason != "" {
		// runs.error can be agent-authored text; routing it through [note]
		// neutralises note tags and caps it, so a hostile reason stays
		// confined to its own element instead of reaching the header.
		b.WriteString("\n")
		b.WriteString(note("the previous run's own recorded failure reason", capText(reason, MaxPreviousReasonBytes)))
	}
}

// issueBodyIsTheInstruction stands in for the issue body when the body is what
// started the run. It says where the text went, so that a note without a body
// does not read as an issue that has none.
const issueBodyIsTheInstruction = "(this issue's body is the instruction above, in full and uncut; " +
	"it is not repeated here)"

// issueNote renders the issue as an untrusted note.
func issueNote(in PromptInput) string {
	// Flattened before it is capped, so that the cap applies to the title
	// itself and the truncation marker keeps its own line.
	title := truncateWithMarker(oneLine(neutraliseNoteTags(in.IssueTitle)), MaxIssueTitleBytes)
	body := capText(in.IssueBody, MaxIssueBodyBytes)
	switch {
	case in.BodyIsInstruction:
		// A pointer, not a copy: the copy would be the capped one.
		body = issueBodyIsTheInstruction
	case strings.TrimSpace(body) == "":
		body = "(the issue has no body)"
	}

	source := fmt.Sprintf("issue %s on %s/%s, opened by %s",
		promptIssue(in), in.Ref.Owner, in.Ref.Repo, attribution(in.IssueAuthor))
	return note(source, "Title: "+title+"\n\n"+body)
}

// commentNote renders the triggering comment as an untrusted note.
func commentNote(in PromptInput) string {
	body := capText(in.CommentBody, MaxCommentBytes)
	if strings.TrimSpace(body) == "" {
		body = "(the comment has no body)"
	}

	source := fmt.Sprintf("comment on issue %s, written by %s, which triggered this run",
		promptIssue(in), attribution(in.CommentAuthor))
	if in.RefinementSource != "" {
		source = "planning refinement comment from " + capText(in.RefinementSource, 1024) + ", written by " + attribution(in.CommentAuthor)
	}
	return note(source, body)
}

// historyNote renders the earlier comments on the issue as a single untrusted
// note, or "" when there are none to render.
//
// One note, not one per comment. N sibling notes would repeat the framing N
// times for no gain and blur which of them is the comment that actually
// triggered this run; the entries carry their own labels instead, and the
// labelling is the point (see [HistoryRole]).
//
// The Source line says two things the block cannot demonstrate about itself:
// that it is condensed — entries are capped individually and the oldest are
// dropped when the budget runs out — and that it may be missing comments
// altogether. litefactory records the highest comment id it finds on an issue
// it has just started watching and keeps none of the bodies, so anything said
// before it first saw the issue is not here to render. Saying so is the honest
// alternative to a block that silently implies it is the whole thread.
//
// The bodies are copied in as they were stored, which is unredacted: S-7
// redaction is outbound-only, so a secret pasted into a comment reaches every
// later cold-start NNNN.meta.json rather than only its own. That exposure is
// accepted deliberately — the metadata files are 0600 ([metaFileMode]) and
// local — and running the outbound redaction over this block was considered and
// rejected, because the model would then be shown a redacted version of text it
// was previously shown verbatim, which is its own kind of confusion.
func historyNote(in PromptInput) string {
	kept, omitted := selectHistory(in.History)
	if len(kept) == 0 {
		return ""
	}

	var content strings.Builder
	if omitted > 0 {
		fmt.Fprintf(&content, "%s %d earlier %s omitted\n\n",
			truncationMarker, omitted, pluralComments(omitted))
	}
	for i, entry := range kept {
		if i > 0 {
			content.WriteString("\n")
		}
		content.WriteString(entry)
	}

	source := fmt.Sprintf("a condensed record of earlier comments on issue %s, oldest first", promptIssue(in))
	return note(source, content.String())
}

// selectHistory renders the entries and drops the oldest of them until what is
// left fits [MaxHistoryBytes], returning the survivors in chronological order
// and the number dropped.
//
// The fill is newest-first because recency is what a cold start needs most: the
// last exchange before this one explains the request far better than the first
// one does. What survives is emitted oldest-first, because a conversation read
// backwards is not a conversation.
//
// The newest entry is always kept, even alone and over budget. A history block
// whose entire content is "everything was omitted" is worse than one that
// overshoots, and the overshoot is bounded by the per-entry cap.
//
// The budget is spent on the entries' own bytes. The frame around them and the
// blank lines between them are litefactory's own words, not untrusted material,
// and are not counted against it.
func selectHistory(entries []HistoryEntry) ([]string, int) {
	kept := make([]string, 0, len(entries))
	budget := MaxHistoryBytes
	for i := len(entries) - 1; i >= 0; i-- {
		rendered := renderHistoryEntry(entries[i])
		if len(kept) > 0 && len(rendered) > budget {
			// Everything from here down is older than what is already kept, so
			// there is nothing further to try: the entries only get older.
			slices.Reverse(kept)
			return kept, i + 1
		}
		budget -= len(rendered)
		kept = append(kept, rendered)
	}
	slices.Reverse(kept)
	return kept, 0
}

// renderHistoryEntry renders one entry: the role, the raw handle beside it, the
// timestamp, and the body capped and neutralised exactly as every other
// untrusted body in this prompt is.
func renderHistoryEntry(entry HistoryEntry) string {
	body := capText(entry.Body, MaxCommentBytes)
	if strings.TrimSpace(body) == "" {
		body = "(this comment has no body)"
	}
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	return fmt.Sprintf("[%s] %s at %s:\n%s",
		entry.Role.label(), attribution(entry.Author), historyTime(entry.At), body)
}

// historyTime renders an entry's timestamp in UTC, so that the same thread
// produces the same prompt wherever it is assembled. It is the forge's
// timestamp for the comment; [BuildPrompt] reads no clock of its own.
func historyTime(t time.Time) string {
	if t.IsZero() {
		return "an unknown time"
	}
	return t.UTC().Format(time.RFC3339)
}

// pluralComments picks the noun for a count, so that the omission line reads as
// English rather than as a template with a number in it.
func pluralComments(n int) string {
	if n == 1 {
		return "comment"
	}
	return "comments"
}

// note wraps already-capped, already-neutralised content in the <note> frame.
func note(source, content string) string {
	var b strings.Builder
	b.WriteString("<note>\n")
	b.WriteString("Source: " + source + "\n\n")
	b.WriteString(content)
	if !strings.HasSuffix(content, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("</note>\n")
	return b.String()
}

// attribution renders an author handle, or says plainly that there is none.
// "unknown" beats an empty space that reads as if litefactory wrote the note.
func attribution(handle string) string {
	handle = oneLine(strings.TrimSpace(handle))
	if handle == "" {
		return "an unknown author"
	}
	return "@" + strings.TrimPrefix(handle, "@")
}

// capText neutralises note framing in untrusted text and cuts it to limit.
func capText(s string, limit int) string {
	return truncateWithMarker(neutraliseNoteTags(s), limit)
}

// neutraliseNoteTags defuses <note> and </note> inside untrusted text.
//
// The frame is the only thing separating "the issue says" from "litefactory
// says". A body that closes the element early would put the rest of itself
// outside the frame, which is prompt injection by punctuation. Replacing the
// angle brackets with square ones keeps the text readable and its meaning
// intact while making it inert as markup.
func neutraliseNoteTags(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == '<' {
			if n := matchNoteTag(s[i:]); n > 0 {
				b.WriteByte('[')
				b.WriteString(s[i+1 : i+n-1])
				b.WriteByte(']')
				i += n
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// matchNoteTag reports the length of a <note> or </note> tag at the start of s,
// case-insensitively, or 0 if there is none.
func matchNoteTag(s string) int {
	for _, tag := range [...]string{"</note>", "<note>"} {
		if len(s) >= len(tag) && strings.EqualFold(s[:len(tag)], tag) {
			return len(tag)
		}
	}
	return 0
}

// truncateWithMarker cuts s to limit bytes on a rune boundary and appends the
// marker on its own line, naming how much was dropped.
func truncateWithMarker(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	kept := truncateUTF8(s, limit)
	return fmt.Sprintf("%s\n%s %d of %d bytes shown\n", kept, truncationMarker, len(kept), len(s))
}

// truncateUTF8 returns the longest prefix of s that is at most limit bytes and
// does not split a rune. A cut through a multi-byte character would produce
// invalid UTF-8, which some JSON encoders replace and others reject.
func truncateUTF8(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// oneLine flattens text that must occupy a single line, such as an issue title
// used inside a "Title:" field.
func oneLine(s string) string {
	replacer := strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ")
	return strings.TrimSpace(replacer.Replace(s))
}

func promptIssue(in PromptInput) string {
	if in.IssueReference != "" {
		return capText(in.IssueReference, 1024)
	}
	if in.Ref.Forge == "linear" {
		return "this issue"
	}
	return fmt.Sprintf("#%d", in.Ref.Issue)
}
func promptTask(in PromptInput) string {
	if in.Ref.Forge == "linear" {
		return promptIssue(in)
	}
	return in.Ref.String()
}
