package turn

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// ticketReplyRecorder is the neutral reply ledger capability at its consumer.
// It verifies the session's durable ticket binding in the insert transaction.
type ticketReplyRecorder interface {
	RecordTicketReply(context.Context, int64, ticket.Reply, store.Classification) (store.Comment, error)
}

func (f *Filter) actorIdentity(id int64) ticket.Identity {
	origin := f.identities.TicketOrigin
	if origin == "" {
		origin = "https://api.github.com"
	}
	remoteID := ""
	if id > 0 {
		remoteID = strconv.FormatInt(id, 10)
	}
	return ticket.Identity{Provider: ticket.GitHub, Origin: origin, RemoteID: remoteID}
}

// ClassifyReply preserves self-filter ordering while comparing qualified
// immutable identities. Matching handles or remote IDs on another origin
// cannot authorize an instruction or impersonate the configured bot account.
func (f *Filter) ClassifyReply(reply ticket.Reply) Verdict {
	reason := SelfReasonNone
	switch {
	case reply.Author.Identity.Equal(f.actorIdentity(f.identities.BotUserID)):
		reason = SelfReasonBotUserID
	case reply.Author.IsBot:
		reason = SelfReasonForgeBot
	case HasMarker(reply.Body):
		reason = SelfReasonMarker
	}
	if reason != SelfReasonNone {
		return Verdict{Classification: store.ClassificationSelf, SelfReason: reason}
	}
	if reply.Author.Identity.Equal(f.actorIdentity(f.identities.OwnerUserID)) {
		return Verdict{Classification: store.ClassificationInstruction}
	}
	return Verdict{Classification: store.ClassificationThirdParty}
}

// AdmitReply requires a resolved ticket and a qualified ledger. It never falls
// back to unqualified recording when provenance or a durable binding is absent.
func (f *Filter) AdmitReply(ctx context.Context, sessionID int64, reply ticket.Reply) (Admission, error) {
	if sessionID < 1 {
		return Admission{}, errors.New("turn: reply session is required")
	}
	if err := reply.Ticket.Validate(); err != nil {
		return Admission{}, err
	}
	if err := reply.Identity.Validate(); err != nil {
		return Admission{}, err
	}
	recorder, ok := f.comments.(ticketReplyRecorder)
	if !ok {
		return Admission{}, errors.New("turn: qualified reply ledger is required")
	}
	verdict := f.ClassifyReply(reply)
	recorded, err := recorder.RecordTicketReply(ctx, sessionID, reply, verdict.Classification)
	if err != nil {
		return Admission{}, fmt.Errorf("turn: admit qualified reply: %w", err)
	}
	f.logger().DebugContext(ctx, "turn: classified qualified reply", "session_id", sessionID, "reply_id", reply.Identity.RemoteID, "provider", string(reply.Identity.Provider), "origin", string(reply.Identity.Origin), "classification", string(verdict.Classification))
	return Admission{Verdict: verdict, Comment: recorded}, nil
}
