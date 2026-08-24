package turn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// MarkerPrefix is the literal every outbound litefactory comment body starts
// with, and the substring the self-filter looks for anywhere in an inbound one
// (S-8).
//
// It is the *prefix* of the marker rather than a whole marker, deliberately:
// both shapes litefactory posts — "<!-- lf:status task=… run=N -->" for a run
// report and "<!-- lf:reply task=… -->" for the run-less replies of AC-2.18 and
// AC-2.20 — begin with it, so one substring test catches every comment
// litefactory has ever written, including shapes added later.
//
// internal/report composes its own marker literals rather than importing this
// constant: the reporter is a dependency of a turn, so an import back this way
// would be a cycle. The duplication is one string, and this side is the one
// that has to keep matching.
const MarkerPrefix = "<!-- lf:"

// HasMarker reports whether body carries a litefactory marker anywhere in it.
//
// Anywhere, not at the start: GitHub's own "quote reply" and a human pasting a
// report back into a comment both move the marker off byte zero, and a comment
// that contains litefactory's output is still litefactory's output as far as
// looping is concerned. Over-matching here costs a dropped comment that a human
// can repost without the quote; under-matching costs an infinite loop against a
// live issue.
func HasMarker(body string) bool { return strings.Contains(body, MarkerPrefix) }

// SelfReason names which of the three self-filter conditions fired. It exists
// for the log line: "dropped as self" is not actionable, "dropped as self
// because the author id is the bot's" is.
type SelfReason string

// The self-filter conditions, in the order they are evaluated.
const (
	// SelfReasonNone means the comment is not litefactory's own.
	SelfReasonNone SelfReason = ""

	// SelfReasonBotUserID means the author's numeric id is the stored bot id.
	// This is the load-bearing one: it is the only condition that fires for a
	// fine-grained PAT on a machine *user* account, which GitHub reports as
	// type "User".
	SelfReasonBotUserID SelfReason = "bot_user_id"

	// SelfReasonForgeBot means the forge itself flagged the author as a bot.
	SelfReasonForgeBot SelfReason = "forge_bot_flag"

	// SelfReasonMarker means the body carries [MarkerPrefix]. This is the
	// belt-and-braces condition: it holds even when the stored identities are
	// wrong, missing or stale, so a misconfigured install still cannot read its
	// own report back as an instruction.
	SelfReasonMarker SelfReason = "marker"
)

// Identities are the two numeric forge ids every classification decision is
// made on.
//
// The handles are carried for log lines and nothing else. Comparing them would
// be a bug: GitHub lets an account be renamed, so a handle is a label with a
// forwarding address, while the numeric id is the account (D-I).
type Identities struct {
	// OwnerUserID is the numeric id of the human whose comments are
	// instructions.
	OwnerUserID int64
	// OwnerHandle is the owner's login, for display only.
	OwnerHandle string
	// BotUserID is the numeric id of the account litefactory posts as.
	BotUserID int64
	// BotHandle is the bot's login, for display only.
	BotHandle string
}

// Validate reports why these identities cannot be used to classify comments.
//
// Both ids must be known, because a zero id would silently match the zero id
// GitHub reports for a deleted account, and they must differ, because an
// account that is both owner and bot reads its own reports as instructions —
// the loop this whole task exists to prevent (AC-0.6).
func (i Identities) Validate() error {
	switch {
	case i.OwnerUserID == 0:
		return errors.New("turn: identities: owner user id is not known; run `litefactory init`")
	case i.BotUserID == 0:
		return errors.New("turn: identities: bot user id is not known; configure the bot credential and run `litefactory init`")
	case i.OwnerUserID == i.BotUserID:
		return fmt.Errorf("turn: identities: owner and bot are the same account (user id %d)", i.OwnerUserID)
	}
	return nil
}

// identityReader is the slice of the store the filter needs to build itself.
// It is declared here, at the point of consumption, so a test can supply the
// two identities without a database.
type identityReader interface {
	GetIdentity(ctx context.Context, forge string, role store.IdentityRole) (store.Identity, error)
}

