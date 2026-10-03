package bugrepair

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidationGenerationRequiresEveryActualPassingObligation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	zero := 0
	commands := []ValidationRequirement{{ID: "check", Command: "make check", Directory: "."}}
	value := Validation{Generation: "run:1:generation:1", SourceDigest: hash, TargetMain: strings.Repeat("b", 40), InputsDigest: hash, Commands: []CommandOutcome{{ID: "check", Command: "make check", Directory: ".", ExitCode: &zero, Outcome: "passed", Log: LogReference{Path: "logs/check.txt", SHA256: hash}}}, Conditions: []ConditionOutcome{{ID: "bug-fixed", Outcome: "passed", Log: LogReference{Path: "logs/criterion.txt", SHA256: hash}}}}
	if e := value.Validate(commands, []string{"bug-fixed"}); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		name   string
		change func(*Validation)
	}{
		{"missing command", func(v *Validation) { v.Commands = nil }},
		{"duplicate command", func(v *Validation) { v.Commands = append(v.Commands, v.Commands[0]) }},
		{"unknown command", func(v *Validation) { v.Commands[0].ID = "invented" }},
		{"altered recipe", func(v *Validation) { v.Commands[0].Command = "true" }},
		{"failed command", func(v *Validation) { one := 1; v.Commands[0].ExitCode = &one }},
		{"missing exit observation", func(v *Validation) { v.Commands[0].ExitCode = nil }},
		{"skipped command", func(v *Validation) { v.Commands[0].Outcome = "skipped" }},
		{"missing condition", func(v *Validation) { v.Conditions = nil }},
		{"unknown condition", func(v *Validation) { v.Conditions[0].ID = "invented" }},
		{"failed condition", func(v *Validation) { v.Conditions[0].Outcome = "failed" }},
		{"escaping evidence", func(v *Validation) { v.Commands[0].Log.Path = "../outside" }},
		{"missing digest", func(v *Validation) { v.Commands[0].Log.SHA256 = "" }},
		{"unknown source", func(v *Validation) { v.SourceDigest = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, e := json.Marshal(value)
			if e != nil {
				t.Fatal(e)
			}
			var candidate Validation
			if e = json.Unmarshal(raw, &candidate); e != nil {
				t.Fatal(e)
			}
			tc.change(&candidate)
			if e = candidate.Validate(commands, []string{"bug-fixed"}); e == nil {
				t.Fatal("invalid generation certified")
			}
		})
	}
}
func TestValidationInputsCanonicalAndComplete(t *testing.T) {
	commands := []ValidationRequirement{{ID: "check", Command: "make check", Directory: "."}}
	inputs := []ValidationInput{{Name: "toolchain", SHA256: strings.Repeat("a", 64)}, {Name: "configuration", SHA256: strings.Repeat("b", 64)}}
	first, e := ValidationInputsDigest(commands, inputs)
	if e != nil {
		t.Fatal(e)
	}
	inputs[0], inputs[1] = inputs[1], inputs[0]
	second, e := ValidationInputsDigest(commands, inputs)
	if e != nil || first != second {
		t.Fatal("input ordering changed canonical identity")
	}
	inputs[0].SHA256 = strings.Repeat("c", 64)
	changed, e := ValidationInputsDigest(commands, inputs)
	if e != nil || changed == first {
		t.Fatal("input mutation omitted")
	}
	inputs = append(inputs, inputs[0])
	if _, e = ValidationInputsDigest(commands, inputs); e == nil {
		t.Fatal("duplicate input certified")
	}
	if _, e = ValidationInputsDigest(commands, nil); e == nil {
		t.Fatal("unknown inputs certified")
	}
}
