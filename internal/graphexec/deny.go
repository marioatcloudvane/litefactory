package graphexec

import (
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/denypolicy"
)

// DenyAttribute names the additive graph and agent-node group selection.
const DenyAttribute = "lf_deny"

// ValidateDenySelections resolves configured names without coupling static DOT
// validation to an operator's catalogue. Call Validate first for activity checks.
func ValidateDenySelections(p *Profile, catalogue denypolicy.Catalogue) error {
	if p == nil {
		return errors.New("graph profile is nil")
	}
	if _, err := denypolicy.Resolve(catalogue, p.Attrs[DenyAttribute], "", ""); err != nil {
		return err
	}
	for _, node := range p.Nodes {
		if _, err := denypolicy.Resolve(catalogue, p.Attrs[DenyAttribute], node.Attrs[DenyAttribute], node.Name); err != nil {
			return fmt.Errorf("node %q: %w", node.Name, err)
		}
	}
	return nil
}
