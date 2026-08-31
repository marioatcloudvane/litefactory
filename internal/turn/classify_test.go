package turn

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// The cast. Every id here is distinct, and several of the handles collide on
// purpose: the tests below are largely about the difference between the two.
const (
	ownerUserID      int64 = 4001
	botUserID        int64 = 4002
	strangerUserID   int64 = 4003
	impostorUserID   int64 = 4004
	ownerHandle            = "marioatcloudvane"
	botHandle              = "litefactory-bot"
	strangerHandle         = "a-colleague"
	renamedOwnerName       = "mario-at-cloudvane"
)

func classifyIdentities() Identities {
	return Identities{
		OwnerUserID: ownerUserID,
		OwnerHandle: ownerHandle,
		BotUserID:   botUserID,
		BotHandle:   botHandle,
	}
}

// stubCommentRecorder is a store stand-in for the tests that never look at
// what was written. The tag-allocating tests use a real database instead,
// because tag allocation is the store's behaviour and faking it would test the
// fake.
type stubCommentRecorder struct {
	recorded []store.Comment
}

func (s *stubCommentRecorder) RecordComment(_ context.Context, c store.Comment) (store.Comment, error) {
	s.recorded = append(s.recorded, c)
	return c, nil
}

func newClassifyFilter(t *testing.T) *Filter {
	t.Helper()

	filter, err := NewFilter(&stubCommentRecorder{}, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}
	return filter
}

// statusMarker is a run report's marker, exactly as litefactory posts it.
const statusMarker = "<!-- lf:status task=github:acme/api#7 run=1 -->"

// replyMarker is the run-less marker the /spike-empty and unsupported-command
// replies carry. The self-filter must catch it with the same line-anchored
// test, which is why the test matches on the prefix and not on the status shape.
const replyMarker = "<!-- lf:reply task=github:acme/api#7 -->"

