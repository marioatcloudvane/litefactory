package report_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

func TestReporter_CreationRejectionSafeGuidance(t *testing.T) {
	t.Parallel()
	cases := []struct {
		reason   string
		guidance []string
	}{
		{"syntax", []string{"64", "lowercase", "linked", "attach", "JSON"}},
		{"payload", []string{"JSON"}}, {"version", []string{"version 1"}},
		{"fields", []string{"fields", "limits", "binding"}}, {"mode", []string{"one Mode label"}},
		{"disabled", []string{"operator", "configuration"}}, {"proposal", []string{"current eligible proposal", "fresh comment"}},
		{"controls", []string{"fresh owner comment"}},
	}
	executable := regexp.MustCompile(`/mcp-create [0-9a-f]{64} (linked|attach)`)
	const secret = "raw-secret-payload-should-never-render"
	for _, kind := range []string{"approval", "preparation"} {
		for _, tt := range cases {
			t.Run(kind+"/"+tt.reason, func(t *testing.T) {
				t.Parallel()
				reply := report.Reply{Ref: testRef, Kind: report.ReplyCreationRejected, CreationRejection: &report.CreationRejection{CommandKind: kind, Reason: tt.reason}, Command: secret, Creation: &report.CreationReply{Envelope: "hidden-proposal-content", Hash: strings.Repeat("a", 64), Relationship: "attach"}}
				got, err := newReporter(t).RenderReply(t.Context(), reply)
				if err != nil {
					t.Fatal(err)
				}
				body := got.String()
				command := "`/mcp-create`"
				if kind == "preparation" {
					command = "`/mcp-create prepare`"
				}
				for _, want := range append(tt.guidance, command, "new comment") {
					if !strings.Contains(body, want) {
						t.Errorf("guidance missing %q: %s", want, body)
					}
				}
				if len(body) > 4096 || got.Truncated() || executable.MatchString(body) || strings.Contains(body, secret) || strings.Contains(body, "hidden-proposal-content") || strings.Contains(body, strings.Repeat("a", 64)) {
					t.Fatalf("unsafe or unbounded rejection: %q", body)
				}
			})
		}
	}
}

func TestReporter_CreationRejectionUnknownMetadataFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		rejection *report.CreationRejection
	}{
		{"missing metadata", nil},
		{"unknown command", &report.CreationRejection{CommandKind: "private-payload", Reason: "syntax"}},
		{"unknown reason", &report.CreationRejection{CommandKind: "approval", Reason: "private-secret"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := newReporter(t).RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationRejected, CreationRejection: tt.rejection})
			if err == nil || got.String() != "" {
				t.Fatalf("unsafe metadata returned body=%q err=%v", got.String(), err)
			}
		})
	}
}
