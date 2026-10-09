package config

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func testStackProfile() StackProfile {
	return normalizeStackProfile(StackProfile{
		Version:        1,
		Guidance:       []StackGuidance{{ID: "go", Skill: "plan/skills/plan-go-guidance/SKILL.md"}},
		Requirements:   StackRequirements{OS: []string{"darwin", "linux"}, Executables: []string{"go"}},
		Checks:         []StackCommand{{Name: "unit", BugCheckCommand: BugCheckCommand{Argv: []string{"go", "test", "./internal/config"}, CWD: ".", TimeoutSeconds: 60, Env: []string{"CGO_ENABLED"}}}},
		Environment:    []StackEnvironment{{Name: "CGO_ENABLED", Value: "0"}},
		OverrideFields: []string{"guidance", "checks", "setup", "environment"},
	})
}

func stackJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStackProfileStrictDecoding(t *testing.T) {
	valid := stackJSON(t, testStackProfile())
	if _, err := DecodeStackProfile(valid); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"oversized", bytes.Repeat([]byte(" "), StackProfileLimit+1)},
		{"unknown field", append([]byte(`{"secret":"SENSITIVE",`), valid[1:]...)},
		{"duplicate version", append([]byte(`{"version":1,`), valid[1:]...)},
		{"escaped duplicate", append([]byte(`{"ver\u0073ion":1,`), valid[1:]...)},
		{"nested duplicate", []byte(strings.Replace(string(valid), `"id":"go"`, `"id":"go","id":"other"`, 1))},
		{"nested unknown", []byte(strings.Replace(string(valid), `"id":"go"`, `"id":"go","secret":"SENSITIVE"`, 1))},
		{"trailing object", append(slices.Clone(valid), []byte(` {}`)...)},
		{"trailing garbage", append(slices.Clone(valid), 'x')},
		{"wrong version", []byte(strings.Replace(string(valid), `"version":1`, `"version":2`, 1))},
		{"alternate field case", []byte(strings.Replace(string(valid), `"version":1`, `"Version":1`, 1))},
		{"case duplicate", append([]byte(`{"Version":1,`), valid[1:]...)},
		{"nested alternate field case", []byte(strings.Replace(string(valid), `"cwd":"."`, `"CWD":"."`, 1))},
		{"wrong type", []byte(strings.Replace(string(valid), `"version":1`, `"version":"SENSITIVE"`, 1))},
		{"null", []byte(strings.Replace(string(valid), `"guidance":[`, `"guidance":null,"extra":[`, 1))},
		{"scalar", []byte(`"SENSITIVE"`)},
		{"deep", []byte(strings.Repeat("[", 18) + "1" + strings.Repeat("]", 18))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeStackProfile(tc.raw)
			if err == nil {
				t.Fatal("invalid profile accepted")
			}
			if strings.Contains(err.Error(), "SENSITIVE") {
				t.Fatalf("diagnostic leaks input: %v", err)
			}
		})
	}
	for _, raw := range []string{
		`{"version":1,"requirements":{}}`, `{"version":1,"override_fields":[]}`,
		`{"version":1,"environment":null}`, `{"version":1,"version":1}`,
		`{"version":1,"checks":[]}{}`, `{"version":0}`, `{"version":1,"checks":"SENSITIVE"}`,
	} {
		if _, err := DecodeStackOverride([]byte(raw)); err == nil || strings.Contains(err.Error(), "SENSITIVE") {
			t.Fatalf("override error = %v", err)
		}
	}
}

