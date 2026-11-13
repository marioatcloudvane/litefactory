package turn

import (
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func loadDenyCatalogue(opts Options) (denypolicy.Catalogue, error) {
	load := opts.LoadDenyConfig
	if load == nil {
		load = config.Load
	}
	cfg, err := load(opts.Paths)
	if err != nil {
		return nil, fmt.Errorf("reload settings for selected deny groups: %w", err)
	}
	if cfg == nil {
		return nil, errors.New("reload settings for selected deny groups returned no configuration")
	}
	if err := denypolicy.ValidateCatalogue(cfg.Settings.DenyRules); err != nil {
		return nil, err
	}
	return cfg.Settings.DenyRules, nil
}

// resolveAttemptDeny reads current settings only for a selected attempt. Its
// owned result survives all provider retries and refused-resume cold fallback.
func resolveAttemptDeny(opts Options, graphRefs, nodeRefs, node string) (*denypolicy.Policy, error) {
	if strings.TrimSpace(graphRefs) == "" && strings.TrimSpace(nodeRefs) == "" {
		return nil, nil
	}
	catalogue, err := loadDenyCatalogue(opts)
	if err != nil {
		return nil, fmt.Errorf("node %q lf_deny: %w", node, err)
	}
	return denypolicy.Resolve(catalogue, graphRefs, nodeRefs, node)
}

func admitDenySelections(opts Options, profile *graphexec.Profile) error {
	selected := strings.TrimSpace(profile.Attrs[graphexec.DenyAttribute]) != ""
	for _, node := range profile.Nodes {
		selected = selected || strings.TrimSpace(node.Attrs[graphexec.DenyAttribute]) != ""
	}
	if !selected {
		return nil
	}
	catalogue, err := loadDenyCatalogue(opts)
	if err != nil {
		return err
	}
	return graphexec.ValidateDenySelections(profile, catalogue)
}
