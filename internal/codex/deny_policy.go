package codex

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/denypolicy"
)

// compiledDenyPolicy contains only native enforcement data, never instructions.
type compiledDenyPolicy struct {
	Rules             string
	DisableDelegation bool
}

func compileDenyPolicy(policy *denypolicy.Policy) (*compiledDenyPolicy, error) {
	if !policy.Selected() {
		return nil, nil
	}
	result := &compiledDenyPolicy{}
	var rules strings.Builder
	var faults []error
	for _, rule := range policy.Rules {
		switch rule.Tool {
		case "Bash":
			tokens, err := literalDenyPrefix(rule.Pattern)
			if err != nil {
				faults = append(faults, denyRuleError(rule, err.Error()))
				continue
			}
			quoted := make([]string, len(tokens))
			for i, token := range tokens {
				quoted[i] = strconv.Quote(token)
			}
			fmt.Fprintf(&rules, "prefix_rule(pattern=[%s], decision=\"forbidden\")\n", strings.Join(quoted, ", "))
		case "Agent":
			if rule.Pattern != "" {
				faults = append(faults, denyRuleError(rule, "named native-agent restrictions have no meaning-preserving mapping"))
				continue
			}
			result.DisableDelegation = true
		case "Read", "Edit":
			faults = append(faults, denyRuleError(rule, "selected file-tool restrictions have no meaning-preserving mapping"))
		default:
			faults = append(faults, denyRuleError(rule, "unsupported native tool restriction"))
		}
	}
	if len(policy.Rules) == 0 {
		return nil, denyPolicyError(policy, "selected policy contains no entries")
	}
	if err := errors.Join(faults...); err != nil {
		return nil, err
	}
	result.Rules = rules.String()
	return result, nil
}

func literalDenyPrefix(pattern string) ([]string, error) {
	if !strings.HasSuffix(pattern, ":*") {
		return nil, errors.New("command restriction must be a plain literal token prefix ending in :*")
	}
	prefix := strings.TrimSuffix(pattern, ":*")
	// An allowlist excludes shell syntax, globbing, escaping, assignment and Unicode
	// whitespace whose native parser semantics cannot be preserved by tokenization.
	for _, char := range prefix {
		literal := char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune(" /._-+:,@%", char)
		if !literal {
			return nil, errors.New("command prefix must contain only plain literal tokens; quoting, expansions, operators, assignments and wildcards are unsupported")
		}
	}
	tokens := strings.Fields(prefix)
	if len(tokens) == 0 || strings.TrimSpace(prefix) != prefix {
		return nil, errors.New("command prefix must be nonempty without surrounding whitespace")
	}
	return tokens, nil
}

func denyRuleError(rule denypolicy.Rule, reason string) error {
	return fmt.Errorf("%s: deny group %q entry %d %q: Codex cannot enforce this selection: %s; correct the entry or remove the selection", rule.Selection, rule.Group, rule.Index, rule.Original, reason)
}

func denyPolicyError(policy *denypolicy.Policy, reason string) error {
	if policy != nil && len(policy.Rules) > 0 {
		return denyRuleError(policy.Rules[0], reason)
	}
	return fmt.Errorf("runtime Codex cannot enforce selected deny groups: %s; correct or remove the selection", reason)
}

// Judge uses a separate isolated transport, not the graph coding launch path.
func selectedDenyRefusal(policy *denypolicy.Policy) error {
	if !policy.Selected() {
		return nil
	}
	return denyPolicyError(policy, "selected groups are unsupported by this execution path; select Claude")
}
