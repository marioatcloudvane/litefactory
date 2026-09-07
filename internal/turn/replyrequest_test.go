package turn

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
)

// testReplyRef is the task every ReplyRequest fixture in this file answers a
// comment on. It is a plain literal rather than a fixture: the functions under
// test here are pure, and none of them touch git or the database.
var testReplyRef = naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}

// TestReplyRequestKindDispatchesSyncRefusalThroughRefusal is AC-4.2's dispatch
// half, confirmed against the real code rather than the sub-issue's own
// (slightly incorrect) description of it: [report.ReplyRefusesSync] is
// delivered through [ReplyRequest.Refusal], and [ReplyRequest.kind] returns it
// directly before ever reaching [replyKind] — the same path
// [report.ReplyAgentSwitchRefused] already uses, which also has no
// [replyKind] case of its own.
func TestReplyRequestKindDispatchesSyncRefusalThroughRefusal(t *testing.T) {
	t.Parallel()

	req := ReplyRequest{
		Ref:           testReplyRef,
		SessionID:     1,
		CommentID:     42,
		Refusal:       report.ReplyRefusesSync,
		ConflictPaths: []string{"README.md"},
	}

	if !req.refusing() {
		t.Fatal("refusing() = false for a ReplyRequest carrying a sync refusal")
	}
	kind, ok := req.kind()
	if !ok {
		t.Fatal("kind() reported no reply for a sync refusal, want it to draw one")
	}
	if kind != report.ReplyRefusesSync {
		t.Errorf("kind() = %v, want %v", kind, report.ReplyRefusesSync)
	}
	if err := req.validate(); err != nil {
		t.Errorf("validate() = %v, want a sync refusal accepted as a valid refusal shape", err)
	}
}

// TestReplyRequestValidateRefusesASyncRefusalMissingRequiredFields covers the
// ordinary [ReplyRequest] preconditions applied to a sync refusal specifically
// — the same session id and comment id every other reply needs, whatever its
// Kind.
func TestReplyRequestValidateRefusesASyncRefusalMissingRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  ReplyRequest
	}{
		{
			name: "no session id",
			req: ReplyRequest{
				Ref: testReplyRef, CommentID: 42,
				Refusal: report.ReplyRefusesSync, ConflictPaths: []string{"README.md"},
			},
		},
		{
			name: "no comment id",
			req: ReplyRequest{
				Ref: testReplyRef, SessionID: 1,
				Refusal: report.ReplyRefusesSync, ConflictPaths: []string{"README.md"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.req.validate(); err == nil {
				t.Error("validate() = nil error, want a refusal")
			}
		})
	}
}

// TestReplyRequestKindOnlyConsultsReplyKindForACommand is the complementary
// half of TestReplyRequestKindDispatchesSyncRefusalThroughRefusal: a
// ReplyRequest that is *not* refusing anything still resolves its kind
// through the CommandKind mapping, confirming the two paths through
// [ReplyRequest.kind] stay disjoint.
func TestReplyRequestKindOnlyConsultsReplyKindForACommand(t *testing.T) {
	t.Parallel()

	req := ReplyRequest{
		Ref:       testReplyRef,
		SessionID: 1,
		CommentID: 42,
		Command:   ParseCommand("/frobnicate"),
	}

	if req.refusing() {
		t.Fatal("refusing() = true for a ReplyRequest with no Refusal set")
	}
	kind, ok := req.kind()
	if !ok {
		t.Fatal("kind() reported no reply for an unknown command, want ReplyUnknownCommand")
	}
	if kind != report.ReplyUnknownCommand {
		t.Errorf("kind() = %v, want %v", kind, report.ReplyUnknownCommand)
	}
}
