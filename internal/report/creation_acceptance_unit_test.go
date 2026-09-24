package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func historicalCreationAcceptance() report.CreationAcceptance {
	created := time.Date(2020, 1, 2, 23, 45, 12, 123456789, time.FixedZone("approval origin", 2*60*60))
	return report.CreationAcceptance{Hash: strings.Repeat("abcdef01", 8), Relationship: "linked", CreatedAt: created, ExpiresAt: created.Add(30 * time.Minute)}
}

func TestReporter_CreationAcceptanceHistoricalGuidance(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		publication report.CreationPublication
		want        []string
	}{
		{"legacy", report.CreationPublicationLegacy, []string{"eligible legacy Research", "further ordinary comment to start the attempt", "does not guarantee publication"}},
		{"graph", report.CreationPublicationGraph, []string{"Graph Research cannot publish", "Another ordinary comment does not enable publication"}},
		{"unavailable", report.CreationPublicationUnavailable, []string{"unavailable", "ordinary comment does not establish publishing eligibility"}},
		{"unknown", report.CreationPublicationUnknown, []string{"eligibility is unknown", "does not establish that another ordinary comment can start an eligible publishing attempt"}},
		{"unrecognized", report.CreationPublication(255), []string{"eligibility is unknown", "does not establish that another ordinary comment can start an eligible publishing attempt"}},
	} {
		for _, relationship := range []string{"linked", "attach"} {
			t.Run(tc.name+"/"+relationship, func(t *testing.T) {
				t.Parallel()
				admission := historicalCreationAcceptance()
				admission.Relationship, admission.Publication = relationship, tc.publication
				before := admission
				reply := report.Reply{Ref: testRef, Kind: report.ReplyCreationAccepted, CreationAcceptance: &admission,
					Command: "mutable-command-must-not-appear", Creation: &report.CreationReply{Hash: strings.Repeat("f", 64), Relationship: "unrelated", Envelope: "mutable-current-proposal-must-not-appear"}}

				body, err := newReporter(t).RenderReply(t.Context(), reply)

				if err != nil {
					t.Fatal(err)
				}
				for _, want := range append([]string{"accepted and recorded", "`" + before.Hash + "`", "relationship `" + relationship + "`", "Approval alone started no publication", "2020-01-02T21:45:12.123456789Z", "2020-01-02T22:15:12.123456789Z", "stored original expiry", "eligible publishing attempt must bind before that deadline", "30-minute lifetime begins at the trusted approval-comment timestamp", "historical acknowledgment", "does not establish that the approval is still usable or the proposal is still current", "fresh approval comment", "only while it remains current and eligible", "otherwise a new eligible proposal is required", "Replaying the old approval comment does not renew consent"}, tc.want...) {
					if !strings.Contains(body.String(), want) {
						t.Errorf("missing %q in %s", want, body.String())
					}
				}
				for _, unwanted := range []string{reply.Command, reply.Creation.Hash, reply.Creation.Envelope, "+02:00"} {
					if strings.Contains(body.String(), unwanted) {
						t.Errorf("unexpected mutable or non-UTC value %q in %s", unwanted, body.String())
					}
				}
				if admission != before || body.Truncated() {
					t.Fatal("admission changed or acceptance truncated")
				}
				replay, err := newReporter(t).RenderReply(t.Context(), reply)
				if err != nil || replay.String() != body.String() {
					t.Fatalf("replay changed historical acceptance: %v", err)
				}
			})
		}
	}
}

func TestReporter_CreationAcceptanceInvalidDataFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		change func(*report.CreationAcceptance)
	}{
		{"short hash", func(a *report.CreationAcceptance) { a.Hash = "abcdef" }},
		{"non hex hash", func(a *report.CreationAcceptance) { a.Hash = strings.Repeat("z", 64) }},
		{"noncanonical hash", func(a *report.CreationAcceptance) { a.Hash = strings.ToUpper(a.Hash) }},
		{"unsupported relationship", func(a *report.CreationAcceptance) { a.Relationship = "anything" }},
		{"missing original timestamp", func(a *report.CreationAcceptance) { a.CreatedAt = time.Time{} }},
		{"missing original deadline", func(a *report.CreationAcceptance) { a.ExpiresAt = time.Time{} }},
		{"extended deadline", func(a *report.CreationAcceptance) { a.ExpiresAt = a.ExpiresAt.Add(time.Minute) }},
		{"shortened deadline", func(a *report.CreationAcceptance) { a.ExpiresAt = a.ExpiresAt.Add(-time.Minute) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			admission := historicalCreationAcceptance()
			tc.change(&admission)
			body, err := newReporter(t).RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationAccepted, CreationAcceptance: &admission})
			if err == nil || body.String() != "" {
				t.Fatalf("invalid acceptance rendered %q: %v", body.String(), err)
			}
		})
	}
	t.Run("no durable admission", func(t *testing.T) {
		t.Parallel()
		body, err := newReporter(t).RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationAccepted})
		if err == nil || body.String() != "" {
			t.Fatalf("missing admission rendered %q: %v", body.String(), err)
		}
	})
}

func TestReporter_CreationAcceptanceUnsafeOutputFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		limit      int
		redactHash bool
	}{
		{"truncated acceptance", 128, false},
		{"redacted admitted identity", 65536, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			admission := historicalCreationAcceptance()
			environ := []string{}
			if tc.redactHash {
				environ = append(environ, "API_TOKEN="+admission.Hash)
			}
			rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: environ}), Limit: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationAccepted, CreationAcceptance: &admission})
			if err == nil || body.String() != "" {
				t.Fatalf("unsafe acceptance rendered %q: %v", body.String(), err)
			}
		})
	}
}
