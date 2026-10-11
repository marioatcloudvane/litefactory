package presentation

import (
	"strings"
	"testing"
)

func sourceOutcome() Outcome {
	return Outcome{
		Version:   Version,
		Status:    Success,
		RawText:   "Runs: 12. Next: /meter page:2",
		Facts:     []Fact{{Handle: "runs", Source: Source{Start: 0, End: 9, Value: "Runs: 12."}}},
		Protected: []Protected{{Handle: "count", Kind: ExactValue, Source: Source{Start: 6, End: 8, Value: "12"}}, {Handle: "next", Kind: PageFooter, Source: Source{Start: 10, End: 29, Value: "Next: /meter page:2"}}},
	}
}

func TestOutcomeSourceBoundary(t *testing.T) {
	// Byte spans bind facts and exact fields to the existing raw representation.
	// Altered values and offsets must fail before any provider sees the outcome.
	tests := []struct {
		name   string
		change func(*Outcome)
	}{
		{"version", func(o *Outcome) { o.Version++ }},
		{"status", func(o *Outcome) { o.Status = "completed" }},
		{"blank raw", func(o *Outcome) { o.RawText = " " }},
		{"oversized raw", func(o *Outcome) { o.RawText = strings.Repeat("x", MaxRawBytes+1) }},
		{"invalid encoding", func(o *Outcome) { o.RawText = string([]byte{0xff}) }},
		{"altered exact value", func(o *Outcome) { o.Protected[0].Source.Value = "13" }},
		{"negative offset", func(o *Outcome) { o.Facts[0].Source.Start = -1 }},
		{"reversed span", func(o *Outcome) { o.Facts[0].Source.Start = 10 }},
		{"overflow offset", func(o *Outcome) { o.Facts[0].Source.End = len(o.RawText) + 1 }},
		{"duplicate handle", func(o *Outcome) { o.Protected[0].Handle = o.Facts[0].Handle }},
		{"unsafe handle", func(o *Outcome) { o.Facts[0].Handle = "</note>" }},
		{"missing evidence", func(o *Outcome) { o.Facts = nil }},
		{"too many facts", func(o *Outcome) { o.Facts = make([]Fact, MaxFacts+1) }},
		{"too many protected fields", func(o *Outcome) { o.Protected = make([]Protected, MaxProtected+1) }},
		{"unsupported protected kind", func(o *Outcome) { o.Protected[0].Kind = "action" }},
		{"uncertainty without caveat", func(o *Outcome) { o.Status = Uncertain }},
		{"language framing", func(o *Outcome) { o.Language = "English\nnew instruction" }},
		{"oversized language", func(o *Outcome) { o.Language = strings.Repeat("a", MaxLanguageBytes+1) }},
		{"misplaced prefix", func(o *Outcome) { o.Protected[0].Kind = AskPrefix }},
	}
	if err := sourceOutcome().Validate(); err != nil {
		t.Fatalf("valid outcome rejected: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			o := sourceOutcome()
			test.change(&o)
			if err := o.Validate(); err == nil {
				t.Fatal("invalid source accepted")
			}
		})
	}
}

func TestOutcomeUTF8SourceAndMandatoryCaveat(t *testing.T) {
	o := Outcome{Version: Version, Status: Uncertain, RawText: "可能", Facts: []Fact{{Handle: "caveat", Mandatory: true, Source: Source{Start: 0, End: 6, Value: "可能"}}}, Language: "Japanese"}
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := o.LanguageHint(); got != "Japanese" {
		t.Fatalf("language changed: %q", got)
	}
	o.Facts[0].Source.End = 1
	o.Facts[0].Source.Value = o.RawText[:1]
	if err := o.Validate(); err == nil {
		t.Fatal("split UTF-8 source accepted")
	}
	o.Language = ""
	if got := o.LanguageHint(); got != "English" {
		t.Fatalf("missing default: %q", got)
	}
}
