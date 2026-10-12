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
		strings.Replace(valid, "current run count", "count of １２", 1),
		strings.Replace(valid, "current run count", "count of 12.0", 1),
		strings.Replace(valid, "current run count", "count of 1", 1),
		strings.Replace(valid, "current run count", "current /meter result", 1),
		strings.Replace(valid, "current run count", "https://example.com", 1),
		strings.Replace(valid, "current run count", "example.com", 1),
		strings.Replace(valid, "current run count", "abcdefabcdef", 1),
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
	if payload.Content != "Here is the current run count." || payload.Attachment.Name != RawFilename || payload.Attachment.Content != o.RawText {
		t.Fatalf("source changed: %#v", payload)
	}
	// A number printed with the same characters in the source is allowed.
	if _, err := DecodeCandidate([]byte(strings.Replace(valid, "current run count", "count of 12 runs", 1)), o); err != nil {
		t.Fatalf("verbatim number rejected: %v", err)
	}
}

func TestMeterNumbersNarratedNearLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("Meter for acme/app\n")
	for i := 0; messageSize(b.String()) < MessageLimit-60; i++ {
		b.WriteString("lf/671 run 0003: 1,234 tokens, $0.42, 12.5s\n")
	}
	b.WriteString("Total: 1,234,567 tokens, $12.34")
	o := FromSource(b.String(), Success, "English")
	if err := o.Validate(); err != nil || messageSize(o.RawText) <= MessageLimit-60 {
		t.Fatalf("meter page: %d %v", messageSize(o.RawText), err)
	}
	candidate := Candidate{Claims: []Claim{{Text: "The total is 1,234,567 tokens costing $12.34.", Facts: []string{"result"}}, {Text: "Each run used 1,234 tokens and took 12.5s.", Facts: []string{"result"}}}}
	check := Check{Findings: []Finding{{Claim: 0, Support: Supported}, {Claim: 1, Support: Supported}}}
	payload, state, err := Render(o, candidate, check)
	if err != nil || state != Narrated || payload.Content != candidate.Claims[0].Text+"\n"+candidate.Claims[1].Text || payload.Attachment.Content != o.RawText {
		t.Fatalf("meter not narrated: %#v %s %v", payload, state, err)
	}
	for _, text := range []string{"The total is 1,234,568 tokens.", "The total is about 1.2 million tokens.", "The total costs $12.3."} {
		if err := (Candidate{Claims: []Claim{{Text: text, Facts: []string{"result"}}}}).Validate(o); err == nil {
			t.Errorf("number absent from source accepted: %q", text)
		}
	}
}

func TestRenderShowsOnlyProseAndOneLineFallback(t *testing.T) {
	const prefix = "[ask:turn]\n"
	const caveat = "The effect is uncertain."
	o := Outcome{Version: Version, Status: Uncertain, RawText: prefix + caveat, Facts: []Fact{{Handle: "caveat", Mandatory: true, Source: Source{Start: len(prefix), End: len(prefix) + len(caveat), Value: caveat}}}, Protected: []Protected{{Handle: "prefix", Kind: AskPrefix, Source: Source{Start: 0, End: len(prefix), Value: prefix}}}}
	candidate := Candidate{Claims: []Claim{{Text: "The result could not be confirmed.", Facts: []string{"caveat"}}}}
	check := Check{Findings: []Finding{{Claim: 0, Support: Supported}}}
	payload, state, err := Render(o, candidate, check)
	if err != nil || state != Narrated || payload.Content != "The result could not be confirmed." || payload.Attachment.Content != o.RawText {
		t.Fatalf("narration not alone: %#v, %s, %v", payload, state, err)
	}
	candidate.Claims[0].Facts = []string{"absent"}
	payload, state, err = Render(o, candidate, check)
	if err != nil || state != RawFallback || payload.Content != FallbackNotice || payload.Attachment.Content != o.RawText {
		t.Fatalf("missing raw fallback: %#v, %s, %v", payload, state, err)
	}
	candidate.Claims[0].Facts = []string{"caveat"}
	payload, state, err = Render(o, candidate, Check{Findings: []Finding{{Claim: 0, Support: Unsupported}}})
	if err != nil || state != RawFallback || payload.Content != "Couldn't phrase this one, raw result attached." || payload.Attachment.Content != o.RawText {
		t.Fatalf("unsupported check not one line: %#v, %s, %v", payload, state, err)
	}
	// Astral runes consume two UTF-16 units each. An oversized source is only
	// attached, never cut.
	o = Outcome{Version: Version, Status: Success, RawText: strings.Repeat("😀", 1001)}
	o.Facts = []Fact{{Handle: "result", Source: Source{Start: 0, End: len(o.RawText), Value: o.RawText}}}
	o.Protected = []Protected{{Handle: "exact", Kind: ExactValue, Source: o.Facts[0].Source}}
	payload, err = Raw(o)
	if err != nil || payload.Content != FallbackNotice || payload.Attachment.Name != RawFilename || payload.Attachment.Content != o.RawText {
		t.Fatalf("oversized source lost: %#v, %v", payload, err)
	}
	candidate = Candidate{Claims: []Claim{{Text: strings.Repeat("Long prose. ", 40), Facts: []string{"result"}}}}
	for len(candidate.Claims) < MaxClaims {
		candidate.Claims = append(candidate.Claims, candidate.Claims[0])
	}
	check = Check{}
	for i := range candidate.Claims {
		check.Findings = append(check.Findings, Finding{Claim: i, Support: Supported})
	}
	payload, state, err = Render(o, candidate, check)
	if err != nil || state != RawFallback || payload.Content != FallbackNotice || payload.Attachment.Content != o.RawText {
		t.Fatalf("oversized prose not one line: %#v, %s, %v", payload, state, err)
	}
}

