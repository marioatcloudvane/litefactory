package config

import (
	"errors"
	"strings"
)

// WorkflowRegistration explicitly installs a version of a classified built-in adapter.
// Missing registrations leave the two built-in contracts available for discovery.
type WorkflowRegistration struct {
	Ref            string `yaml:"ref"`
	Version        string `yaml:"version"`
	Adapter        string `yaml:"adapter"`
	Name           string `yaml:"name"`
	Purpose        string `yaml:"purpose"`
	BindingProject string `yaml:"binding_project"`
	ProjectMode    string `yaml:"project_mode"`
	FixedProject   string `yaml:"fixed_project"`
}

// WorkflowNoticeDestination is an explicit owner-approved Discord association.
type WorkflowNoticeDestination struct {
	Project   string `yaml:"project"`
	ChannelID string `yaml:"channel_id"`
}

func validateWorkflowRegistrations(entries []WorkflowRegistration, destinations []WorkflowNoticeDestination) error {
	var errs []error
	if len(entries) > 100 || len(destinations) > 100 {
		errs = append(errs, errors.New("workflow configuration exceeds registration or destination limits"))
	}
	seen := map[string]bool{}
	for _, v := range entries {
		key := v.Ref + "\x00" + v.Version
		if strings.TrimSpace(v.Ref) == "" || len(v.Ref) > 128 || v.Version != "1" || seen[key] || v.Ref == "log-analysis" || v.Ref == "standards-authoring" {
			errs = append(errs, errors.New("workflow registration requires a unique reference and supported version"))
		}
		seen[key] = true
		if v.Adapter != "log-analysis" && v.Adapter != "standards-authoring" {
			errs = append(errs, errors.New("workflow adapter is not installed and classified"))
		}
		switch v.ProjectMode {
		case "fixed":
			if v.FixedProject == "" {
				errs = append(errs, errors.New("fixed workflow requires a project"))
			}
		case "required", "optional", "none":
			if v.FixedProject != "" {
				errs = append(errs, errors.New("only fixed workflows may specify fixed_project"))
			}
		default:
			errs = append(errs, errors.New("workflow requires an explicit project mode"))
		}
		if v.Adapter == "log-analysis" && (v.ProjectMode == "optional" || v.ProjectMode == "none") {
			errs = append(errs, errors.New("log analysis requires repository inputs"))
		}
		for _, ref := range []string{v.BindingProject, v.FixedProject} {
			if ref != "" && !workflowProjectRef(ref) {
				errs = append(errs, errors.New("workflow project must use a registered forge:owner/name reference"))
			}
		}
	}
	seen = map[string]bool{}
	for _, v := range destinations {
		if v.ChannelID == "" || strings.ContainsAny(v.ChannelID, " \r\n\t") || seen[v.Project] || v.Project != "" && !workflowProjectRef(v.Project) {
			errs = append(errs, errors.New("workflow notice destination requires a unique explicit project or factory association and channel"))
		}
		seen[v.Project] = true
	}
	return errors.Join(errs...)
}
func workflowProjectRef(ref string) bool {
	forge, slug, ok := strings.Cut(ref, ":")
	owner, name, slash := strings.Cut(slug, "/")
	return ok && (forge == "github" || forge == "linear") && slash && owner != "" && name != "" && !strings.ContainsAny(owner, " \r\n\t") && !strings.ContainsAny(name, "/ \r\n\t")
}
