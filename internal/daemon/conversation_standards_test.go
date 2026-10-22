package daemon

import (
	"context"
	"testing"

	"github.com/cloudvane/litefactory/internal/messaging"
)

func TestConversationStandardsAuthorityFailsBeforeDomainAccess(t *testing.T) {
	adapter := &ConversationStandards{OwnerID: "2", Authorized: func(context.Context, messaging.StandardsRequest) error { return nil }}
	valid := messaging.StandardsRequest{Operation: "book.current", Transport: "discord", OwnerID: "2", ChannelID: "3", MessageID: "4"}
	for name, change := range map[string]func(*messaging.StandardsRequest){
		"wrong owner":     func(r *messaging.StandardsRequest) { r.OwnerID = "5" },
		"missing channel": func(r *messaging.StandardsRequest) { r.ChannelID = "" },
		"missing source":  func(r *messaging.StandardsRequest) { r.MessageID = "" },
		"other transport": func(r *messaging.StandardsRequest) { r.Transport = "webhook" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			change(&request)
			if _, err := adapter.Read(t.Context(), request); err == nil {
				t.Fatal("unauthenticated read reached domain")
			}
			if _, err := adapter.Execute(t.Context(), request); err == nil {
				t.Fatal("unauthenticated write reached domain")
			}
		})
	}
	adapter.Authorized = nil
	if _, err := adapter.Read(t.Context(), valid); err == nil {
		t.Fatal("missing host authorization reached domain")
	}
}
func TestConversationStandardsRejectsTrailingJSON(t *testing.T) {
	var target struct {
		Name string `json:"name"`
	}
	for _, raw := range []string{`{"name":"a"} {"name":"b"}`, `{"name":"a","extra":1}`, `{"name":"a"} false`} {
		if err := standardsJSON(raw, &target); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	if err := standardsJSON(`{"name":"a"}`, &target); err != nil {
		t.Fatal(err)
	}
}

func TestConversationStandardsRequiresCompleteScheduleTerms(t *testing.T) {
	args := map[string]string{"name": "weekly logs", "kind": "logs", "repository_id": "1", "interval_seconds": "604800", "run_limit": "3"}
	for _, missing := range []string{"name", "kind", "repository_id", "interval_seconds", "run_limit"} {
		t.Run(missing, func(t *testing.T) {
			terms := make(map[string]string)
			for key, value := range args {
				if key != missing {
					terms[key] = value
				}
			}
			if _, err := standardsScheduleConfig(terms); err == nil {
				t.Fatal("incomplete schedule accepted")
			}
		})
	}
	c, err := standardsScheduleConfig(args)
	if err != nil {
		t.Fatal(err)
	}
	if c.RunLimit != 3 || c.IntervalSeconds != 604800 || c.RepositoryID != 1 {
		t.Fatalf("terms changed: %+v", c)
	}
}