func TestStackProfileOverrideReplacementAndRequiredSettings(t *testing.T) {
	base := stackJSON(t, testStackProfile())
	for _, tc := range []struct {
		name, override      string
		checks, environment int
		fail                bool
	}{
		{"inherit", `{"version":1}`, 1, 1, false},
		{"replace", `{"version":1,"checks":[{"name":"other","argv":["runner"],"cwd":".","timeout_seconds":2}]}`, 1, 1, false},
		{"clear optional", `{"version":1,"checks":[],"environment":[]}`, 0, 0, false},
		{"clear required guidance", `{"version":1,"guidance":[]}`, 0, 0, true},
		{"bad merged reference", `{"version":1,"environment":[]}`, 0, 0, true},
		{"wrong schema", `{"version":3}`, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resolved, err := ResolveStackProfile(base, []byte(tc.override))
			if tc.fail {
				if err == nil {
					t.Fatal("invalid merge accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(resolved.Effective.Checks) != tc.checks || len(resolved.Effective.Environment) != tc.environment {
				t.Fatalf("wrong merged settings: %+v", resolved.Effective)
			}
			if tc.name == "replace" && resolved.Effective.Checks[0].Name != "other" {
				t.Fatal("array did not replace")
			}
			if !slices.Equal(resolved.Effective.Requirements.OS, testStackProfile().Requirements.OS) {
				t.Fatal("requirements changed")
			}
			if err := resolved.ValidateRetained(); err != nil {
				t.Fatal(err)
			}
		})
	}
	p := testStackProfile()
	p.OverrideFields = []string{"checks"}
	if _, err := ResolveStackProfile(stackJSON(t, p), []byte(`{"version":1,"environment":[]}`)); err == nil {
		t.Fatal("team restriction ignored")
	}
	p.RequiredFields = []string{"checks", "environment"}
	p.OverrideFields = []string{"checks", "environment"}
	for _, field := range p.RequiredFields {
		if _, err := ResolveStackProfile(stackJSON(t, p), []byte(`{"version":1,"`+field+`":[]}`)); err == nil {
			t.Fatal("required field cleared")
		}
	}
	if _, err := ResolveStackProfile(base, []byte{}); err == nil {
		t.Fatal("empty file treated as absent")
	}
}

func TestStackProfileCommandAndResourceLimits(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*StackProfile)
	}{
		{"guidance required", func(p *StackProfile) { p.Guidance = nil }},
		{"guidance path escape", func(p *StackProfile) { p.Guidance[0].Skill = "../SENSITIVE/SKILL.md" }},
		{"guidance absolute", func(p *StackProfile) { p.Guidance[0].Skill = "/tmp/SENSITIVE/SKILL.md" }},
		{"guidance windows path", func(p *StackProfile) { p.Guidance[0].Skill = `C:\SENSITIVE\SKILL.md` }},
		{"guidance resource type", func(p *StackProfile) { p.Guidance[0].Skill = "plan/resource.md" }},
		{"duplicate guidance", func(p *StackProfile) { p.Guidance = append(p.Guidance, p.Guidance[0]) }},
		{"empty argv", func(p *StackProfile) { p.Checks[0].Argv = nil }},
		{"blank command", func(p *StackProfile) { p.Checks[0].Argv = []string{" "} }},
		{"too many argv", func(p *StackProfile) { p.Checks[0].Argv = slices.Repeat([]string{"x"}, 129) }},
		{"large argv", func(p *StackProfile) { p.Checks[0].Argv = []string{strings.Repeat("x", 64<<10+1)} }},
		{"nul argv", func(p *StackProfile) { p.Checks[0].Argv = []string{"SENSITIVE\x00"} }},
		{"cwd escape", func(p *StackProfile) { p.Checks[0].CWD = "../SENSITIVE" }},
		{"cwd empty", func(p *StackProfile) { p.Checks[0].CWD = "" }},
		{"cwd absolute", func(p *StackProfile) { p.Checks[0].CWD = "/SENSITIVE" }},
		{"timeout zero", func(p *StackProfile) { p.Checks[0].TimeoutSeconds = 0 }},
		{"timeout large", func(p *StackProfile) { p.Checks[0].TimeoutSeconds = 86401 }},
		{"undeclared env", func(p *StackProfile) { p.Checks[0].Env = []string{"SENSITIVE"} }},
		{"duplicate env ref", func(p *StackProfile) { p.Checks[0].Env = []string{"CGO_ENABLED", "CGO_ENABLED"} }},
		{"too many commands", func(p *StackProfile) { p.Checks = slices.Repeat(p.Checks, 65) }},
		{"duplicate command", func(p *StackProfile) { p.Checks = append(p.Checks, p.Checks[0]) }},
		{"invalid setup", func(p *StackProfile) { p.Setup = []StackCommand{{Name: "setup"}} }},
		{"unsupported rerun", func(p *StackProfile) { p.Checks[0].Rerun = &BugCheckRerun{Failures: "SENSITIVE"} }},
		{"forbidden policy", func(p *StackProfile) { p.OverrideFields = []string{"requirements"} }},
		{"duplicate policy", func(p *StackProfile) { p.OverrideFields = []string{"checks", "checks"} }},
		{"required missing", func(p *StackProfile) { p.RequiredFields = []string{"setup"} }},
		{"bad executable", func(p *StackProfile) { p.Requirements.Executables = []string{"/SENSITIVE"} }},
		{"duplicate host", func(p *StackProfile) { p.Requirements.OS = []string{"darwin", "darwin"} }},
		{"large host list", func(p *StackProfile) { p.Requirements.OS = slices.Repeat([]string{"linux"}, 65) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testStackProfile()
			tc.change(&p)
			err := p.Validate()
			if err == nil {
				t.Fatal("invalid setting accepted")
			}
			if strings.Contains(err.Error(), "SENSITIVE") {
				t.Fatalf("diagnostic leaks value: %v", err)
			}
		})
	}
	p := testStackProfile()
	p.Checks[0].Rerun = &BugCheckRerun{Failures: BugRerunFailuresGoTest, Argv: []string{"go", "test", BugRerunPackages, "-run", BugRerunTests}, TimeoutSeconds: 60}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestStackEnvironmentPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"CGO_ENABLED", "0", true}, {"GOTOOLCHAIN", "local", true}, {"GOMAXPROCS", "4", true},
		{"LANG", "en_US.UTF-8", true}, {"CI", "true", true}, {"NO_COLOR", "1", true},
		{"DOTNET_NOLOGO", "1", true}, {"PYTHONUNBUFFERED", "1", true},
		{"PATH", "SENSITIVE", false}, {"HOME", "SENSITIVE", false}, {"CODEX_HOME", "SENSITIVE", false},
		{"ANTHROPIC_API_KEY", "SENSITIVE", false}, {"OPENAI_API_KEY", "SENSITIVE", false},
		{"GH_TOKEN", "SENSITIVE", false}, {"GOCACHE", "/SENSITIVE", false}, {"LD_PRELOAD", "SENSITIVE", false},
		{"GOTOOLCHAIN", "auto", false}, {"CGO_ENABLED", "SENSITIVE", false},
		{"LANG", "SENSITIVE\n", false}, {"LANG", "", false}, {"GOMAXPROCS", "000", false},
		{"GOMAXPROCS", "-1", false}, {"TZ", strings.Repeat("SENSITIVE", 600), false},
	} {
		t.Run(tc.name+tc.value[:min(len(tc.value), 10)], func(t *testing.T) {
			err := ValidateStackEnvironment(StackEnvironment{Name: tc.name, Value: tc.value})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "SENSITIVE") {
				t.Fatal("sensitive value echoed")
			}
		})
	}
}