// LoadIdentities reads the owner and bot identities for a forge.
//
// A missing bot identity is a normal state for a freshly initialised install
// (AC-0.5) but not a state a classifying daemon may run in, so it is reported
// as an error here — wrapping [store.ErrNotFound], which the caller can test
// for to tell "not configured yet" from "the database is broken".
func LoadIdentities(ctx context.Context, r identityReader, forge string) (Identities, error) {
	owner, err := r.GetIdentity(ctx, forge, store.RoleOwner)
	if err != nil {
		return Identities{}, fmt.Errorf("turn: load owner identity: %w", err)
	}
	bot, err := r.GetIdentity(ctx, forge, store.RoleBot)
	if err != nil {
		return Identities{}, fmt.Errorf("turn: load bot identity: %w", err)
	}

	identities := Identities{
		OwnerUserID: owner.UserID,
		OwnerHandle: owner.Handle,
		BotUserID:   bot.UserID,
		BotHandle:   bot.Handle,
	}
	if err := identities.Validate(); err != nil {
		return Identities{}, err
	}
	return identities, nil
}

// Verdict is what the filter decided about one comment.
type Verdict struct {
	// Classification is the S-8 verdict: self, instruction or third_party.
	Classification store.Classification
	// SelfReason is which self-filter condition fired, and is
	// [SelfReasonNone] unless Classification is [store.ClassificationSelf].
	SelfReason SelfReason
}

// Triggers reports whether this comment may start work. Exactly one
// classification can: an instruction from the owner. A third-party comment is
// recorded and tagged and does nothing (AC-2.15); litefactory's own comment
// does nothing (AC-2.14).
func (v Verdict) Triggers() bool { return v.Classification == store.ClassificationInstruction }

// IsSelf reports whether the comment is litefactory's own.
func (v Verdict) IsSelf() bool { return v.Classification == store.ClassificationSelf }

// commentRecorder is the slice of the store the filter writes through,
// declared at the point of consumption so a turn can be tested without one.
type commentRecorder interface {
	RecordComment(ctx context.Context, c store.Comment) (store.Comment, error)
}

// Filter classifies inbound comments and records what it decided.
//
// It is the daemon's gate: every comment a tick sees goes through
// [Filter.Admit], and only the ones it says trigger reach [ParseCommand] and a
// run. Every comparison it makes is on a numeric forge id.
type Filter struct {
	identities Identities
	comments   commentRecorder
	log        *slog.Logger
}

// NewFilter builds a filter over a comment store and the two identities.
//
// The identities are validated here rather than per comment, so an install
// whose owner and bot are the same account, or whose ids were never derived,
// cannot produce a filter at all — it fails at wiring time, before a single
// comment has been read.
func NewFilter(comments commentRecorder, identities Identities, log *slog.Logger) (*Filter, error) {
	if comments == nil {
		return nil, errors.New("turn: new filter: comment recorder is required")
	}
	if err := identities.Validate(); err != nil {
		return nil, err
	}
	return &Filter{identities: identities, comments: comments, log: log}, nil
}

// Identities returns the ids this filter decides on, for a startup log line.
func (f *Filter) Identities() Identities { return f.identities }

// Classify decides what a comment is, without touching the database.
//
// The order is the point. A comment is tested for being litefactory's own
// first and dropped if it is, before anything can look at it and see an
// instruction — that ordering is what makes AC-2.14 hold even when the bot
// account is also the owner's account by misconfiguration.
//
//	self         — the author id is the bot's, or the forge flagged the author
//	               as a bot, or the body carries `<!-- lf:` anywhere.
//	instruction  — the author id is the owner's (AC-2.15).
//	third_party  — any other author. Recorded, tagged, triggers nothing.
//
// The parameter is a [github.Comment] and never an issue: an issue body is not
// a comment and can never be an instruction, so a task opened by someone else
// starts nothing (AC-2.16). There is deliberately no overload that takes a
// bare body and an author.
func (f *Filter) Classify(c github.Comment) Verdict {
	if reason := f.selfReason(c); reason != SelfReasonNone {
		return Verdict{Classification: store.ClassificationSelf, SelfReason: reason}
	}
	if f.identities.OwnerUserID != 0 && c.Author.ID == f.identities.OwnerUserID {
		return Verdict{Classification: store.ClassificationInstruction}
	}
	return Verdict{Classification: store.ClassificationThirdParty}
}