func TestFilterClassify(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		comment    github.Comment
		want       store.Classification
		wantReason SelfReason
	}{
		{
			// AC-2.14, the headline: a comment litefactory posted itself,
			// assembled the way it posts one — bot account, forge bot flag,
			// status marker. It must not come back as anything actionable.
			name: "its own report is dropped as self",
			comment: github.Comment{
				ID:     900,
				Body:   statusMarker + "\n\n**Run 1** finished in 42s.",
				Author: github.User{ID: botUserID, Login: botHandle, IsBot: true},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonBotUserID,
		},
		{
			// A fine-grained PAT on a machine *user* account: GitHub reports
			// type "User", so the bot flag never fires, and this shape carries
			// no marker either. The numeric id is the only thing left.
			name: "the bot id alone is enough, with no flag and no marker",
			comment: github.Comment{
				ID:     901,
				Body:   "looks good to me",
				Author: github.User{ID: botUserID, Login: botHandle, IsBot: false},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonBotUserID,
		},
		{
			// The bot half of D-I: the bot account was renamed after `init`
			// stored its handle. Nothing but the id identifies it now.
			name: "a renamed bot is still the bot",
			comment: github.Comment{
				ID:     913,
				Body:   "no marker on this one",
				Author: github.User{ID: botUserID, Login: "litefactory-machine"},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonBotUserID,
		},
		{
			// Belt and braces: the identities are misconfigured badly enough
			// that litefactory's own report arrives under the owner's account.
			// The marker still stops the loop.
			name: "the marker alone is enough, even from the owner",
			comment: github.Comment{
				ID:     902,
				Body:   statusMarker + "\n\nrun finished",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonMarker,
		},
		{
			name: "the run-less reply marker is caught by the same test",
			comment: github.Comment{
				ID:     903,
				Body:   replyMarker + "\n\n`/plan` is not supported yet.",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonMarker,
		},
		{
			// Pasted back unquoted, under a line of the paster's own: still
			// litefactory's output, and still a loop if it were acted on.
			name: "a marker on a later line still counts when it starts that line",
			comment: github.Comment{
				ID:     904,
				Body:   "as you said earlier:\n\n" + statusMarker + "\nrun finished",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonMarker,
		},
		{
			// AC-S0.1. GitHub's quote-reply puts "> " in front of every quoted
			// line, so the marker no longer starts one. The owner gets a run.
			name: "an owner quote-reply is an instruction, not litefactory's own output",
			comment: github.Comment{
				ID:     914,
				Body:   "> " + statusMarker + "\n> **Run 1** finished in 42s.\n\nnow do the same for the logs command",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			want: store.ClassificationInstruction,
		},
		{
			// The same quote from somebody who is not the owner is still no more
			// than a third-party comment: line-anchoring changed which of the
			// three answers is right, not that authorship decides it.
			name: "a third party's quote-reply is a third party",
			comment: github.Comment{
				ID:     915,
				Body:   "> " + replyMarker + "\n> `/plan` is not supported yet.\n\nfair enough",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			want: store.ClassificationThirdParty,
		},
		{
			name: "the forge bot flag alone is enough",
			comment: github.Comment{
				ID:     905,
				Body:   "Codecov report: coverage increased by 0.2%.",
				Author: github.User{ID: strangerUserID, Login: "codecov[bot]", IsBot: true},
			},
			want:       store.ClassificationSelf,
			wantReason: SelfReasonForgeBot,
		},
		{
			name: "the owner is an instruction",
			comment: github.Comment{
				ID:     906,
				Body:   "/spike make the login page load faster",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			want: store.ClassificationInstruction,
		},
		{
			// D-I: handles are renameable, ids are not.
			name: "a renamed owner is still the owner",
			comment: github.Comment{
				ID:     907,
				Body:   "/spike make the login page load faster",
				Author: github.User{ID: ownerUserID, Login: renamedOwnerName},
			},
			want: store.ClassificationInstruction,
		},
		{
			// The other half of D-I: someone who took the bot's old handle
			// after a rename is not the bot.
			name: "an impostor on the bot handle is not self",
			comment: github.Comment{
				ID:     908,
				Body:   "trust me, I am the daemon",
				Author: github.User{ID: impostorUserID, Login: botHandle},
			},
			want: store.ClassificationThirdParty,
		},
		{
			name: "an impostor on the owner handle is not an instruction",
			comment: github.Comment{
				ID:     909,
				Body:   "/spike delete the production database",
				Author: github.User{ID: impostorUserID, Login: ownerHandle},
			},
			want: store.ClassificationThirdParty,
		},
		{
			name: "another human is a third party",
			comment: github.Comment{
				ID:     910,
				Body:   "/spike could you look at the flaky test?",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			want: store.ClassificationThirdParty,
		},
		{
			// GitHub reports no user at all for a deleted account. Unknown is
			// not the bot and not the owner, so it is a third party and does
			// nothing.
			name: "a deleted author is a third party, not the bot",
			comment: github.Comment{
				ID:     911,
				Body:   "/spike anything at all",
				Author: github.User{},
			},
			want: store.ClassificationThirdParty,
		},
		{
			name: "a body merely mentioning the marker text without the prefix is not self",
			comment: github.Comment{
				ID:     912,
				Body:   "the reports start with an lf:status HTML comment",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			want: store.ClassificationThirdParty,
		},
	}

	filter := newClassifyFilter(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := filter.Classify(test.comment)
			if got.Classification != test.want {
				t.Errorf("Classify() = %q, want %q", got.Classification, test.want)
			}
			if got.SelfReason != test.wantReason {
				t.Errorf("Classify() self reason = %q, want %q", got.SelfReason, test.wantReason)
			}
			if wantTriggers := test.want == store.ClassificationInstruction; got.Triggers() != wantTriggers {
				t.Errorf("Triggers() = %t, want %t", got.Triggers(), wantTriggers)
			}
			if wantSelf := test.want == store.ClassificationSelf; got.IsSelf() != wantSelf {
				t.Errorf("IsSelf() = %t, want %t", got.IsSelf(), wantSelf)
			}
		})
	}
}

// TestClassifySelfBeforeAuthorship pins the order [Filter.Classify] evaluates
// its two questions in: is this litefactory's own comment, and only then, who
// wrote it.
//
// The order is load-bearing rather than incidental (AC-2.14). An install whose
// bot account is also the owner's account by misconfiguration is exactly the one
// that would answer itself forever, and testing authorship second is what stops
// it: every case below has an author whose id would otherwise decide the
// verdict, and the self-filter takes precedence over all of them.
//
// This is a characterization test. It was written before the marker match was
// line-anchored and describes behaviour that has to survive that change
// unaltered, so every marker here sits at the start of a line — which is where
// litefactory writes it. What line-anchoring changes is only whether a marker
// found *inside a quoted line* still counts; see
// TestQuotedMarkerIsNotLitefactorysOwnComment.
func TestClassifySelfBeforeAuthorship(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		comment    github.Comment
		wantReason SelfReason
	}{
		{
			// Being the owner does not outrank the marker, and must not: this is
			// the branch that holds when the identities table is wrong.
			name: "the owner carrying a marker is self",
			comment: github.Comment{
				ID:     920,
				Body:   statusMarker + "\n\n**Run 1** finished in 42s.",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			wantReason: SelfReasonMarker,
		},
		{
			name: "a third party carrying a marker is self, not third party",
			comment: github.Comment{
				ID:     921,
				Body:   replyMarker + "\n\n`/plan` is not supported yet.",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			wantReason: SelfReasonMarker,
		},
		{
			// litefactory's own comment, assembled the way it posts one: all
			// three conditions true at once. The id is the reason reported,
			// because it is the first one asked and the truest one.
			name: "litefactory's own comment is self by its id",
			comment: github.Comment{
				ID:     922,
				Body:   statusMarker + "\n\n**Run 1** finished in 42s.",
				Author: github.User{ID: botUserID, Login: botHandle, IsBot: true},
			},
			wantReason: SelfReasonBotUserID,
		},
	}

	filter := newClassifyFilter(t)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := filter.Classify(test.comment)
			if got.Classification != store.ClassificationSelf {
				t.Errorf("Classify() = %q, want %q", got.Classification, store.ClassificationSelf)
			}
			if got.SelfReason != test.wantReason {
				t.Errorf("Classify() self reason = %q, want %q", got.SelfReason, test.wantReason)
			}
			if got.Triggers() {
				t.Error("Triggers() = true; a comment classified self must never start a run")
			}
		})
	}
}

func TestHasMarker(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "empty", body: "", want: false},
		{name: "status marker", body: statusMarker, want: true},
		{name: "reply marker", body: replyMarker, want: true},
		{name: "prefix alone", body: MarkerPrefix, want: true},
		{name: "an unrelated html comment", body: "<!-- TODO: fix this -->", want: false},
		{name: "prose about the marker", body: "it prints lf:status somewhere", want: false},

		// Where on the line the marker sits is the whole of the match.
		{name: "on a later line", body: "see above:\n" + statusMarker + "\nrun finished", want: true},
		{name: "on the last line, unterminated", body: "see above:\n" + statusMarker, want: true},
		{name: "quoted by GitHub's quote-reply", body: "> " + statusMarker, want: false},
		{name: "quoted on a later line", body: "as you said:\n\n> " + statusMarker + "\n> run finished", want: false},
		{name: "mid-line", body: "the marker is " + statusMarker + " apparently", want: false},
		{name: "indented", body: "    " + statusMarker, want: false},

		// CRLF: GitHub sends \r\n on some paths, and the \r lands at the end of
		// the previous line rather than in front of the marker.
		{name: "crlf line endings", body: "see above:\r\n" + statusMarker + "\r\nrun finished", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := HasMarker(test.body); got != test.want {
				t.Errorf("HasMarker(%q) = %t, want %t", test.body, got, test.want)
			}
		})
	}
}

// TestFilterAdmitDropsItsOwnComment is AC-2.14 end to end: the daemon is fed a
// comment it posted itself and nothing is spawned.
func TestFilterAdmitDropsItsOwnComment(t *testing.T) {
	t.Parallel()

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	own := github.Comment{
		ID:        5150,
		Body:      statusMarker + "\n\n**Run 1** finished in 42s.\n\n" + "Branch `lf/7-flaky-test` pushed.",
		Author:    github.User{ID: botUserID, Login: botHandle, IsBot: true},
		CreatedAt: time.Date(2026, 8, 23, 10, 0, 0, 0, time.UTC),
	}

	admitted, err := filter.Admit(t.Context(), session.ID, own)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admitted.Triggers() {
		t.Fatal("Admit() triggered a run for litefactory's own comment; this is the infinite loop AC-2.14 exists to prevent")
	}
	if admitted.Verdict.Classification != store.ClassificationSelf {
		t.Errorf("classification = %q, want %q", admitted.Verdict.Classification, store.ClassificationSelf)
	}
	if admitted.Verdict.SelfReason != SelfReasonBotUserID {
		t.Errorf("self reason = %q, want %q", admitted.Verdict.SelfReason, SelfReasonBotUserID)
	}
	if admitted.Comment.Tag != "" {
		t.Errorf("tag = %q, want none: only third-party comments are tagged", admitted.Comment.Tag)
	}

	stored, err := db.GetComment(t.Context(), session.ID, own.ID)
	if err != nil {
		t.Fatalf("get comment: %v", err)
	}
	if stored.Classification != store.ClassificationSelf {
		t.Errorf("stored classification = %q, want %q", stored.Classification, store.ClassificationSelf)
	}
	if stored.AuthorUserID != botUserID {
		t.Errorf("stored author user id = %d, want %d", stored.AuthorUserID, botUserID)
	}
	if stored.Body != own.Body {
		t.Error("stored body was not the body as posted")
	}
}

// TestQuotedMarkerIsNotLitefactorysOwnComment puts AC-S0.1 and AC-S0.2 side by
// side, which is the only way either of them means anything: the two bodies
// below contain the same marker text, and exactly one of them is litefactory's.
//
// The owner's quote-reply used to be classified self and thrown away, so
// answering a litefactory question worked or did not depending on which GitHub
// button was pressed. It starts a run now, and the comment the quote came from
// is still dropped.
func TestQuotedMarkerIsNotLitefactorysOwnComment(t *testing.T) {
	t.Parallel()

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	posted := statusMarker + "\n\n**Run 1** finished in 42s.\n\nBranch `lf/7-flaky-test` pushed."

	// What GitHub's "quote reply" button produces: every line of the quoted
	// comment prefixed with "> ", then the reader's own text underneath.
	quoted := "> " + strings.ReplaceAll(posted, "\n", "\n> ") +
		"\n\nnow do the same for `litefactory logs`"

	reply, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:        5200,
		Body:      quoted,
		Author:    github.User{ID: ownerUserID, Login: ownerHandle},
		CreatedAt: time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("admit the quote-reply: %v", err)
	}
	if !reply.Triggers() {
		t.Fatalf("the owner's quote-reply was classified %q with reason %q; it must start a run (AC-S0.1)",
			reply.Verdict.Classification, reply.Verdict.SelfReason)
	}
	if reply.Verdict.Classification != store.ClassificationInstruction {
		t.Errorf("classification = %q, want %q", reply.Verdict.Classification, store.ClassificationInstruction)
	}

	// AC-S0.2, in the same test and against the same marker: the comment that
	// quote came from is still litefactory's own and is still dropped.
	own, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:        5201,
		Body:      posted,
		Author:    github.User{ID: botUserID, Login: botHandle, IsBot: true},
		CreatedAt: time.Date(2026, 8, 30, 9, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("admit litefactory's own comment: %v", err)
	}
	if own.Triggers() {
		t.Fatal("litefactory's own report started a run; this is the loop AC-2.14 exists to prevent")
	}
	if own.Verdict.Classification != store.ClassificationSelf {
		t.Errorf("classification = %q, want %q", own.Verdict.Classification, store.ClassificationSelf)
	}

	// And the same body under an account the identities table does not know is
	// still caught, because the marker starts a line of it. Line-anchoring
	// narrowed the belt-and-braces condition; it did not remove it.
	stray, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:        5202,
		Body:      posted,
		Author:    github.User{ID: strangerUserID, Login: strangerHandle},
		CreatedAt: time.Date(2026, 8, 30, 9, 10, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("admit a stray copy of the report: %v", err)
	}
	if stray.Verdict.SelfReason != SelfReasonMarker {
		t.Errorf("self reason = %q, want %q", stray.Verdict.SelfReason, SelfReasonMarker)
	}
}

// TestSelfDropIsLogged is AC-S0.4. A comment classified self is thrown away
// without a run, a reply or a reaction, so from the thread it is
// indistinguishable from a daemon that is not running — the log line is the only
// trace a human can follow, and it has to name which condition fired.
//
// The level matters as much as the line. It is asserted against a handler at
// slog.LevelInfo, which is what `litefactory` runs at without log_level being
// set: a line only visible under debug is not a line the owner can find.
func TestSelfDropIsLogged(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), log)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	// The marker case: the only self reason that can fire on a comment a human
	// really wrote, and so the only one whose log line is ever the answer to
	// "why did nothing happen when I commented?".
	if _, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:     5400,
		Body:   statusMarker + "\n\nRun 1 succeeded.",
		Author: github.User{ID: strangerUserID, Login: strangerHandle},
	}); err != nil {
		t.Fatalf("admit: %v", err)
	}

	logged := logs.String()
	for _, want := range []string{
		`"level":"INFO"`,
		`"self_reason":"marker"`,
		`"comment_id":5400`,
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("the drop log line does not contain %s:\n%s", want, logged)
		}
	}
	if got := strings.Count(logged, dropMessage); got != 1 {
		t.Errorf("logged %d drop lines for one dropped comment, want 1:\n%s", got, logged)
	}

	// An instruction is not a drop and must not produce one, or the line stops
	// being worth grepping for.
	logs.Reset()
	if _, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:     5401,
		Body:   "/spike look at the retry path",
		Author: github.User{ID: ownerUserID, Login: ownerHandle},
	}); err != nil {
		t.Fatalf("admit the instruction: %v", err)
	}
	if strings.Contains(logs.String(), dropMessage) {
		t.Errorf("an owner instruction logged a drop:\n%s", logs.String())
	}
}