func fenceForms(object string) map[string]string {
	return map[string]string{
		"bare":          object,
		"padded":        "\n  " + object + "\n",
		"fenced":        "```\n" + object + "\n```",
		"fenced json":   "```json\n" + object + "\n```\n",
		"fenced JSON":   "  ```JSON\n" + object + "\n```",
		"fenced spaced": "```json \n\n" + object + "\n\n```",
	}
}

func TestFencedNarrationRepliesDecode(t *testing.T) {
	meter := FromSource("Collection: acme/app\nObserved lower bound USD: 279.378863300000\nRuns: 12\n", Success, "English")
	ask := FromSource("> how much did we spend\n[lf:ask:v1]\nThe project spent 279.378863300000 USD over 12 runs.", Success, "English")
	for name, source := range map[string]struct {
		outcome Outcome
		reply   string
	}{
		"meter": {meter, `{"claims":[{"text":"The collection used 12 runs and reports an observed lower bound in USD.","facts":["result"],"protected":[]}]}`},
		"ask":   {ask, `{"claims":[{"text":"The project spent its budget over 12 runs.","facts":["result"],"protected":["ask_prefix"]}]}`},
	} {
		if err := source.outcome.Validate(); err != nil {
			t.Fatalf("%s source: %v", name, err)
		}
		want, err := DecodeCandidate([]byte(source.reply), source.outcome)
		if err != nil {
			t.Fatalf("%s bare candidate: %v", name, err)
		}
		for form, raw := range fenceForms(source.reply) {
			got, err := DecodeCandidate([]byte(raw), source.outcome)
			if err != nil || len(got.Claims) != 1 || got.Claims[0].Text != want.Claims[0].Text {
				t.Errorf("%s %s candidate: %+v %v", name, form, got, err)
			}
		}
		const findings = `{"findings":[{"claim":0,"support":"supported"}]}`
		wantCheck, err := DecodeCheck([]byte(findings), want)
		if err != nil {
			t.Fatalf("%s bare check: %v", name, err)
		}
		for form, raw := range fenceForms(findings) {
			got, err := DecodeCheck([]byte(raw), want)
			if err != nil || len(got.Findings) != len(wantCheck.Findings) || got.Findings[0] != wantCheck.Findings[0] {
				t.Errorf("%s %s check: %+v %v", name, form, got, err)
			}
		}
	}
}

func TestFenceStrippingKeepsStrictRules(t *testing.T) {
	o := sourceOutcome()
	const valid = `{"claims":[{"text":"Here is the current run count.","facts":["runs"],"protected":["count"]}]}`
	bad := []string{
		"Here is the JSON:\n" + valid,
		"Here is the JSON:\n```json\n" + valid + "\n```",
		valid + "\nDone.",
		"```json\n" + valid + "\n```\nDone.",
		"```json\n" + valid + "\n```\n```",
		"```yaml\n" + valid + "\n```",
		"```json " + valid + "```",
		"```json\n" + valid + "```",
		"```json\n" + valid,
		"```json\n```",
		"```json\n" + strings.Replace(valid, `"claims":`, `"unknown":`, 1) + "\n```",
		"```json\n" + `{"claims":[],"claims":[]}` + "\n```",
		"```json\n" + strings.Replace(valid, `["count"]`, `null`, 1) + "\n```",
		"```json\n" + valid + "{}\n```",
	}
	for i, raw := range bad {
		if _, err := DecodeCandidate([]byte(raw), o); err == nil {
			t.Errorf("malformed candidate %d accepted: %q", i, raw)
		}
	}
}