func TestStackProfileDigestsAndRetainedIntegrity(t *testing.T) {
	base := stackJSON(t, testStackProfile())
	a, err := ResolveStackProfile(base, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ResolveStackProfile(append(slices.Clone(base), '\n'), []byte(`{"version":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if a.EffectiveDigest != b.EffectiveDigest || a.BaseDigest == b.BaseDigest || b.OverrideDigest == "" {
		t.Fatal("raw and effective digests do not distinguish formatting")
	}
	base[0] = 'x'
	if err := a.ValidateRetained(); err != nil {
		t.Fatal("resolution aliases input:", err)
	}
	for _, change := range []func(*ResolvedStackProfile){
		func(r *ResolvedStackProfile) { r.BaseBytes[0] = 'x' },
		func(r *ResolvedStackProfile) { r.BaseDigest = "bad" },
		func(r *ResolvedStackProfile) { r.OverrideDigest = "bad" },
		func(r *ResolvedStackProfile) { r.EffectiveDigest = "bad" },
		func(r *ResolvedStackProfile) { r.Effective.Checks[0].Argv[0] = "other" },
	} {
		r, err := ResolveStackProfile(stackJSON(t, testStackProfile()), nil)
		if err != nil {
			t.Fatal(err)
		}
		change(&r)
		if err := r.ValidateRetained(); err == nil {
			t.Fatal("changed retained capture accepted")
		}
	}
	raw := stackJSON(t, b)
	var restored ResolvedStackProfile
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.ValidateRetained(); err != nil {
		t.Fatal(err)
	}
}
