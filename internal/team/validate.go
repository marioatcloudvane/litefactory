package team

import (
	"fmt"
	"sort"
)

// Validate checks semantic rules against an already type-decoded Index:
// filename-safety on every team name and agent key, non-empty Source/Dest
// on every agent entry, and that no agent key appears in more than one
// location (AC-5). It never re-checks JSON shape — [Decode]'s pass one
// already guarantees the shape is clean before Validate ever runs.
//
// TODO(US-14): once config.ContainsForbiddenFlag exists, call it here
// against each entry's Source and Dest and fold any hit into the returned
// errors the same way the checks below do. Nothing in this story's own
// acceptance criteria requires it yet.
func Validate(idx *Index) []error {
	var errs []error
	locations := make(map[string][]string)

	for i, t := range idx.Teams {
		path := teamPath(t, i)
		if !KeyPattern.MatchString(t.Name) {
			errs = append(errs, &KeyError{Path: path + ".name", Key: t.Name})
		}
		for key, entry := range t.Agents {
			entryPath := path + ".agents." + key
			errs = append(errs, validateAgentEntry(key, entry, entryPath)...)
			locations[key] = append(locations[key], entryPath)
		}
	}

	for key, entry := range idx.Shared {
		entryPath := "shared." + key
		errs = append(errs, validateAgentEntry(key, entry, entryPath)...)
		locations[key] = append(locations[key], entryPath)
	}

	for key, locs := range locations {
		if len(locs) < 2 {
			continue
		}
		sorted := append([]string(nil), locs...)
		sort.Strings(sorted)
		errs = append(errs, &DuplicateKeyError{Key: key, Locations: sorted})
	}

	return errs
}

// validateAgentEntry checks one agent entry's key and fields, regardless of
// whether it came from a team's inline map or from shared.
func validateAgentEntry(key string, entry AgentEntry, path string) []error {
	var errs []error
	if !KeyPattern.MatchString(key) {
		errs = append(errs, &KeyError{Path: path, Key: key})
	}
	if entry.Source == "" {
		errs = append(errs, &EmptyFieldError{Path: path, Field: "source"})
	}
	if entry.Dest == "" {
		errs = append(errs, &EmptyFieldError{Path: path, Field: "dest"})
	}
	return errs
}

// teamPath is the dotted-path prefix for a team: its name when non-empty,
// its array index otherwise. This mirrors the fallback [checkTeamEntry]
// uses during pass one, before the name itself has been validated.
func teamPath(t TeamEntry, index int) string {
	if t.Name == "" {
		return fmt.Sprintf("teams[%d]", index)
	}
	return "teams." + t.Name
}
