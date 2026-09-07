package team

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/config"
)

// Validate aggregates semantic faults in a typed version 2 catalog.
func Validate(idx *Index) []error {
	if idx == nil {
		return []error{fmt.Errorf("team index is nil")}
	}
	var errs []error
	if idx.SchemaVersion != 2 {
		errs = append(errs, fmt.Errorf("schema_version: unsupported version %d; require 2", idx.SchemaVersion))
	}
	if idx.Teams == nil {
		errs = append(errs, &EmptyFieldError{Path: "index", Field: "teams"})
	}
	if idx.Shared == nil {
		errs = append(errs, &EmptyFieldError{Path: "index", Field: "shared"})
	}
	for id, a := range idx.Shared {
		errs = append(errs, validateAgentEntry(id, a, "shared."+id)...)
		if a.Ref != "" {
			errs = append(errs, fmt.Errorf("shared.%s: shared definitions cannot reference another agent", id))
		}
		if a.ID != "" && a.ID != id {
			errs = append(errs, fmt.Errorf("shared.%s: id must match map key", id))
		}
	}
	for name, t := range idx.Teams {
		p := "teams." + name
		// A scoped override identifies one (team, id), so every appearance of
		// that inline identity must describe the same agent across modes.
		definitions := make(map[string]AgentEntry)
		locations := make(map[string]string)
		if !KeyPattern.MatchString(name) {
			errs = append(errs, &KeyError{Path: p, Key: name})
		}
		if strings.TrimSpace(t.Desc) == "" {
			errs = append(errs, &EmptyFieldError{Path: p, Field: "desc"})
		}
		for _, mode := range Modes() {
			entries, _ := t.Mode(mode)
			if entries == nil {
				errs = append(errs, fmt.Errorf("%s.%s: required array (use [] for root-only)", p, mode))
			}
			seen := map[string]bool{}
			for i, a := range entries {
				loc := fmt.Sprintf("%s.%s[%d]", p, mode, i)
				id := a.ID
				if a.Ref != "" {
					id = a.Ref
					if _, ok := idx.Shared[id]; !ok {
						errs = append(errs, fmt.Errorf("%s: unknown shared ref %q", loc, id))
					}
				} else {
					errs = append(errs, validateAgentEntry(id, a, loc)...)
					if previous, exists := definitions[id]; exists {
						if previous.Desc != a.Desc || previous.Path != a.Path || previous.Tier != a.Tier || !slices.Equal(previous.Skills, a.Skills) {
							errs = append(errs, fmt.Errorf("%s: inline agent %q conflicts with %s", loc, id, locations[id]))
						}
					} else {
						definitions[id] = a
						locations[id] = loc
					}
				}
				if seen[id] {
					errs = append(errs, &DuplicateKeyError{Key: id, Locations: []string{p + "." + mode}})
				}
				seen[id] = true
			}
		}
	}
	return errs
}
func validateAgentEntry(id string, a AgentEntry, p string) []error {
	var errs []error
	if !KeyPattern.MatchString(id) {
		errs = append(errs, &KeyError{Path: p, Key: id})
	}
	for field, value := range map[string]string{"desc": a.Desc, "path": a.Path, "tier": a.Tier} {
		if strings.TrimSpace(value) == "" {
			errs = append(errs, &EmptyFieldError{Path: p, Field: field})
		}
		if config.ContainsForbiddenFlag(value) {
			errs = append(errs, &ForbiddenFlagInIndexError{Path: p, Field: field})
		}
	}
	if a.Tier != "" && !config.Tier(a.Tier).Valid() {
		errs = append(errs, fmt.Errorf("%s.tier: unknown capability tier", p))
	}
	for _, id := range a.Skills {
		if !KeyPattern.MatchString(id) {
			errs = append(errs, &KeyError{Path: p + ".skills", Key: id})
		}
	}
	return errs
}

// Roster resolves exactly the ordered mode array; unused shared entries stay out.
func (idx *Index) Roster(name, mode string) ([]AgentEntry, error) {
	t, ok := idx.Teams[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTeam, name)
	}
	entries, ok := t.Mode(mode)
	if !ok {
		return nil, fmt.Errorf("unknown mode %q", mode)
	}
	out := make([]AgentEntry, 0, len(entries))
	for _, a := range entries {
		if a.Ref != "" {
			id := a.Ref
			var ok bool
			a, ok = idx.Shared[id]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrUnknownAgentKey, id)
			}
			a.ID = id
		}
		a.Skills = append([]string(nil), a.Skills...)
		out = append(out, a)
	}
	return out, nil
}
