package denypolicy

import (
	"reflect"
	"strings"
	"testing"
)

func TestResolveOrderedUnion(t *testing.T) {
	c := Catalogue{"base": {Deny: []string{"Bash(make check:*)", "Agent(Explore)"}}, "extra": {Deny: []string{"Agent(Explore)", "Edit(secret/**)"}}}
	p, err := Resolve(c, "base, base", "extra,base", "worker")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Groups, []string{"base", "extra"}) || len(p.Rules) != 3 {
		t.Fatalf("policy: %+v", p)
	}
	if p.Rules[2].Group != "extra" || p.Rules[2].Index != 2 || !strings.Contains(p.Rules[2].Selection, "worker") {
		t.Fatalf("provenance: %+v", p.Rules[2])
	}
	clone := p.Clone()
	clone.Groups[0] = "other"
	clone.Rules[0].Original = "other"
	if p.Groups[0] != "base" || p.Rules[0].Original != "Bash(make check:*)" {
		t.Fatal("clone aliases policy")
	}
	empty, err := Resolve(c, "base", "", "worker")
	if err != nil || len(empty.Rules) != 2 {
		t.Fatalf("empty node clears defaults: %+v %v", empty, err)
	}
	none, err := Resolve(nil, " ", "", "")
	if err != nil || none != nil {
		t.Fatalf("empty: %+v %v", none, err)
	}
	var nilPolicy *Policy
	if nilPolicy.Clone() != nil || nilPolicy.Selected() {
		t.Fatal("nil handling")
	}
}
func TestRejectInvalidReferencesAndCatalogue(t *testing.T) {
	for _, value := range []string{"a,,b", ",a", "a,", "Bash(make check)", "a.b", "1group"} {
		if _, err := ParseReferences(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, value := range []string{"", " make check", "Bash()", "Read(x", "Unknown(x)", "Agent( )", "Bash(a))(", "Bash(a\nb)"} {
		if _, err := ParseRule(value); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, c := range []Catalogue{{"bad.name": {Deny: []string{"Agent"}}}, {"empty": {}}, {"bad": {Deny: []string{"make check"}}}} {
		if err := ValidateCatalogue(c); err == nil {
			t.Errorf("accepted %+v", c)
		}
	}
	if _, err := Resolve(nil, "missing", "", "worker"); err == nil || !strings.Contains(err.Error(), "graph lf_deny") {
		t.Fatalf("missing provenance: %v", err)
	}
}
