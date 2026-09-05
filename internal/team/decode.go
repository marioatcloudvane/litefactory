package team

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// Decode parses and validates an agent team index document. It runs in two
// passes (see doc.go): an unknown-field check over the raw JSON shape, and,
// only once that is clean, a typed decode followed by semantic validation.
// Every fault either pass finds is collected and returned together via
// errors.Join, in a stable sorted order, rather than stopping at the first
// one (CFG-5).
func Decode(data []byte) (*Index, error) {
	if errs := checkUnknownFields(data); len(errs) > 0 {
		return nil, joinSorted(errs)
	}

	var idx Index
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode team index: %w", err)
	}

	if errs := Validate(&idx); len(errs) > 0 {
		return nil, joinSorted(errs)
	}
	return &idx, nil
}

// checkUnknownFields is pass one: a generic walk over the raw JSON shape,
// checked against the known-key table at every nesting level. It reports
// every unknown field it finds, each carrying the dotted path to it.
func checkUnknownFields(data []byte) []error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return []error{fmt.Errorf("parse team index: %w", err)}
	}

	var errs []error
	for key := range top {
		if key != "teams" && key != "shared" {
			errs = append(errs, &UnknownFieldError{Path: key})
		}
	}
	if raw, ok := top["teams"]; ok {
		errs = append(errs, checkTeamsField(raw)...)
	}
	if raw, ok := top["shared"]; ok {
		errs = append(errs, checkAgentsMap(raw, "shared")...)
	}
	return errs
}

// checkTeamsField walks the teams array.
func checkTeamsField(raw json.RawMessage) []error {
	var teams []json.RawMessage
	if err := json.Unmarshal(raw, &teams); err != nil {
		return []error{fmt.Errorf("teams: %w", err)}
	}
	var errs []error
	for i, rawTeam := range teams {
		errs = append(errs, checkTeamEntry(rawTeam, i)...)
	}
	return errs
}

// checkTeamEntry walks one team object. The path it reports under uses the
// team's own name once it can read one as a non-empty string, and falls
// back to the array index otherwise — a name has not been validated yet at
// this point, but any string is better for a person reading the error than
// an index, and an unreadable or missing name leaves no better option than
// the index (see doc.go).
func checkTeamEntry(raw json.RawMessage, index int) []error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []error{fmt.Errorf("teams[%d]: %w", index, err)}
	}

	teamPath := fmt.Sprintf("teams[%d]", index)
	if nameRaw, ok := obj["name"]; ok {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err == nil && name != "" {
			teamPath = "teams." + name
		}
	}

	var errs []error
	for key := range obj {
		if key != "name" && key != "agents" {
			errs = append(errs, &UnknownFieldError{Path: teamPath + "." + key})
		}
	}
	if agentsRaw, ok := obj["agents"]; ok {
		errs = append(errs, checkAgentsMap(agentsRaw, teamPath+".agents")...)
	}
	return errs
}

// checkAgentsMap walks a map of agent key to agent entry — either a team's
// inline agents map or the top-level shared map.
func checkAgentsMap(raw json.RawMessage, path string) []error {
	var agents map[string]json.RawMessage
	if err := json.Unmarshal(raw, &agents); err != nil {
		return []error{fmt.Errorf("%s: %w", path, err)}
	}
	var errs []error
	for key, rawEntry := range agents {
		errs = append(errs, checkAgentEntry(rawEntry, path+"."+key)...)
	}
	return errs
}

// checkAgentEntry walks one {source, dest} object.
func checkAgentEntry(raw json.RawMessage, path string) []error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []error{fmt.Errorf("%s: %w", path, err)}
	}
	var errs []error
	for key := range obj {
		if key != "source" && key != "dest" {
			errs = append(errs, &UnknownFieldError{Path: path + "." + key})
		}
	}
	return errs
}

// joinSorted sorts errs by message before joining them, so that the
// aggregate error's text is stable across runs even though the maps that
// produced it were walked in Go's randomized order.
func joinSorted(errs []error) error {
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errors.Join(errs...)
}
