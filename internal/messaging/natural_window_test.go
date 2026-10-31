package messaging

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// TestAnalysisWindowAcceptsPlainLocalWindows pins the owner's local-time window
// forms, including clock times, against a fixed now of 31 Oct 21:19 in the
// factory's local zone.
func TestAnalysisWindowAcceptsPlainLocalWindows(t *testing.T) {
	zone := time.Local
	now := time.Date(2026, 10, 31, 21, 19, 0, 0, zone)
	local := func(day, hour, minute int) string {
		return time.Date(2026, 10, day, hour, minute, 0, 0, zone).UTC().Format(time.RFC3339)
	}
	until := now.UTC().Format(time.RFC3339)
	for _, tc := range []struct{ phrase, after, before string }{
		{"today", local(31, 0, 0), until},
		{"today only", local(31, 0, 0), until},
		{"last 6 hours", now.Add(-6 * time.Hour).UTC().Format(time.RFC3339), until},
		{"last 2 days", now.Add(-48 * time.Hour).UTC().Format(time.RFC3339), until},
		{"yesterday", local(30, 0, 0), local(31, 0, 0)},
		{"today since 2 pm", local(31, 14, 0), until},
		{"today, since 2 pm", local(31, 14, 0), until},
		{"since 14:00", local(31, 14, 0), until},
		{"since 14", local(31, 14, 0), until},
		{"from 2pm", local(31, 14, 0), until},
		{"since 2:30 pm today", local(31, 14, 30), until},
		{"since 2 pm yesterday", local(30, 14, 0), until},
		{"yesterday since 14:00", local(30, 14, 0), until},
		{"since 12 am", local(31, 0, 0), until},
		{"since 12 pm", local(31, 12, 0), until},
	} {
		after, before, ok := analysisWindow(tc.phrase, now)
		if !ok || after != tc.after || before != tc.before {
			t.Errorf("%q = %q..%q ok=%v, want %q..%q", tc.phrase, after, before, ok, tc.after, tc.before)
		}
	}
	for _, phrase := range []string{"since 22:00", "since 11 pm", "since 25:00", "since 13 pm", "since 2:75", "today since", "whenever", "only", "since 2 pm tomorrow"} {
		if after, before, ok := analysisWindow(phrase, now); ok {
			t.Errorf("%q accepted as %q..%q", phrase, after, before)
		}
	}
}

func TestAnalysisWindowFormsAreNamedToOwnerAndAgent(t *testing.T) {
	s, _, _ := standardsGateFixture(t)
	if _, err := s.db.UpsertRepo(t.Context(), store.Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"}); err != nil {
		t.Fatal(err)
	}
	description := ""
	for _, def := range s.Declarations() {
		if def.Name == "standards-analysis" {
			description = def.Description
		}
	}
	p := naturalPayload{Invocation: Invocation{Command: "standards-analysis", Arguments: map[string]string{"action": "start", "project": "acme/app", "window": "whenever"}}, Channel: "3"}
	refusal, err := s.resolveNatural(t.Context(), &p, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, form := range []string{"today", "today since 2 pm", "since 14:00", "since 2 pm yesterday", "yesterday", "last N days", "last N hours", "this week", "last week", "exact after and before times"} {
		if !strings.Contains(refusal, form) {
			t.Errorf("refusal text does not name %q: %s", form, refusal)
		}
		if !strings.Contains(description, form) {
			t.Errorf("standards-analysis description does not name %q: %s", form, description)
		}
	}
}
