package config

import (
	"strings"
	"testing"
)

func TestDurableWorkflowConfigurationDryRun(t *testing.T) {
	cfg, err := Decode(strings.NewReader(`remote_connector:
  workflows:
    - ref: book-review
      version: "1"
      adapter: standards-authoring
      project_mode: none
    - ref: contextual-review
      version: "1"
      adapter: standards-authoring
      project_mode: optional
    - ref: daily-review
      version: "1"
      adapter: log-analysis
      project_mode: fixed
      binding_project: github:acme/app
      fixed_project: github:acme/app
`))
	if err != nil {
		t.Fatal(err)
	}
	base := canonicalConfig(t)
	base.RemoteConnector = cfg.RemoteConnector
	if err = base.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.RemoteConnector.Workflows) != 3 || len(cfg.RemoteConnector.WorkflowNotices) != 0 {
		t.Fatalf("registrations or absent Discord default changed: %+v", cfg.RemoteConnector)
	}
	if cfg.RemoteConnector.Enabled {
		t.Fatal("workflow registration enabled remote connector implicitly")
	}
	cfg, err = Decode(strings.NewReader(`remote_connector:
  workflow_notices:
    - channel_id: "factory-channel"
    - project: github:acme/app
      channel_id: "project-channel"
`))
	if err != nil {
		t.Fatal(err)
	}
	if err = cfg.RemoteConnector.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(cfg.RemoteConnector.WorkflowNotices) != 2 || cfg.RemoteConnector.WorkflowNotices[0].Project != "" {
		t.Fatal("explicit factory/project notice associations changed")
	}
}
func TestDurableWorkflowConfigurationRejectsUnsafeRegistrations(t *testing.T) {
	valid := WorkflowRegistration{Ref: "review", Version: "1", Adapter: "standards-authoring", ProjectMode: "optional"}
	for name, change := range map[string]func(*WorkflowRegistration){"reserved built-in reference": func(v *WorkflowRegistration) { v.Ref = "log-analysis" }, "unknown adapter": func(v *WorkflowRegistration) { v.Adapter = "shell" }, "unknown version": func(v *WorkflowRegistration) { v.Version = "2" }, "implicit target": func(v *WorkflowRegistration) { v.ProjectMode = "" }, "missing fixed target": func(v *WorkflowRegistration) { v.ProjectMode = "fixed" }, "nonfixed target override": func(v *WorkflowRegistration) { v.FixedProject = "github:acme/app" }, "log without repository": func(v *WorkflowRegistration) { v.Adapter = "log-analysis" }, "invalid binding": func(v *WorkflowRegistration) { v.BindingProject = "default" }} {
		t.Run(name, func(t *testing.T) {
			entry := valid
			change(&entry)
			if err := validateWorkflowRegistrations([]WorkflowRegistration{entry}, nil); err == nil {
				t.Fatal("unsafe registration accepted")
			}
		})
	}
	if err := validateWorkflowRegistrations([]WorkflowRegistration{valid, valid}, nil); err == nil {
		t.Fatal("duplicate installed identity accepted")
	}
	if err := validateWorkflowRegistrations(nil, []WorkflowNoticeDestination{{ChannelID: "one"}, {ChannelID: "two"}}); err == nil {
		t.Fatal("ambiguous factory destination accepted")
	}
}
