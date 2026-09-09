package harness

import (
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/agent"
	"os"
	"strings"
	"testing"
	"time"
)

// Synthetic window combinations exercise the conservative exhausted-window rule.
func TestQuotaEvidenceWindows(t *testing.T) {
	tests := []struct {
		name, info      string
		terminal, quota bool
		due             int64
	}{
		{"exact", `{"status":"rejected","resetsAt":2000000100}`, true, true, 2000000160},
		{"latest exhausted", `{"status":"rejected","resetsAt":2000000100,"unifiedWindows":{"seven_day":{"utilization":1,"resetsAt":2000000200}}}`, true, true, 2000000260},
		{"ignore nonexhausted", `{"status":"rejected","resetsAt":2000000100,"unifiedWindows":{"seven_day":{"utilization":0.69,"resetsAt":2000000200}}}`, true, true, 2000000160},
		{"missing exhausted", `{"status":"rejected","resetsAt":2000000100,"unifiedWindows":{"seven_day":{"utilization":1}}}`, true, true, 0},
		{"malformed exhausted", `{"status":"rejected","resetsAt":"tomorrow"}`, true, true, 0},
		{"stale exhausted", `{"status":"rejected","resetsAt":1999999999,"unifiedWindows":{"seven_day":{"utilization":1,"resetsAt":2000000200}}}`, true, true, 0},
		{"allowed", `{"status":"allowed","resetsAt":2000000100}`, true, false, 0},
		{"warning", `{"status":"allowed_warning","resetsAt":2000000100}`, true, false, 0},
		{"overage only", `{"status":"allowed","overageStatus":"rejected","resetsAt":2000000100}`, true, false, 0},
		{"rejection then success", `{"status":"rejected","resetsAt":2000000100}`, false, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var info quotaInfo
			if err := json.Unmarshal([]byte(tt.info), &info); err != nil {
				t.Fatal(err)
			}
			var q quotaEvidence
			q.observe(info)
			q.observe(info)
			got := q.interruption(&resultEvent{IsError: tt.terminal}, time.Unix(2000000000, 0))
			if !tt.quota {
				if got != nil {
					t.Fatalf("unexpected quota %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("missing quota")
			}
			if got.Precision != agent.ResetExact {
				t.Fatal(got)
			}
			if tt.due == 0 {
				if !got.DueAt.IsZero() || got.Reason == "" {
					t.Fatalf("expected report-only %+v", got)
				}
			} else if got.DueAt.Unix() != tt.due || got.ResetBound.Unix() != tt.due-60 {
				t.Fatalf("incorrect due %+v", got)
			}
		})
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
		{"authentic exit zero", string(authentic), true, false},
		{"allowed error", strings.ReplaceAll(string(authentic), `"status":"rejected"`, `"status":"allowed"`), false, false},
		{"warning then success", strings.ReplaceAll(strings.ReplaceAll(string(authentic), `"status":"rejected"`, `"status":"allowed_warning"`), `"is_error":true`, `"is_error":false`), false, true},
		{"duplicate rejection", strings.Split(string(authentic), "\n")[0] + "\n" + string(authentic), true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, "cat <<'EVENTS'\n"+`{"type":"system","subtype":"init","session_id":"`+storedSessionID+`"}`+"\n"+tt.stream+"{\"type\":\"system\",\"subtype\":\"notification\"}\nEVENTS\n")
			req := f.request()
			req.Path = agent.PathResume
			req.Session.Resume = storedSessionID
			got, err := f.runner.Run(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			if (got.Interruption != nil) != tt.quota || got.Succeeded() != tt.success || (tt.quota && got.ResumeToken != storedSessionID) || !got.HasResult {
				t.Fatalf("result %+v", got)
			}
			if tt.quota && (got.Outcome != agent.OutcomeInterrupted || got.ExitCode != 0 || got.Interruption.Precision != agent.ResetExact) {
				t.Fatalf("result %+v", got)
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
		Info quotaInfo `json:"rate_limit_info"`
	}
	if err := json.Unmarshal([]byte(strings.Split(string(raw), "\n")[0]), &event); err != nil {
		t.Fatal(err)
	}
	var q quotaEvidence
	q.observe(event.Info)
	got := q.interruption(&resultEvent{IsError: true}, time.Date(2026, 9, 7, 17, 12, 41, 0, time.UTC))
	want := time.Date(2026, 9, 7, 18, 41, 0, 0, time.UTC)
	if got == nil || !got.DueAt.Equal(want) {
		t.Fatalf("due = %+v, want %s", got, want)
	}
}

// Synthetic admission transitions must supersede an earlier rejected attempt.
func TestRunnerQuotaAdmissionSupersedesRejection(t *testing.T) {
	for _, status := range []string{"allowed", "allowed_warning"} {
		for _, fresh := range []bool{false, true} {
			name := status + "/unrelated error"
			if fresh {
				name = status + "/fresh rejection"
			}
			t.Run(name, func(t *testing.T) {
				stream := `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":4102444900}}` + "\n" +
					`{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":"unusable"}}` + "\n" +
					`{"type":"rate_limit_event","rate_limit_info":{"status":"` + status + `","overageStatus":"rejected"}}` + "\n"
				if fresh {
					stream += `{"type":"rate_limit_event","rate_limit_info":{"status":"rejected","resetsAt":4102444800}}` + "\n"
				}
				stream += `{"type":"result","subtype":"success","is_error":true,"result":"terminal error"}` + "\n"
				f := newFixture(t, "cat <<'EVENTS'\n"+stream+"EVENTS\n")
				got := f.run(t)
				if got.Succeeded() {
					t.Fatal("terminal error classified successful")
				}
				if !fresh {
					if got.Interruption != nil || got.Outcome != agent.OutcomeFailed {
						t.Fatalf("stale rejection revived: %+v", got)
					}
					return
				}
				if got.Interruption == nil || got.Outcome != agent.OutcomeInterrupted || got.Interruption.DueAt.Unix() != 4102444860 {
					t.Fatalf("fresh evidence not isolated from previous invalid reset: %+v", got)
				}
			})
		}
	}
}