// dropMessage is the message [Filter.Admit] logs a self-classified comment
// with. It is duplicated here rather than exported: the message is the thing a
// human greps daemon.log for, so it is worth a test that fails when it is
// reworded silently.
const dropMessage = "turn: dropped a comment as litefactory's own"

func TestFilterAdmitRecordsOwnerInstruction(t *testing.T) {
	t.Parallel()

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	// The owner, renamed since `init` ran. The stored handle no longer
	// matches; the id still does.
	admitted, err := filter.Admit(t.Context(), session.ID, github.Comment{
		ID:        5151,
		Body:      "/spike make the login page load faster",
		Author:    github.User{ID: ownerUserID, Login: renamedOwnerName},
		CreatedAt: time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !admitted.Triggers() {
		t.Fatal("Admit() did not trigger for an owner instruction")
	}
	if admitted.Comment.Tag != "" {
		t.Errorf("tag = %q, want none: instructions are not tagged", admitted.Comment.Tag)
	}
	if admitted.Comment.AuthorHandle != renamedOwnerName {
		t.Errorf("stored handle = %q, want the handle as it arrived (%q)", admitted.Comment.AuthorHandle, renamedOwnerName)
	}
}

// TestFilterAdmitTagsThirdPartyComments covers AC-2.15: a third party is
// recorded with the next cN tag and triggers nothing. The tags must be
// sequential across the session and must not be spent on comments of any other
// classification.
func TestFilterAdmitTagsThirdPartyComments(t *testing.T) {
	t.Parallel()

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	incoming := []struct {
		comment github.Comment
		wantTag string
	}{
		{
			comment: github.Comment{
				ID:     6001,
				Body:   "the retry path looks suspicious to me",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			wantTag: "c1",
		},
		{
			comment: github.Comment{
				ID:     6002,
				Body:   "/spike look at the retry path",
				Author: github.User{ID: ownerUserID, Login: ownerHandle},
			},
			wantTag: "",
		},
		{
			comment: github.Comment{
				ID:     6003,
				Body:   statusMarker + "\n\nrun finished",
				Author: github.User{ID: botUserID, Login: botHandle, IsBot: true},
			},
			wantTag: "",
		},
		{
			comment: github.Comment{
				ID:     6004,
				Body:   "and the backoff too",
				Author: github.User{ID: impostorUserID, Login: "someone-else"},
			},
			wantTag: "c2",
		},
		{
			comment: github.Comment{
				ID:     6005,
				Body:   "one more thought",
				Author: github.User{ID: strangerUserID, Login: strangerHandle},
			},
			wantTag: "c3",
		},
	}

	for _, in := range incoming {
		admitted, err := filter.Admit(t.Context(), session.ID, in.comment)
		if err != nil {
			t.Fatalf("admit %d: %v", in.comment.ID, err)
		}
		if admitted.Comment.Tag != in.wantTag {
			t.Errorf("comment %d tag = %q, want %q", in.comment.ID, admitted.Comment.Tag, in.wantTag)
		}
		if in.wantTag != "" {
			if admitted.Verdict.Classification != store.ClassificationThirdParty {
				t.Errorf("comment %d classification = %q, want %q",
					in.comment.ID, admitted.Verdict.Classification, store.ClassificationThirdParty)
			}
			if admitted.Triggers() {
				t.Errorf("comment %d from a third party triggered a run", in.comment.ID)
			}
		}
	}

	// Re-seeing a comment is routine (D-A: `since` is inclusive) and must not
	// spend another tag.
	replayed, err := filter.Admit(t.Context(), session.ID, incoming[0].comment)
	if err != nil {
		t.Fatalf("replay admit: %v", err)
	}
	if replayed.Comment.Tag != "c1" {
		t.Errorf("replayed tag = %q, want c1", replayed.Comment.Tag)
	}
	next, err := db.NextCommentTag(t.Context(), session.ID)
	if err != nil {
		t.Fatalf("next comment tag: %v", err)
	}
	if next != "c4" {
		t.Errorf("next tag = %q, want c4", next)
	}
}

// TestFilterAdmitRejectsNonComments guards AC-2.16: only comments are ever
// acted on. An issue body has no comment id, and there is no way to feed one
// in as if it were a comment.
func TestFilterAdmitRejectsNonComments(t *testing.T) {
	t.Parallel()

	db, session := newCommentStore(t)
	filter, err := NewFilter(db, classifyIdentities(), nil)
	if err != nil {
		t.Fatalf("new filter: %v", err)
	}

	issueBody := github.Comment{
		Body:   "/spike make the login page load faster",
		Author: github.User{ID: ownerUserID, Login: ownerHandle},
	}
	if _, err := filter.Admit(t.Context(), session.ID, issueBody); err == nil {
		t.Fatal("Admit() accepted a comment with no forge id; an issue body must never be admissible")
	}

	if _, err := filter.Admit(t.Context(), 0, github.Comment{
		ID:     6100,
		Body:   "hello",
		Author: github.User{ID: ownerUserID, Login: ownerHandle},
	}); err == nil {
		t.Fatal("Admit() accepted a zero session id")
	}
}

func TestNewFilterRejectsUnusableIdentities(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		identities Identities
	}{
		{
			name:       "no owner id",
			identities: Identities{OwnerHandle: ownerHandle, BotUserID: botUserID, BotHandle: botHandle},
		},
		{
			name:       "no bot id",
			identities: Identities{OwnerUserID: ownerUserID, OwnerHandle: ownerHandle, BotHandle: botHandle},
		},
		{
			name: "owner and bot are the same account",
			identities: Identities{
				OwnerUserID: ownerUserID, OwnerHandle: ownerHandle,
				BotUserID: ownerUserID, BotHandle: botHandle,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			filter, err := NewFilter(&stubCommentRecorder{}, test.identities, nil)
			if err == nil {
				t.Fatal("NewFilter() accepted identities it cannot safely classify with")
			}
			if filter != nil {
				t.Error("NewFilter() returned a filter alongside an error")
			}
		})
	}

	if _, err := NewFilter(nil, classifyIdentities(), nil); err == nil {
		t.Error("NewFilter() accepted a nil comment recorder")
	}
}

func TestLoadIdentities(t *testing.T) {
	t.Parallel()

	t.Run("both identities", func(t *testing.T) {
		t.Parallel()

		db, _ := newCommentStore(t)
		upsertIdentity(t, db, store.RoleOwner, ownerHandle, ownerUserID)
		upsertIdentity(t, db, store.RoleBot, botHandle, botUserID)

		identities, err := LoadIdentities(t.Context(), db, store.ForgeGitHub)
		if err != nil {
			t.Fatalf("load identities: %v", err)
		}
		want := classifyIdentities()
		if identities != want {
			t.Errorf("LoadIdentities() = %+v, want %+v", identities, want)
		}
	})

	t.Run("no bot identity yet", func(t *testing.T) {
		t.Parallel()

		db, _ := newCommentStore(t)
		upsertIdentity(t, db, store.RoleOwner, ownerHandle, ownerUserID)

		_, err := LoadIdentities(t.Context(), db, store.ForgeGitHub)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("LoadIdentities() error = %v, want one wrapping store.ErrNotFound", err)
		}
	})

	t.Run("owner and bot are the same account", func(t *testing.T) {
		t.Parallel()

		db, _ := newCommentStore(t)
		upsertIdentity(t, db, store.RoleOwner, ownerHandle, ownerUserID)
		upsertIdentity(t, db, store.RoleBot, botHandle, ownerUserID)

		_, err := LoadIdentities(t.Context(), db, store.ForgeGitHub)
		if err == nil {
			t.Fatal("LoadIdentities() accepted an owner and bot sharing one account")
		}
		if !strings.Contains(err.Error(), "same account") {
			t.Errorf("error = %v, want it to name the collision", err)
		}
	})
}

// newCommentStore opens a migrated database with one registered repo and one
// session, which is the least a comment can be recorded against.
func newCommentStore(t *testing.T) (*store.DB, store.Session) {
	t.Helper()

	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "litefactory.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	root := t.TempDir()
	repo, err := db.UpsertRepo(t.Context(), store.Repo{
		Path:          filepath.Join(root, "api"),
		Forge:         store.ForgeGitHub,
		Owner:         "acme",
		Name:          "api",
		RemoteURL:     "git@github.com:acme/api.git",
		BaseBranch:    "main",
		FeaturePrefix: "lf",
		WorktreeRoot:  filepath.Join(root, ".lf-worktrees"),
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	session, err := db.GetOrCreateSession(t.Context(), repo, 7)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	return db, session
}

func upsertIdentity(t *testing.T, db *store.DB, role store.IdentityRole, handle string, userID int64) {
	t.Helper()

	if _, err := db.UpsertIdentity(t.Context(), store.Identity{
		Forge:  store.ForgeGitHub,
		Role:   role,
		Handle: handle,
		UserID: userID,
		Source: store.SourceForgeAPI,
	}); err != nil {
		t.Fatalf("upsert %s identity: %v", role, err)
	}
}
