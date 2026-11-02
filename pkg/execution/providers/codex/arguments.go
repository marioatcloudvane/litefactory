package codex

import "strconv"

// AdvisoryArguments builds stateless read-only native calls. The caller supplies
// any already-selected skill/MCP overrides as native extra arguments.
func AdvisoryArguments(model string, extra []string) []string {
	args := []string{"exec", "--json", "--ignore-user-config", "--ignore-rules", "--sandbox", "read-only"}
	args = append(args, extra...)
	if model != "" {
		args = append(args, "--model", model)
	}
	return append(args, "-")
}

// ConversationOptions carries prepared isolation configuration selected by the
// caller. Session is the exact native resume handle; no session is invented.
type ConversationOptions struct {
	Session, Model, Effort string
	Configurations         []string
	DisabledFeatures       []string
}

// ConversationArguments preserves the native exec/resume flag ordering used by
// recorded-session callers. The caller owns the permission profile definition.
func ConversationArguments(options ConversationOptions) []string {
	args := []string{"exec"}
	if options.Session != "" {
		args = append(args, "resume")
	}
	args = append(args, "--json", "--strict-config", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--model", options.Model)
	for _, configuration := range options.Configurations {
		args = append(args, "-c", configuration)
	}
	if options.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(options.Effort))
	}
	for _, feature := range options.DisabledFeatures {
		args = append(args, "-c", "features."+feature+"=false")
	}
	if options.Session != "" {
		args = append(args, options.Session)
	}
	return append(args, "-")
}

// ReasoningSummaryArguments encodes a provider-native summary selection.
func ReasoningSummaryArguments(summary string) []string {
	if summary == "" {
		return nil
	}
	return []string{"-c", "model_reasoning_summary=" + strconv.Quote(summary)}
}

// ScopedOptions carries native flags after the caller selects isolation policy.
// Configurations are complete native key/value settings in their supplied order.
type ScopedOptions struct {
	Model, Effort, ReasoningSummary string
	BeforeModelConfigurations       []string
	NativeArguments                 []string
	Configurations                  []string
	DisabledFeatures                []string
	ExtraArgs                       []string
}

// ScopedArguments encodes ephemeral native calls without selecting factory tools
// or permission profiles. BeforeModelConfigurations preserves native argv order.
func ScopedArguments(options ScopedOptions) []string {
	args := []string{"exec", "--json", "--strict-config", "--ignore-user-config", "--ignore-rules", "--skip-git-repo-check", "--ephemeral"}
	args = append(args, ConfigurationArguments(options.BeforeModelConfigurations)...)
	args = append(args, "--model", options.Model)
	args = append(args, options.NativeArguments...)
	args = append(args, ConfigurationArguments(options.Configurations)...)
	for _, feature := range options.DisabledFeatures {
		args = append(args, "-c", "features."+feature+"=false")
	}
	if options.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(options.Effort))
	}
	args = append(args, ReasoningSummaryArguments(options.ReasoningSummary)...)
	args = append(args, options.ExtraArgs...)
	return append(args, "-")
}

// ConfigurationArguments encodes caller-selected native configuration values.
func ConfigurationArguments(settings []string) []string {
	args := make([]string, 0, 2*len(settings))
	for _, setting := range settings {
		args = append(args, "-c", setting)
	}
	return args
}
