package discord_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/discord"
)

func TestOrdinaryIntakeFiltersAndCapability(t *testing.T) {
	flags := uint64(1 << 18)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/applications/@me" {
			fmt.Fprintf(w, `{"flags":%d}`, flags)
			return
		}
		fmt.Fprint(w, `[
   {"id":"9","channel_id":"2","content":"owner","timestamp":"2026-10-11T12:00:00Z","author":{"id":"3"},"message_reference":{"channel_id":"2","message_id":"7"}},
   {"id":"8","channel_id":"2","content":"private outsider","author":{"id":"4"}},
   {"id":"7","channel_id":"2","content":"bot","author":{"id":"3","bot":true}},
   {"id":"6","channel_id":"2","content":"webhook","author":{"id":"3"},"webhook_id":"1"},
   {"id":"5","channel_id":"2","content":"system","author":{"id":"3"},"type":6},
   {"id":"4","channel_id":"99","content":"thread","author":{"id":"3"}},
   {"id":"3","channel_id":"2","content":"forwarded","author":{"id":"3"},"message_snapshots":[{}]}
  ]`)
	}))
	defer api.Close()
	a := discord.New(discord.Options{APIURL: api.URL, Client: api.Client()})
	if err := a.CheckIntakeCapability(t.Context(), "2"); err != nil {
		t.Fatal(err)
	}
	page, err := a.ReadIntakePage(t.Context(), "2", "3", "")
	if err != nil || len(page.Messages) != 1 || page.Count != 7 || page.HighID != "9" || page.LowID != "3" || page.Messages[0].ReplyTo != "7" {
		t.Fatalf("page %+v error %v", page, err)
	}
	flags = 0
	if a.CheckIntakeCapability(t.Context(), "2") == nil {
		t.Error("missing content capability accepted")
	}
}
func TestOrdinaryIntakeRateLimit(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"retry_after":12}`)
	}))
	defer api.Close()
	a := discord.New(discord.Options{APIURL: api.URL, Client: api.Client()})
	_, err := a.ReadIntakePage(t.Context(), "2", "3", "")
	var failure *discord.IntakeFailure
	if !errors.As(err, &failure) || failure.RetryAfter != 12*time.Second {
		t.Fatalf("retry error %v", err)
	}
}
