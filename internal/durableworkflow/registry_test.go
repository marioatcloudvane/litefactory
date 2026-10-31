package durableworkflow

import (
	"testing"
)

func TestRegistryFrozenContractsAndTargets(t *testing.T) {
	definitions := BuiltinDefinitions()
	for _, fixture := range []struct{ ref, project, input string }{{"daily", "alpha", "focus"}, {"incident", "beta", "incident"}} {
		definitions = append(definitions, Definition{Ref: fixture.ref, Version: "1", Adapter: "fixture", Name: "Review", Purpose: "Distinct installed review", ProjectMode: ProjectFixed, BindingProject: fixture.project, FixedProject: fixture.project, Inputs: object(map[string]Field{fixture.input: {Type: "string"}}, fixture.input), RequiredPermissions: []string{"repository:actions"}, DeadlineSeconds: 60})
	}
	registry, err := NewRegistry(definitions)
	if err != nil {
		t.Fatal(err)
	}
	definitions[0].Inputs.Required[0] = "mutated"
	log, err := registry.Get("log-analysis", "1")
	if err != nil {
		t.Fatal(err)
	}
	if log.Inputs.Required[0] != "after" || log.Digest == "" {
		t.Fatal("registry mutated after installation")
	}
	log.Inputs.Required[0] = "again"
	log, _ = registry.Get("log-analysis", "1")
	if log.Inputs.Required[0] != "after" {
		t.Fatal("read mutated registry")
	}
	for _, fixture := range []struct{ ref, project, input string }{{"daily", "alpha", "focus"}, {"incident", "beta", "incident"}} {
		d, _ := registry.Get(fixture.ref, "1")
		resolved, err := Resolve(d, "", map[string]any{fixture.input: "review"})
		if err != nil || resolved.Project != fixture.project {
			t.Fatalf("fixed resolution: %+v %v", resolved, err)
		}
		if _, err = Resolve(d, fixture.project, map[string]any{fixture.input: "review"}); err == nil {
			t.Fatal("explicit fixed project accepted")
		}
		if _, err = Resolve(d, "", map[string]any{fixture.input: "review", "nested": map[string]any{"project": "other"}}); err == nil {
			t.Fatal("nested override accepted")
		}
	}
	if _, err = registry.Get("log-analysis", "2"); err == nil {
		t.Fatal("unknown version accepted")
	}
}

func TestResolveStrictCanonicalInputs(t *testing.T) {
	registry, _ := NewRegistry(BuiltinDefinitions())
	log, _ := registry.Get("log-analysis", "1")
	valid := map[string]any{"after": "2026-10-30T01:00:00+01:00", "before": "2026-10-31T00:00:00Z", "dry_run": true}
	resolved, err := Resolve(log, "repository", valid)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Inputs["after"] != "2026-10-30T00:00:00Z" {
		t.Fatalf("time not normalized: %v", resolved.Inputs)
	}
	for name, inputs := range map[string]map[string]any{
		"missing dry run": {"after": valid["after"], "before": valid["before"]},
		"unknown":         {"after": valid["after"], "before": valid["before"], "dry_run": true, "unknown": 1},
		"wrong type":      {"after": valid["after"], "before": valid["before"], "dry_run": "true"},
		"relative time":   {"after": "yesterday", "before": valid["before"], "dry_run": true},
		"reverse period":  {"after": valid["before"], "before": valid["after"], "dry_run": true},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Resolve(log, "repository", inputs); err == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	if _, err = Resolve(log, "", valid); err == nil {
		t.Fatal("missing project accepted")
	}
	author, _ := registry.Get("standards-authoring", "1")
	for _, project := range []string{"", "repository"} {
		if _, err = Resolve(author, project, map[string]any{"intent": "draft testing"}); err != nil {
			t.Fatal(err)
		}
	}
	author.ProjectMode = ProjectNone
	if _, err = Resolve(author, "repository", map[string]any{"intent": "draft testing"}); err == nil {
		t.Fatal("none mode accepted project")
	}
	if _, err = Resolve(author, "", map[string]any{"intent": "draft testing"}); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRejectsInvalidDefinitions(t *testing.T) {
	for _, mutate := range []func(*Definition){func(d *Definition) { d.Version = "" }, func(d *Definition) { d.ProjectMode = "ambient" }, func(d *Definition) { d.ProjectMode = ProjectFixed }, func(d *Definition) { d.Inputs.AdditionalProperties = true }, func(d *Definition) { d.RequiredPermissions = nil }, func(d *Definition) { d.DeadlineSeconds = 0 }} {
		d := BuiltinDefinitions()[0]
		mutate(&d)
		if _, err := NewRegistry([]Definition{d}); err == nil {
			t.Fatal("invalid definition accepted")
		}
	}
	d := BuiltinDefinitions()[0]
	if _, err := NewRegistry([]Definition{d, d}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
