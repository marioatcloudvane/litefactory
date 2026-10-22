package messaging

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/cloudvane/litefactory/internal/store"
)

type standardsDeliverySource struct {
	notices []StandardsNotice
	reads   int
}

func (f *standardsDeliverySource) Notices(_ context.Context, r StandardsRequest) ([]StandardsNotice, error) {
	f.reads++
	var after int64
	_, _ = fmt.Sscan(r.Arguments["after"], &after)
	var out []StandardsNotice
	for _, n := range f.notices {
		if n.ID > after {
			out = append(out, n)
		}
	}
	return out, nil
}
func (*standardsDeliverySource) Read(context.Context, StandardsRequest) (StandardsResult, error) {
	panic("notice delivery called domain read")
}
func (*standardsDeliverySource) Execute(context.Context, StandardsRequest) (StandardsResult, error) {
	panic("notice delivery mutated standards")
}
func (*standardsDeliverySource) Review(context.Context, StandardsRequest) (StandardsReview, error) {
	panic("notice delivery requested review")
}

type standardsDeliverySender struct {
	targets []Target
	texts   []string
	result  Delivery
}

func (f *standardsDeliverySender) Deliver(_ context.Context, target Target, text string) Delivery {
	f.targets = append(f.targets, target)
	f.texts = append(f.texts, text)
	return f.result
}

func TestStandardsNoticeRetryRetainsExactPartsAndPausesPermanentRefusal(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "factory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	source := &standardsDeliverySource{notices: []StandardsNotice{{ID: 1, Payload: "Unapproved at origin; schedule=saved-schedule analysis=analysis-1 proposal=proposal-1 version=1 base=2 digest=exact-original\n" + strings.Repeat("界🙂", 900)}}}
	sender := &standardsDeliverySender{result: Delivery{RetryAfter: time.Minute}}
	service := New(db, "discord", "7", []string{"3"}, sender, FactoryStatus{DB: db})
	service.Protect = func(_ context.Context, text string) (string, error) { return text, nil }
	service.EnableStandards(source)
	service.EnableStandardsNotices("3")
	authorized := true
	service.conversations.Store(&conversationRuntime{bindings: map[string]ConversationBinding{"3": {Provider: "claude"}}, check: func(context.Context, string) error {
		if !authorized {
			return &ConversationAccessError{Permanent: true}
		}
		return nil
	}, accessAfter: map[string]time.Time{}})
	at := time.Now().UTC().Add(48 * time.Hour)
	worked, err := service.StandardsNoticeStep(ctx, at)
	if err != nil || !worked {
		t.Fatalf("first attempt: %t %v", worked, err)
	}
	if len(sender.texts) != 1 {
		t.Fatal("worker attempted more than one notice part")
	}
	initial := sender.texts[0]
	target := sender.targets[0]
	if !target.Protected || !target.Notice || len(utf16.Encode([]rune(initial))) > 2000 {
		t.Fatalf("invalid protected notice part: %+v", target)
	}
	if _, err = service.StandardsNoticeStep(ctx, at.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 1 {
		t.Fatal("worker ignored Retry-After")
	}
	if source.reads != 1 {
		t.Fatal("worker expanded the notice backlog before draining its retained batch")
	}
	authorized = false
	if _, err = service.StandardsNoticeStep(ctx, at.Add(time.Minute)); err == nil {
		t.Fatal("delivery ignored removed authorization")
	}
	if len(sender.texts) != 1 {
		t.Fatal("removed authorization still sent a notice")
	}
	authorized = true
	sender.result = Delivery{Confirmed: true, PromptID: "remote-1"}
	if _, err = service.StandardsNoticeStep(ctx, at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 2 || sender.texts[1] != initial || sender.targets[1].MessageID != target.MessageID {
		t.Fatal("retry changed saved bytes or delivery identity")
	}
	sender.result = Delivery{Permanent: true}
	if _, err = service.StandardsNoticeStep(ctx, at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	retained, err := db.GetStandardsConversationNoticeDelivery(ctx, 1, "discord", "7", "3")
	if err != nil {
		t.Fatal(err)
	}
	if retained.Status != "paused" || retained.PartsAcknowledged != 1 || retained.Payload != source.notices[0].Payload {
		t.Fatalf("permanent refusal discarded progress or content: %+v", retained)
	}
	if !strings.Contains(service.standardsNoticeStatus(ctx), "paused") {
		t.Fatal("paused delivery is invisible")
	}
	service.EnableStandardsNotices("")
	if !strings.Contains(service.standardsNoticeStatus(ctx), "unconfigured") {
		t.Fatal("unconfigured destination is invisible")
	}
	if _, err = service.StandardsNoticeStep(ctx, at.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(sender.texts) != 3 {
		t.Fatal("unconfigured destination attempted delivery")
	}
}

func TestStandardsReviewPartsPreserveUnicodeAndExactContent(t *testing.T) {
	source := strings.Repeat("Complete before and after 界🙂\n", 5000)
	parts := standardsReviewParts(source)
	if strings.Join(parts, "") != source {
		t.Fatal("full review lost or changed content")
	}
	for _, part := range parts {
		if len(utf16.Encode([]rune(part))) > 2000 {
			t.Fatal("review part exceeds Discord UTF-16 limit")
		}
	}
}
