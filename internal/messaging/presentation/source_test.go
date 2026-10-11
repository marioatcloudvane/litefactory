package presentation

import (
	"strings"
	"testing"
)

func TestSourceProtectionAndLanguageHints(t *testing.T) {
	for _, test := range []struct{ question, language string }{
		{"Wie ist der Zustand?", "German"},
		{"What is the current status?", "English"},
		{"Pourquoi le projet est bloqué?", "French"},
		{"¿Cómo está el proyecto?", "Spanish"},
		{"状況を教えてください", "Japanese"},
		{"", "English"},
	} {
		if got := DetectLanguage(test.question); got != test.language {
			t.Errorf("language %q: %s", test.question, got)
		}
	}
	const raw = "> Wie ist der Zustand?\n> Project: acme/app\n[lf:ask:v1]\nThe effect is uncertain.\nToken: abcdef123456\nhttps://example.com/issue/12\nnext: /meter page:2"
	o := FromSource(raw, Uncertain, "German")
	if err := o.Validate(); err != nil {
		t.Fatal(err)
	}
	c := Candidate{Claims: []Claim{{Text: "Der Ausgang ist ungewiss.", Facts: []string{"result"}}}}
	check := Check{Findings: []Finding{{Claim: 0, Support: Supported}}}
	p, state, err := Render(o, c, check)
	if err != nil || state != Narrated || strings.Count(p.Content, "[lf:ask:v1]") != 1 || p.Attachment.Content != raw {
		t.Fatalf("protected source changed: %+v %s %v", p, state, err)
	}
	for _, field := range o.Protected {
		if !strings.Contains(p.Content, field.Source.Value) {
			t.Errorf("protected span lost: %+v", field)
		}
	}
}
