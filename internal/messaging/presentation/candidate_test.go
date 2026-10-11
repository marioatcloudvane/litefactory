package presentation

import (
	"strings"
	"testing"
)

func TestStrictCandidateAndIndependentCheck(t *testing.T) {
	o := sourceOutcome()
	const valid = `{"claims":[{"text":"Here is the current run count.","facts":["runs"],"protected":["count"]}]}`
	candidate, err := DecodeCandidate([]byte(valid), o)
	if err != nil {
		t.Fatal(err)
	}
	bad := []string{
		`null`,
		valid + `{}`,
		`{"claims":[],"claims":[]}`,
		`{"Claims":[]}`,
		`{"claims":null}`,
		`{"claims":[{"text":"Count","text":"Other","facts":["runs"]}]}`,
		strings.Replace(valid, `"claims":`, `"unknown":`, 1),
		strings.Replace(valid, `"runs"`, `"missing"`, 1),
		strings.Replace(valid, `"count"`, `"missing"`, 1),
		strings.Replace(valid, "current run count", "count of 13", 1),
		strings.Replace(valid, "current run count", "count of １３", 1),
		strings.Replace(valid, "current run count", "current /meter result", 1),
		strings.Replace(valid, "current run count", "https://example.com", 1),
		strings.Replace(valid, "current run count", "[ask marker]", 1),
		strings.Repeat(" ", MaxCandidateBytes) + valid,
	}
	for i, raw := range bad {
		if _, err := DecodeCandidate([]byte(raw), o); err == nil {
			t.Errorf("invalid candidate %d accepted", i)
		}
	}
	checks := []string{
		`{"findings":[]}`,
		`{"findings":[{"support":"supported"}]}`,
		`{"findings":[{"claim":0,"support":"uncertain"}]}`,
		`{"findings":[{"claim":0,"support":"unsupported"}]}`,
		`{"findings":[{"claim":1,"support":"supported"}]}`,
		`{"findings":[{"claim":0,"claim":1,"support":"supported"}]}`,
	}
	for i, raw := range checks {
		if _, err := DecodeCheck([]byte(raw), candidate); err == nil {
			t.Errorf("invalid check %d accepted", i)
		}
	}
	check, err := DecodeCheck([]byte(`{"findings":[{"claim":0,"support":"supported"}]}`), candidate)
	if err != nil {
		t.Fatal(err)
	}
	payload, state, err := Render(o, candidate, check)
	if err != nil || state != Narrated {
		t.Fatalf("valid candidate failed: %s, %v", state, err)
	}
	if payload.Content != "Here is the current run count.\n12\nNext: /meter page:2" || payload.Attachment.Name != RawFilename || payload.Attachment.Content != o.RawText {
		t.Fatalf("source changed: %#v", payload)
	}
}

func TestRenderPreservesPrefixCaveatAndBudget(t *testing.T) {
	const prefix = "[ask:turn]\n"
	const caveat = "The effect is uncertain."
	o := Outcome{Version: Version, Status: Uncertain, RawText: prefix + caveat, Facts: []Fact{{Handle: "caveat", Mandatory: true, Source: Source{Start: len(prefix), End: len(prefix) + len(caveat), Value: caveat}}}, Protected: []Protected{{Handle: "prefix", Kind: AskPrefix, Source: Source{Start: 0, End: len(prefix), Value: prefix}}}}
	candidate := Candidate{Claims: []Claim{{Text: "The result could not be confirmed.", Facts: []string{"caveat"}}}}
	check := Check{Findings: []Finding{{Claim: 0, Support: Supported}}}
	payload, state, err := Render(o, candidate, check)
	if err != nil || state != Narrated || !strings.HasPrefix(payload.Content, prefix) || !strings.Contains(payload.Content, caveat) || payload.Attachment.Content != o.RawText {
		t.Fatalf("prefix or caveat changed: %#v, %s, %v", payload, state, err)
	}
	candidate.Claims[0].Facts = []string{"absent"}
	payload, state, err = Render(o, candidate, check)
	if err != nil || state != RawFallback || payload.Content != o.RawText+"\n\nRaw result." {
		t.Fatalf("missing raw fallback: %#v, %s, %v", payload, state, err)
	}
	// Astral runes consume two UTF-16 units each. No exact source value may
	// be cut to make room for prose or a raw label.
	o = Outcome{Version: Version, Status: Success, RawText: strings.Repeat("😀", 1001)}
	o.Facts = []Fact{{Handle: "result", Source: Source{Start: 0, End: len(o.RawText), Value: o.RawText}}}
	o.Protected = []Protected{{Handle: "exact", Kind: ExactValue, Source: o.Facts[0].Source}}
	candidate = Candidate{Claims: []Claim{{Text: "Here is the result.", Facts: []string{"result"}}}}
	payload, state, err = Render(o, candidate, check)
	if err != nil || state != RawFallback || payload.Content != "Raw result attached." || payload.Attachment.Content != o.RawText {
		t.Fatalf("oversized source lost: %#v, %s, %v", payload, state, err)
	}
}
