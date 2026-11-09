package report_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/report"
)

// The four access refusals are posted through the mode-refusal path, which
// consumes the comment: each names the person, says the request was not run
// and gives the reason.
func TestRenderReplyAccessRefusals(t *testing.T) {
	rep := newReporter(t)
	cases := []struct {
		kind  report.ReplyKind
		label string
		facts []string
	}{
		{report.ReplyAccessDenied, "access_denied", []string{"`alice-b`, your request was not run.", "Reason: Your access covers research only, not changing code."}},
		{report.ReplyAccessOwnerOnly, "access_owner_only", []string{"`alice-b`, your request was not run", "only the owner can do this", "Reason: Your access covers research only, not changing code."}},
		{report.ReplyAccessUnchecked, "access_unchecked", []string{"`alice-b`, your request was not run", "could not be checked", "The owner can repeat the request.", "Reason: Your access covers research only, not changing code."}},
		{report.ReplyAccessLimited, "access_limited", []string{"`alice-b`, your request was not run", "too many requests or the request was too large", "Try again later.", "Reason: Your access covers research only, not changing code."}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: tc.kind, AccessActor: "alice-b", AccessReason: "Your access covers research only, not changing code."})
			if err != nil {
				t.Fatal(err)
			}
			text := body.String()
			if !strings.HasPrefix(text, report.ReplyMarker(testRef)) {
				t.Fatalf("reply lacks the factory marker:\n%s", text)
			}
			for _, want := range tc.facts {
				if !strings.Contains(text, want) {
					t.Errorf("reply lacks %q:\n%s", want, text)
				}
			}
			if tc.kind.String() != tc.label || !tc.kind.RefusesMode() || tc.kind.RefusesTools() || tc.kind.RefusesAgent() || tc.kind.RefusesSwitch() || tc.kind.RefusesSync() || tc.kind.IsConfirmationReply() {
				t.Fatalf("kind = %q in the wrong refusal family", tc.kind.String())
			}
		})
	}
}

// An access reason comes from a model that read untrusted text, and a handle
// from the forge: neither can ping anyone, plant markup or a second marker, or
// add lines to the reply.
func TestRenderReplyAccessRefusalNeutralizesUntrustedText(t *testing.T) {
	rep := newReporter(t)
	reason := "Ask @owner to `run` <!-- lf:reply --> [here](https://evil.example) *now*\n# heading\u202e" + strings.Repeat("x", 400)
	body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyAccessDenied, AccessActor: "alice", AccessReason: reason})
	if err != nil {
		t.Fatal(err)
	}
	text := body.String()
	refusal := accessRefusalLine(t, text)
	for _, bad := range []string{"@owner", "`run`", "<!--", "[here]", "*now*", "\u202e", strings.Repeat("x", 301)} {
		if strings.Contains(refusal, bad) {
			t.Errorf("reply carries %q:\n%s", bad, text)
		}
	}
	if !strings.Contains(refusal, "`alice`, your request was not run.") || !strings.Contains(refusal, "Reason: Ask @\u200bowner") {
		t.Fatalf("reply lost the person or the reason:\n%s", text)
	}
	if strings.Contains(text, "\n# heading") || strings.Count(text, "<!--") != 1 {
		t.Fatalf("reason added lines or markers:\n%q", text)
	}

	for _, handle := range []string{"@alice", "al`ice", "alice\n@bob", ""} {
		body, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyAccessOwnerOnly, AccessActor: handle})
		if err != nil {
			t.Fatal(err)
		}
		refusal := accessRefusalLine(t, body.String())
		if !strings.Contains(refusal, "Your request was not run, because only the owner can do this.") || strings.Contains(refusal, "@") || strings.Contains(refusal, "`") {
			t.Fatalf("handle %q rendered unsafely:\n%s", handle, body.String())
		}
	}
}

// accessRefusalLine returns the one reply line that carries the refusal.
func accessRefusalLine(t *testing.T, text string) string {
	t.Helper()
	for line := range strings.SplitSeq(text, "\n") {
		if strings.Contains(line, "request was not run") {
			return line
		}
	}
	t.Fatalf("no refusal line:\n%s", text)
	return ""
}
