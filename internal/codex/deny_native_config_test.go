package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

const selectedConfigFixture = `{"config":{"approval_policy":"never","default_permissions":"factory_selected","cli_auth_credentials_store":"file","sqlite_home":"/fixture/canonical","permissions":{"factory_selected":{"extends":":workspace","filesystem":{"/fixture/protected":"read"},"network":{"enabled":false,"proxy_url":null}}},"projects":{"/fixture/work":{"trust_level":"untrusted"}},"features":{"multi_agent":false}},"layers":[{"name":{"type":"user","file":"/fixture/home/config.toml"},"config":{"sqlite_home":"/fixture/canonical","permissions":{"factory_selected":{"extends":":workspace","filesystem":{"/fixture/protected":"read"},"network":{"enabled":false}}},"projects":{"/fixture/work":{"trust_level":"untrusted"}}}}]}`

func TestSelectedConfigurationAdmission(t *testing.T) {
	t.Parallel()
	t.Run("configuration exchange creates no agent", func(t *testing.T) {
		r, argv := selectedTestRunner(t, `read -r request
printf '%s\n' '{"id":1,"result":{}}'
read -r initialized
read -r request
printf '%s\n' '{"id":2,"result":`+selectedConfigFixture+`}'
if read -r request; then exit 9; fi
exit 0
`)
		r.environ = append(r.environ, "CODEX_HOME=/fixture/home")
		args := []string{"exec", "--strict-config", "--json", "--model", "gpt-test", "-c", `default_permissions="factory_selected"`, "-c", `mcp_servers.factory.command="helper"`, "-c", `features.multi_agent=false`, "resume", "native-session", "-"}
		if err := r.checkSelectedConfiguration(t.Context(), agent.Request{WorktreePath: t.TempDir()}, r.environ, args); err != nil {
			t.Fatal(err)
		}
		got := string(mustRead(t, argv))
		for _, want := range []string{"app-server --strict-config --listen stdio://", `-c default_permissions="factory_selected"`, `-c mcp_servers.factory.command="helper"`, `-c features.multi_agent=false`, `-c model="gpt-test"`} {
			if !strings.Contains(got, want) {
				t.Errorf("check arguments missing %q: %q", want, got)
			}
		}
		for _, forbidden := range []string{"resume", "native-session", "--json", "--model"} {
			if strings.Contains(got, forbidden) {
				t.Errorf("exec-only argument in validation: %q", got)
			}
		}
	})
	t.Run("rejects and redacts native error", func(t *testing.T) {
		r, _ := selectedTestRunner(t, `printf '%s' 'sensitive configuration value' >&2
exit 1
`)
		err := r.checkSelectedConfiguration(t.Context(), agent.Request{WorktreePath: t.TempDir()}, r.environ, []string{"exec"})
		if err == nil || strings.Contains(err.Error(), "sensitive configuration value") {
			t.Fatalf("unsafe admission error: %v", err)
		}
	})
}

func TestSelectedConfigurationManagedConflicts(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*selectedConfigSnapshot)
	}{
		{"approval", func(s *selectedConfigSnapshot) { s.Config["approval_policy"] = "on-request" }},
		{"credential mode", func(s *selectedConfigSnapshot) { s.Config["cli_auth_credentials_store"] = "keyring" }},
		{"nonlocal thread store", func(s *selectedConfigSnapshot) {
			s.Config["experimental_thread_store"] = map[string]any{"type": "in_memory", "id": "isolated"}
		}},
		{"permission selection", func(s *selectedConfigSnapshot) { s.Config["default_permissions"] = ":workspace" }},
		{"canonical state", func(s *selectedConfigSnapshot) { s.Config["sqlite_home"] = "/other" }},
		{"filesystem write", func(s *selectedConfigSnapshot) {
			p := configMap(configMap(s.Config["permissions"])["factory_selected"])
			configMap(p["filesystem"])["/fixture/protected"] = "write"
		}},
		{"network", func(s *selectedConfigSnapshot) {
			p := configMap(configMap(s.Config["permissions"])["factory_selected"])
			configMap(p["network"])["enabled"] = true
		}},
		{"trust", func(s *selectedConfigSnapshot) {
			configMap(configMap(s.Config["projects"])["/fixture/work"])["trust_level"] = "trusted"
		}},
		{"delegation", func(s *selectedConfigSnapshot) { configMap(s.Config["features"])["multi_agent"] = true }},
		{"other user layer", func(s *selectedConfigSnapshot) { s.Layers[0].Name.File = "/other/config.toml" }},
		{"active project layer", func(s *selectedConfigSnapshot) {
			s.Layers = append(s.Layers, s.Layers[0])
			s.Layers[1].Name.Type = "project"
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var snapshot selectedConfigSnapshot
			if err := json.Unmarshal([]byte(selectedConfigFixture), &snapshot); err != nil {
				t.Fatal(err)
			}
			args := []string{"exec", "-c", "features.multi_agent=false"}
			if err := validateSelectedConfiguration(snapshot, "/fixture/home", args); err != nil {
				t.Fatalf("baseline: %v", err)
			}
			tc.mutate(&snapshot)
			if err := validateSelectedConfiguration(snapshot, "/fixture/home", args); err == nil {
				t.Fatal("managed conflict accepted")
			}
		})
	}
}

func TestSelectedConfigurationChecksInheritedProfile(t *testing.T) {
	t.Parallel()
	var snapshot selectedConfigSnapshot
	if err := json.Unmarshal([]byte(selectedConfigFixture), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, config := range []map[string]any{snapshot.Config, snapshot.Layers[0].Config} {
		profiles := configMap(config["permissions"])
		profiles["factory_reviewer"] = map[string]any{"filesystem": map[string]any{"/": "read", "/fixture/cache": "write"}, "network": map[string]any{"enabled": false}}
		configMap(profiles["factory_selected"])["extends"] = "factory_reviewer"
	}
	if err := validateSelectedConfiguration(snapshot, "/fixture/home", nil); err != nil {
		t.Fatal(err)
	}
	parent := configMap(configMap(snapshot.Config["permissions"])["factory_reviewer"])
	configMap(parent["filesystem"])["/"] = "write"
	if err := validateSelectedConfiguration(snapshot, "/fixture/home", nil); err == nil {
		t.Fatal("widened inherited permission profile accepted")
	}
}
