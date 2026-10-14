package messaging

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/presentation"
	"github.com/cloudvane/litefactory/internal/store"
)

type detailsUnitSender struct {
	result   Delivery
	payloads []presentation.Payload
	texts    []string
	targets  []Target
}

func (s *detailsUnitSender) Deliver(_ context.Context, target Target, text string) Delivery {
	s.texts = append(s.texts, text)
	s.targets = append(s.targets, target)
	return s.result
}
func (s *detailsUnitSender) DeliverReply(_ context.Context, _ Target, payload presentation.Payload) Delivery {
	s.payloads = append(s.payloads, payload)
	return s.result
}

func detailsUnitAnswer(t *testing.T, results []conversationReadResult) (*factoryConversationRun, store.ConversationAnswer, time.Time) {
	t.Helper()
	run, at := factoryUnitRun(t, &factoryUnitProvider{fail: true})
	if err := run.s.db.SaveConversationSession(t.Context(), run.t, "unit-session"); err != nil {
		t.Fatal(err)
	}
	for i, result := range results {
		result.FactID = fmt.Sprintf("%s-fact-%d", run.t.ID, i+1)
		result.CapturedAt = at
		results[i] = result
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if err = run.s.db.SaveConversationExchange(t.Context(), run.t, store.ConversationExchange{TurnID: run.t.ID, Ordinal: i + 1, Kind: "evidence", Result: string(raw), CapturedAt: at, Complete: result.Complete}); err != nil {
			t.Fatal(err)
		}
	}
	if err := run.s.saveConversationPresentation(t.Context(), run.t, "completed", []presentation.AnswerBlock{{Text: "An explanation."}}, "", at); err != nil {
		t.Fatal(err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	if err = run.s.db.FinishConversationReply(t.Context(), reply, "answer-message", at); err != nil {
		t.Fatal(err)
	}
	answer, err := run.s.db.ReadConversationAnswer(t.Context(), "discord", "1", "2", "")
	if err != nil {
		t.Fatal(err)
	}
	return run, answer, at
}

func detailsUnitFreeze(t *testing.T, run *factoryConversationRun, answer store.ConversationAnswer, at time.Time) store.ChatReply {
	t.Helper()
	if _, inserted, err := run.s.Admit(t.Context(), Message{Transport: "discord", ID: "details-request", Channel: "2", Sender: "1", Command: "details", Arguments: map[string]string{"answer": answer.ID}}, at); err != nil || !inserted {
		t.Fatalf("admit details: %t %v", inserted, err)
	}
	if more, err := run.s.Step(t.Context(), at); err != nil || !more {
		t.Fatalf("freeze details: %t %v", more, err)
	}
	reply, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at)
	if err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestConversationDetailsPartitionRetryAndInlineFallback(t *testing.T) {
	results := make([]conversationReadResult, 3)
	for i := range results {
		results[i] = conversationReadResult{Command: "status", Arguments: map[string]string{}, Source: "factory:status", Target: "factory", Status: "success", Result: strings.Repeat("\"\n", 8192), Complete: true, Gaps: []string{}}
	}
	run, answer, at := detailsUnitAnswer(t, results)
	sent := &detailsUnitSender{}
	run.s.sender = sent
	head := detailsUnitFreeze(t, run, answer, at)
	saved, err := run.s.db.ReadConversationDetailsParts(t.Context(), head)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) < 2 {
		t.Fatalf("large export not partitioned: %d parts", len(saved))
	}
	var document, completeExport strings.Builder
	for i, part := range saved {
		if len(part.Attachment) > 65536 || !strings.Contains(part.Attachment, fmt.Sprintf("export part %d of %d", i+1, len(saved))) {
			t.Fatalf("invalid bounded manifest: %+v", part)
		}
		_, segment, found := strings.Cut(part.Attachment, "\n\n")
		if !found {
			t.Fatal("manifest missing")
		}
		document.WriteString(segment)
		completeExport.WriteString(part.Attachment)
	}
	if document.Len() <= 65536 {
		t.Fatalf("fixture must exceed attachment bound: %d", document.Len())
	}
	var decoded struct {
		AnswerID string                   `json:"answer_id"`
		Results  []conversationReadResult `json:"results"`
	}
	if err = json.Unmarshal([]byte(document.String()), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.AnswerID != answer.ID || !reflect.DeepEqual(decoded.Results, results) {
		t.Fatal("partitioned export changed saved results")
	}
	// An ambiguous attempt retains the exact attachment and part identity.
	if err = run.s.deliverConversationDetails(t.Context(), head, at); err != nil {
		t.Fatal(err)
	}
	retry, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if retry.MessageID != head.MessageID || retry.ReplyText != head.ReplyText || retry.ReplyAttachment != head.ReplyAttachment {
		t.Fatal("uncertain send regenerated export bytes")
	}
	sent.result = Delivery{AttachmentRejected: true}
	if err = run.s.deliverConversationDetails(t.Context(), retry, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sent.payloads) != 2 || !reflect.DeepEqual(sent.payloads[0], sent.payloads[1]) {
		t.Fatal("uncertain retry did not send identical payload")
	}
	inline, err := run.s.db.ClaimConversationReply(t.Context(), "discord", at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := run.s.db.ReadConversationDetailsParts(t.Context(), inline)
	if err != nil {
		t.Fatal(err)
	}
	escaped := regexp.MustCompile("\\\\([\\\\*_~`|>#\\[\\]@])")
	var reconstructed strings.Builder
	for _, part := range frozen {
		if part.Attachment != "" {
			t.Fatal("definite rejection left attachment parts unfrozen")
		}
		fragment := part.Text
		reconstructed.WriteString(escaped.ReplaceAllString(fragment, "$1"))
	}
	if reconstructed.String() != completeExport.String() {
		t.Fatalf("inline fallback lost export bytes: %d != %d", reconstructed.Len(), completeExport.Len())
	}
	sent.result = Delivery{Confirmed: true}
	if err = run.s.deliverConversationDetails(t.Context(), inline, at.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for range len(frozen) {
		more, e := run.s.ConversationDeliveryStep(t.Context(), at.Add(time.Second))
		if e != nil {
			t.Fatal(e)
		}
		if !more {
			break
		}
	}
	for i, target := range sent.targets {
		want := ""
		if i == 0 {
			want = "details-request"
		}
		if target.Handle != want {
			t.Fatalf("part %d interaction handle %q != %q", i+1, target.Handle, want)
		}
	}
	if len(sent.texts) != len(frozen) {
		t.Fatalf("inline delivery incomplete: %d of %d", len(sent.texts), len(frozen))
	}
}

func TestConversationDetailsAccessRecheckedAfterFreeze(t *testing.T) {
	run, answer, at := detailsUnitAnswer(t, []conversationReadResult{{Command: "status", Arguments: map[string]string{}, Source: "factory:status", Target: "factory", Status: "success", Result: "Owner-visible result", Complete: true, Gaps: []string{}}})
	sent := &detailsUnitSender{result: Delivery{Confirmed: true}}
	run.s.sender = sent
	head := detailsUnitFreeze(t, run, answer, at)
	run.s.conversations.Load().check = func(context.Context, string) error { return &ConversationAccessError{Permanent: true} }
	if err := run.s.deliverConversationDetails(t.Context(), head, at); err != nil {
		t.Fatal(err)
	}
	if len(sent.payloads) != 0 || len(sent.texts) != 0 {
		t.Fatal("access loss disclosed frozen data")
	}
	retained, err := run.s.db.ReadChatReply(t.Context(), "discord", head.MessageID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.State != "pending" || retained.ReplyAttachment != head.ReplyAttachment {
		t.Fatalf("access loss failed to retain export: %+v", retained)
	}
	conversation, err := run.s.db.ReadConversation(t.Context(), "discord", "2")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conversation.HealthReason, "access lost") {
		t.Fatalf("access loss has no health reason: %+v", conversation)
	}
	if more, e := run.s.ConversationDeliveryStep(t.Context(), at.Add(time.Minute)); e != nil || more {
		t.Fatalf("paused export was reclaimed: %t %v", more, e)
	}
}

func TestConversationDetailsEmptyAndExpiredNeverSubstitute(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%t", expired), func(t *testing.T) {
			var results []conversationReadResult
			if expired {
				results = []conversationReadResult{{Command: "status", Arguments: map[string]string{}, Source: "factory:status", Target: "factory", Result: "Must not be exported", Complete: true, Gaps: []string{}}}
			}
			run, answer, at := detailsUnitAnswer(t, results)
			if expired {
				at = at.Add(24 * time.Hour)
			}
			reply := detailsUnitFreeze(t, run, answer, at)
			want := "No tool results were obtained for this answer."
			if expired {
				want = "Details for this answer have expired."
			}
			if reply.ReplyText != want || reply.ReplyAttachment != "" {
				t.Fatalf("details substituted content: %+v", reply)
			}
		})
	}
}
