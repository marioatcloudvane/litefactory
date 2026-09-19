package graphexec

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// InvokeAgentConfig is a decoded vendor-neutral reference, not an agent handler.
type InvokeAgentConfig struct {
	AgentMarkdownRef string
	ModelTier        config.Tier
}

// DecodeInvokeAgentConfig requires both reserved attributes, validates their
// syntax and does not resolve or open the markdown reference.
func DecodeInvokeAgentConfig(attrs map[string]string) (InvokeAgentConfig, error) {
	c := InvokeAgentConfig{
		AgentMarkdownRef: attrs["lf_agent_markdown"],
		ModelTier:        config.Tier(attrs["lf_model_tier"]),
	}
	var faults []error
	if err := validateMarkdownRef(c.AgentMarkdownRef); err != nil {
		faults = append(faults, err)
	}
	if err := validateTier(string(c.ModelTier)); err != nil {
		faults = append(faults, err)
	}
	return c, errors.Join(faults...)
}

func validateTier(value string) error {
	if !config.Tier(value).Valid() {
		return fmt.Errorf("lf_model_tier %q: must be flagship, balanced or fast", value)
	}
	return nil
}

func validateMarkdownRef(value string) error {
	valid := value != "" && !strings.HasPrefix(value, "/") && !strings.ContainsAny(value, "\\\x00")
	if len(value) >= 2 && value[1] == ':' {
		valid = false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." || part == ".." {
			valid = false
		}
	}
	if !valid {
		return fmt.Errorf("lf_agent_markdown %q: must be a nonempty catalog-relative slash path without drive prefixes or dot segments", value)
	}
	return nil
}

func validateInvokeAttrs(attrs map[string]string) error {
	var faults []error
	if value, exists := attrs["lf_agent_markdown"]; exists {
		faults = append(faults, validateMarkdownRef(value))
	}
	if value, exists := attrs["lf_model_tier"]; exists {
		faults = append(faults, validateTier(value))
	}
	return errors.Join(faults...)
}