// selfReason is the whole self-filter: one code path, three conditions, any of
// which is sufficient (S-8). They are ordered by how much they explain — the
// id match is the true statement about who wrote it, the marker is the fallback
// that survives the ids being wrong.
//
// The id comparison is guarded on a non-zero bot id so that the zero id GitHub
// reports for a deleted account cannot match an unknown bot and quietly drop
// every such comment.
func (f *Filter) selfReason(c github.Comment) SelfReason {
	switch {
	case f.identities.BotUserID != 0 && c.Author.ID == f.identities.BotUserID:
		return SelfReasonBotUserID
	case c.Author.IsBot:
		return SelfReasonForgeBot
	case HasMarker(c.Body):
		return SelfReasonMarker
	default:
		return SelfReasonNone
	}
}

// Admission is a classified, recorded comment.
type Admission struct {
	// Verdict is what the comment was classified as.
	Verdict Verdict
	// Comment is the stored row, including the cN tag a third-party comment
	// was given.
	Comment store.Comment
}

// Triggers reports whether the daemon may act on this comment.
func (a Admission) Triggers() bool { return a.Verdict.Triggers() }

// Admit classifies a comment and records it against a session, returning what
// it decided and the stored row.
//
// Every comment is recorded, including litefactory's own: a dropped comment
// that left no trace is indistinguishable from a comment that was never
// fetched, and "it never reacted to itself" is a claim worth being able to
// check against the database after the fact.
//
// A third-party comment is stored with the session's next cN tag, allocated by
// the store inside the insert transaction so two comments cannot be handed the
// same one. The tag is unused at M0 and is what /include consumes at M1.
//
// Recording is idempotent on (session, comment id) — the inclusive `since`
// filter of D-A means a comment is routinely re-seen — so a replayed tick
// returns the row already stored and writes nothing. The returned Verdict is
// always freshly computed from the current identities; it can differ from the
// stored row's classification only if the identities changed after the comment
// was first seen, and the fresh one is the one to act on.
func (f *Filter) Admit(ctx context.Context, sessionID int64, c github.Comment) (Admission, error) {
	if sessionID == 0 {
		return Admission{}, errors.New("turn: admit comment: session id is required")
	}
	if c.ID == 0 {
		return Admission{}, errors.New("turn: admit comment: forge comment id is required; only comments are classified, never an issue body")
	}

	verdict := f.Classify(c)
	recorded, err := f.comments.RecordComment(ctx, store.Comment{
		SessionID:      sessionID,
		ForgeCommentID: c.ID,
		AuthorHandle:   c.Author.Login,
		AuthorUserID:   c.Author.ID,
		AuthorIsBot:    c.Author.IsBot,
		Body:           c.Body,
		CreatedAt:      c.CreatedAt,
		Classification: verdict.Classification,
	})
	if err != nil {
		return Admission{}, fmt.Errorf("turn: admit comment %d on session %d: %w", c.ID, sessionID, err)
	}

	f.logger().DebugContext(ctx, "turn: classified comment",
		slog.Int64("session_id", sessionID),
		slog.Int64("comment_id", c.ID),
		slog.String("classification", string(verdict.Classification)),
		slog.String("self_reason", string(verdict.SelfReason)),
		slog.Int64("author_user_id", c.Author.ID),
		slog.String("author_handle", c.Author.Login),
		slog.String("tag", recorded.Tag),
		slog.Bool("triggers", verdict.Triggers()),
	)
	return Admission{Verdict: verdict, Comment: recorded}, nil
}

// logger returns the filter's logger, or a discarding one when it was built
// without.
func (f *Filter) logger() *slog.Logger {
	if f == nil || f.log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return f.log
}
