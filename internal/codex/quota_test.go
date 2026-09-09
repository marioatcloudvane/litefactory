package codex

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"os"
	"strings"
	"testing"
	"time"
)

// All variants below are synthetic boundary cases; authentic stream replay is separate.
func TestQuotaInterruptionLocalBounds(t *testing.T) {
	vienna, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatal(err)
	}
	parse := func(s string) time.Time {
		t.Helper()
		v, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	tests := []struct {
		name, message, start, end, due string
		a, b                           *time.Location
		recognized                     bool
	}{
		{"minute upper bound", "3:00 PM", "2026-09-08T12:00:00Z", "2026-09-08T12:30:00Z", "2026-09-08T13:02:00Z", vienna, vienna, true},
		{"noon", "12:00 PM", "2026-09-08T09:00:00Z", "2026-09-08T09:30:00Z", "2026-09-08T10:02:00Z", vienna, vienna, true},
		{"explicit next date", "Sep 9, 2026 12:00 AM", "2026-09-08T20:00:00Z", "2026-09-08T21:30:00Z", "2026-09-08T22:02:00Z", vienna, vienna, true},
		{"midnight spanning", "3:00 AM", "2026-09-08T21:00:00Z", "2026-09-08T22:30:00Z", "", vienna, vienna, true},
		{"stale minute", "3:00 PM", "2026-09-08T12:00:00Z", "2026-09-08T14:00:00Z", "", vienna, vienna, true},
		{"fold", "2:30 AM", "2026-10-24T22:00:00Z", "2026-10-24T23:00:00Z", "", vienna, vienna, true},
		{"gap", "2:30 AM", "2026-03-28T23:00:00Z", "2026-03-29T00:00:00Z", "", vienna, vienna, true},
		{"explicit contradictory zone", "3:00 PM UTC", "2026-09-08T12:00:00Z", "2026-09-08T12:30:00Z", "", vienna, vienna, true},
		{"missing timezone", "3:00 PM", "2026-09-08T12:00:00Z", "2026-09-08T12:30:00Z", "", nil, nil, true},
		{"changed timezone", "3:00 PM", "2026-09-08T12:00:00Z", "2026-09-08T12:30:00Z", "", vienna, time.UTC, true},
		{"missing reset", "", "2026-09-08T12:00:00Z", "2026-09-08T12:30:00Z", "", vienna, vienna, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			message := "You've hit your usage limit. Please try again at " + tt.message + "."
			got := quotaInterruption(message, parse(tt.start), parse(tt.end), tt.a, tt.b)
			if got == nil {
				t.Fatal("quota not recognized")
			}
			if got.Precision != agent.ResetMinute {
				t.Fatal(got)
			}
			if tt.due == "" {
				if !got.DueAt.IsZero() || got.Reason == "" {
					t.Fatalf("expected report-only: %+v", got)
				}
			} else if !got.DueAt.Equal(parse(tt.due)) || !got.ResetBound.Equal(parse(tt.due).Add(-time.Minute)) {
				t.Fatalf("bound: %+v", got)
			}
		})
	}
	for _, message := range []string{"network error", "I think You've hit your usage limit. try again at 3:00 PM."} {
		if quotaInterruption(message, parse("2026-09-08T12:00:00Z"), parse("2026-09-08T12:30:00Z"), vienna, vienna) != nil {
			t.Fatalf("recognized generic text %q", message)
		}
	}
}

func TestRunnerQuotaDiagnostics(t *testing.T) {
	authentic, err := os.ReadFile("testdata/quota-authentic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, stream   string
		quota, success bool
	}{
		{"authentic duplicate terminal", string(authentic), true, false},
		{"nested terminal only", strings.Split(strings.TrimSpace(string(authentic)), "\n")[1] + "\n", true, false},
		{"agent prose", `{"type":"item.completed","item":{"type":"agent_message","text":"You've hit your usage limit. try again at 10:24 PM."}}
{"type":"turn.completed"}
`, false, true},
		{"generic terminal", `{"type":"turn.failed","error":{"message":"network unavailable"}}
`, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, _ := fakeRunner(t, "cat <<'EVENTS'\n"+`{"type":"thread.started","thread_id":"quota-thread"}`+"\n"+tt.stream+`{"type":"item.completed","item":{"type":"agent_message","text":"trailing output"}}`+"\nEVENTS\n")
			req := request(t, agent.PathResume)
			req.Session.Resume = "quota-thread"
			got, err := runner.Run(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Interruption != nil) != tt.quota || got.Succeeded() != tt.success || got.ResumeToken != "quota-thread" || !strings.Contains(got.ResultText, "trailing output") {
				t.Fatalf("result %+v", got)
			}
			if tt.quota && got.Outcome != agent.OutcomeInterrupted {
				t.Fatalf("outcome %s", got.Outcome)
			}
		})
	}
}

func TestAuthenticQuotaReset(t *testing.T) {
	raw, err := os.ReadFile("testdata/quota-authentic.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var event struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &event); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("Europe/Vienna")
	if err != nil {
		t.Fatal(err)
	}
	got := quotaInterruption(event.Message, time.Date(2026, 9, 5, 15, 24, 49, 0, time.UTC), time.Date(2026, 9, 5, 16, 18, 38, 0, time.UTC), loc, loc)
	want := time.Date(2026, 9, 5, 20, 26, 0, 0, time.UTC)
	if got == nil || !got.DueAt.Equal(want) {
		t.Fatalf("due = %+v, want %s under documented local assumption", got, want)
	}
}
