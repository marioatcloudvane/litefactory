package codex

import (
	"fmt"
	"github.com/cloudvane/litefactory/internal/denypolicy"
)

func selectedDenyRefusal(policy *denypolicy.Policy) error {
	if !policy.Selected() {
		return nil
	}
	if len(policy.Rules) > 0 {
		rule := policy.Rules[0]
		return fmt.Errorf("%s: deny group %q entry %d %q: Codex cannot enforce selected deny groups yet; select Claude or remove the selection", rule.Selection, rule.Group, rule.Index, rule.Original)
	}
	return fmt.Errorf("selected deny groups %v: Codex cannot enforce these yet; select Claude or remove the selection", policy.Groups)
}
