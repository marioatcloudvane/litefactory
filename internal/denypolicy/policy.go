// Package denypolicy parses operator deny groups independently of providers.
package denypolicy

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
)

// Group is one reusable list of explicit native tool restrictions.
type Group struct {
	Deny []string `yaml:"deny"`
}

// Catalogue maps case-sensitive operator names to deny groups.
type Catalogue map[string]Group

// Rule retains parsed native syntax and its selection provenance.
type Rule struct {
	Tool      string
	Pattern   string
	Original  string
	Group     string
	Selection string
	Index     int
}

// Policy is an owned snapshot of the additive selected restrictions.
type Policy struct {
	Groups []string
	Rules  []Rule
}

// Clone returns an independent snapshot, preserving nil for no selection.
func (p *Policy) Clone() *Policy {
	if p == nil {
		return nil
	}
	return &Policy{Groups: slices.Clone(p.Groups), Rules: slices.Clone(p.Rules)}
}

// Selected reports whether this snapshot contains selected groups.
func (p *Policy) Selected() bool { return p != nil && len(p.Groups) > 0 }

var namePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// ParseReferences validates and deduplicates comma-separated group names.
func ParseReferences(value string) ([]string, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	var names []string
	seen := map[string]bool{}
	for part := range strings.SplitSeq(value, ",") {
		name := strings.TrimSpace(part)
		if !namePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid deny group reference %q; use comma-separated group names", name)
		}
		if !seen[name] {
			names = append(names, name)
			seen[name] = true
		}
	}
	return names, nil
}

// ParseRule validates the common syntax without certifying provider support.
func ParseRule(value string) (Rule, error) {
	r := Rule{Original: value}
	if strings.TrimSpace(value) != value || value == "" || strings.ContainsFunc(value, unicode.IsControl) {
		return r, errors.New("entry must be nonempty and contain no surrounding whitespace or control characters")
	}
	if value == "Agent" {
		r.Tool = "Agent"
		return r, nil
	}
	tool, rest, ok := strings.Cut(value, "(")
	if !ok || !strings.HasSuffix(rest, ")") {
		return r, errors.New("expected Bash(pattern), Read(path), Edit(path), Agent or Agent(name)")
	}
	switch tool {
	case "Bash", "Read", "Edit", "Agent":
	default:
		return r, fmt.Errorf("unknown tool %q", tool)
	}
	pattern := strings.TrimSuffix(rest, ")")
	if strings.TrimSpace(pattern) == "" {
		return r, errors.New("tool pattern must be nonempty")
	}
	// Parentheses inside command patterns may be balanced, but a closing delimiter
	// cannot terminate and then reopen the entry.
	depth := 0
	for _, ch := range pattern {
		if ch == '(' {
			depth++
		}
		if ch == ')' {
			depth--
			if depth < 0 {
				return r, errors.New("unbalanced tool pattern")
			}
		}
	}
	if depth != 0 {
		return r, errors.New("unbalanced tool pattern")
	}
	r.Tool = tool
	r.Pattern = pattern
	return r, nil
}

// ValidateCatalogue checks every group, including currently unselected groups.
func ValidateCatalogue(c Catalogue) error {
	var faults []error
	names := make([]string, 0, len(c))
	for name := range c {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !namePattern.MatchString(name) {
			faults = append(faults, fmt.Errorf("settings.denyrules: invalid group name %q", name))
		}
		group := c[name]
		if len(group.Deny) == 0 {
			faults = append(faults, fmt.Errorf("settings.denyrules.%s.deny must contain at least one entry", name))
		}
		for i, entry := range group.Deny {
			if _, err := ParseRule(entry); err != nil {
				faults = append(faults, fmt.Errorf("settings.denyrules.%s.deny entry %d %q: %w", name, i+1, entry, err))
			}
		}
	}
	return errors.Join(faults...)
}

// Resolve forms a graph-first ordered union with exact-entry deduplication.
func Resolve(c Catalogue, graphRefs, nodeRefs, nodeName string) (*Policy, error) {
	p := &Policy{}
	groups := map[string]bool{}
	entries := map[string]bool{}
	for _, selection := range []struct{ value, source string }{{graphRefs, "graph lf_deny"}, {nodeRefs, fmt.Sprintf("node %q lf_deny", nodeName)}} {
		names, err := ParseReferences(selection.value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", selection.source, err)
		}
		for _, name := range names {
			if groups[name] {
				continue
			}
			groups[name] = true
			group, ok := c[name]
			if !ok {
				return nil, fmt.Errorf("%s: unknown deny group %q; define it in settings.denyrules", selection.source, name)
			}
			if len(group.Deny) == 0 {
				return nil, fmt.Errorf("%s: deny group %q has no entries", selection.source, name)
			}
			p.Groups = append(p.Groups, name)
			for i, entry := range group.Deny {
				rule, err := ParseRule(entry)
				if err != nil {
					return nil, fmt.Errorf("%s: group %q entry %d %q: %w", selection.source, name, i+1, entry, err)
				}
				if entries[entry] {
					continue
				}
				entries[entry] = true
				rule.Group = name
				rule.Selection = selection.source
				rule.Index = i + 1
				p.Rules = append(p.Rules, rule)
			}
		}
	}
	if !p.Selected() {
		return nil, nil
	}
	return p, nil
}
