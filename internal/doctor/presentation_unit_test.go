package doctor_test

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/doctor"
)

func TestDoctorPresentationPartsRetainCompleteText(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, body string
		valid      bool
	}{
		{"short proposal/one complete part", "Target: run:406\nPrecondition: stopped\nRollback: restore original", true},
		{"ASCII boundary/complete parts", strings.Repeat("a", 48000) + "rollback tail", true},
		{"multibyte boundary/valid UTF8 parts", strings.Repeat("a", 47999) + strings.Repeat("界🙂", 10000) + "rollback tail", true},
		{"total byte limit/complete parts", strings.Repeat("x", 262144), true},
		{"total byte overflow/refused", strings.Repeat("x", 262145), false},
		{"empty presentation/refused", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			parts, err := doctor.PresentationParts(tt.body)
			if (err == nil) != tt.valid {
				t.Fatalf("error=%v; valid=%t", err, tt.valid)
			}
			if !tt.valid {
				if len(parts) != 0 {
					t.Fatal("refused presentation returned partial parts")
				}
				return
			}
			if strings.Join(parts, "") != tt.body {
				t.Fatal("multipart presentation dropped or changed review text")
			}
			for i, p := range parts {
				if len(p) == 0 || len(p) > 60000 || !utf8.ValidString(p) {
					t.Fatalf("part %d is empty, oversized, or invalid UTF8", i)
				}
			}
			again, err := doctor.PresentationParts(tt.body)
			if err != nil || !reflect.DeepEqual(again, parts) {
				t.Fatal("multipart boundaries are not deterministic")
			}
		})
	}
}

func TestDoctorRenderConversationReviewDetails(t *testing.T) {
	t.Parallel()
	proposal := doctor.RepairStep{Number: 1, Target: "run:406", Precondition: "Original state is running", Change: "Record interrupted state", Reason: "Process has stopped", Reversibility: "Original row remains restorable", Rollback: strings.Repeat("Restore original row and verify related state. ", 1600)}
	c := doctor.Conversation{Diagnosis: &doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "The selected run is stranded", Evidence: []string{"run:406", "session:13"}, Uncertainty: "Daemon liveness has not been observed"}}, CoverageGaps: []string{"Recent logs unavailable"}, Questions: []string{"Did the process exit?"}}, CurrentProposal: []doctor.RepairStep{proposal}}
	body := doctor.RenderConversation(c)
	for _, want := range []string{c.Diagnosis.Findings[0].Explanation, "run:406", "session:13", c.Diagnosis.Findings[0].Uncertainty, c.Diagnosis.CoverageGaps[0], c.Diagnosis.Questions[0], proposal.Target, proposal.Precondition, proposal.Change, proposal.Reason, proposal.Reversibility, proposal.Rollback, "/doctor <instructions>", "/doctor stop"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered proposal omitted required review text (length %d)", len(want))
		}
	}
	parts, err := doctor.PresentationParts(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) < 2 || strings.Join(parts, "") != body {
		t.Fatal("large proposal rollback detail not preserved in multipart presentation")
	}
}

func TestDoctorRenderConversationWithoutDiagnosis(t *testing.T) {
	t.Parallel()
	got := doctor.RenderConversation(doctor.Conversation{})
	if !strings.Contains(got, "No accepted diagnosis") || !strings.Contains(got, "/doctor <instructions>") {
		t.Fatalf("missing honest no-result status or follow-up: %s", got)
	}
}
