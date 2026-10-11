package discord

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/messaging"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNeutralArguments(t *testing.T) {
	got, err := arguments([]interactionOption{{Name: "project", Type: 3, Value: json.RawMessage(`"owner/repo"`)}, {Name: "page", Type: 4, Value: json.RawMessage(`2`)}, {Name: "details", Type: 5, Value: json.RawMessage(`true`)}})
	if err != nil || got["project"] != "owner/repo" || got["page"] != "2" || got["details"] != "true" {
		t.Fatalf("arguments = %v, %v", got, err)
	}
	for _, options := range [][]interactionOption{
		{{Name: "page", Type: 4, Value: json.RawMessage(`1.5`)}},
		{{Name: "page", Type: 4, Value: json.RawMessage(`null`)}},
		{{Name: "details", Type: 5, Value: json.RawMessage(`"true"`)}},
		{{Name: "x", Type: 3, Value: json.RawMessage(`"a"`)}, {Name: "x", Type: 3, Value: json.RawMessage(`"b"`)}},
	} {
		if _, err := arguments(options); err == nil {
			t.Fatalf("accepted invalid options %v", options)
		}
	}
}

func TestCommandOptionDeclarations(t *testing.T) {
	var got []command
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	adapter := New(Options{ApplicationID: "1", APIURL: api.URL, Client: api.Client()})
	def := messaging.CommandDefinition{Name: "meter", Description: "Read usage", Arguments: []messaging.Argument{
		{Name: "project", Type: messaging.TextArgument, Description: "Project"},
		{Name: "page", Type: messaging.IntegerArgument, Description: "Page", Required: true},
		{Name: "details", Type: messaging.BooleanArgument, Description: "Details"},
	}}
	if err := adapter.Register(t.Context(), def); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != "meter" || len(got[0].Options) != 3 {
		t.Fatalf("commands = %+v", got)
	}
	for i, kind := range []int{3, 4, 5} {
		if got[0].Options[i].Type != kind || got[0].Options[i].Name != def.Arguments[i].Name || got[0].Options[i].Description != def.Arguments[i].Description || got[0].Options[i].Required != def.Arguments[i].Required {
			t.Fatalf("option = %+v", got[0].Options[i])
		}
	}
}

func TestDeliveryPreservesPageFooter(t *testing.T) {
	var delivered string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload outgoing
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		delivered = payload.Content
		w.WriteHeader(http.StatusOK)
	}))
	defer api.Close()
	adapter := New(Options{ApplicationID: "1", APIURL: api.URL, Client: api.Client()})
	for _, content := range []string{
		strings.Repeat("x", 1950) + "\npage 1 of 2, as of 2026-10-10T12:00:00Z",
		strings.Repeat("😀", 975) + "\npage 1 of 2, as of 2026-10-10T12:00:00Z",
	} {
		result := adapter.Deliver(t.Context(), messaging.Target{Transport: "discord", MessageID: "1", Channel: "2"}, content)
		if !result.Confirmed || delivered != content {
			t.Fatalf("delivery confirmed=%t; delivered %d bytes, want %d", result.Confirmed, len(delivered), len(content))
		}
	}
	result := adapter.Deliver(t.Context(), messaging.Target{Transport: "discord", MessageID: "2", Channel: "2"}, strings.Repeat("x", 2001))
	if !result.Confirmed || delivered != "report unavailable" {
		t.Fatalf("oversize delivery = %+v, %q", result, delivered)
	}
}
