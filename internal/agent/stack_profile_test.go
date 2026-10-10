package agent

import (
	"github.com/cloudvane/litefactory/internal/config"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestStackEnvironmentRetainsProviderCredentialsAndSameResolvedValues(t *testing.T) {
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}],"environment":[{"name":"CI","value":"true"},{"name":"GOTOOLCHAIN","value":"local"},{"name":"LANG","value":"C"}],"override_fields":["environment"]}`), []byte(`{"version":1,"environment":[{"name":"CI","value":"1"},{"name":"GOTOOLCHAIN","value":"local"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req := Request{StackProfile: &profile}
	for _, base := range [][]string{{"PATH=/bin", "ANTHROPIC_API_KEY=provider-owned", "CI=false"}, {"PATH=/bin", "CODEX_API_KEY=provider-owned", "CI=false"}} {
		got, err := req.StackEnvironment(base)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(got, "CI=1") || !slices.Contains(got, "GOTOOLCHAIN=local") || slices.Contains(got, "CI=false") {
			t.Fatal("resolved settings not applied", got)
		}
		if !slices.Contains(got, base[1]) {
			t.Fatal("provider authentication changed")
		}
		again, err := req.StackEnvironment(base)
		if err != nil || !reflect.DeepEqual(got, again) {
			t.Fatal("environment is not deterministic")
		}
	}
}
func TestStackEnvironmentRejectsTamperedRetainedSettings(t *testing.T) {
	profile, err := config.ResolveStackProfile([]byte(`{"version":1,"guidance":[{"id":"stack","skill":"plan/skills/stack/SKILL.md"}]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	profile.Effective.Environment = []config.StackEnvironment{{Name: "ANTHROPIC_API_KEY", Value: "private-canary"}}
	got, err := (Request{StackProfile: &profile}).StackEnvironment([]string{"PATH=/bin"})
	if err == nil || got != nil {
		t.Fatal("tampered environment accepted")
	}
	if strings.Contains(err.Error(), "private-canary") {
		t.Fatal("diagnostic exposed sensitive value")
	}
}
func TestStackEnvironmentLegacyLeavesProviderEnvironmentUnchanged(t *testing.T) {
	base := []string{"PATH=/bin", "HOME=/retained", "CI=existing"}
	got, err := (Request{}).StackEnvironment(base)
	if err != nil || !reflect.DeepEqual(got, base) {
		t.Fatal("legacy provider environment changed", err)
	}
}
