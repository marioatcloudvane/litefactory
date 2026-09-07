package team

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// Decode refuses duplicate keys and aggregates shape and semantic errors.
func Decode(data []byte) (*Index, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueJSON(d, "index"); err != nil {
		return nil, err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("team index: trailing JSON")
	}
	if errs := checkUnknownFields(data); len(errs) > 0 {
		return nil, joinSorted(errs)
	}
	var idx Index
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&idx); err != nil {
		return nil, fmt.Errorf("decode team index: %w", err)
	}
	if errs := Validate(&idx); len(errs) > 0 {
		return nil, joinSorted(errs)
	}
	return &idx, nil
}
func uniqueJSON(d *json.Decoder, p string) error {
	t, err := d.Token()
	if err != nil {
		return fmt.Errorf("%s: %w", p, err)
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		child := p + "[]"
		if delim == '{' {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok {
				return fmt.Errorf("%s: invalid key", p)
			}
			if seen[key] {
				return &DuplicateKeyError{Key: key, Locations: []string{p}}
			}
			seen[key] = true
			child = p + "." + key
		}
		if err := uniqueJSON(d, child); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}
func checkUnknownFields(data []byte) []error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return []error{err}
	}
	errs := unknown(top, "index", []string{"schema_version", "teams", "shared"})
	var teams map[string]json.RawMessage
	if err := json.Unmarshal(top["teams"], &teams); err != nil {
		return append(errs, fmt.Errorf("teams: %w", err))
	}
	for name, raw := range teams {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			errs = append(errs, err)
			continue
		}
		p := "teams." + name
		errs = append(errs, unknown(obj, p, append(Modes(), "desc"))...)
		for _, mode := range Modes() {
			var entries []json.RawMessage
			if err := json.Unmarshal(obj[mode], &entries); err != nil {
				errs = append(errs, fmt.Errorf("%s.%s: required array", p, mode))
				continue
			}
			for i, entry := range entries {
				errs = append(errs, checkEntry(entry, fmt.Sprintf("%s.%s[%d]", p, mode, i))...)
			}
		}
	}
	var shared map[string]json.RawMessage
	if err := json.Unmarshal(top["shared"], &shared); err != nil {
		errs = append(errs, fmt.Errorf("shared: %w", err))
	}
	for id, raw := range shared {
		errs = append(errs, checkEntry(raw, "shared."+id)...)
	}
	return errs
}
func checkEntry(raw json.RawMessage, p string) []error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return []error{fmt.Errorf("%s: %w", p, err)}
	}
	errs := unknown(obj, p, []string{"ref", "id", "desc", "path", "tier", "skills"})
	if _, ok := obj["ref"]; ok && len(obj) != 1 {
		errs = append(errs, fmt.Errorf("%s: ref cannot accompany inline fields", p))
	}
	return errs
}
func unknown(obj map[string]json.RawMessage, p string, keys []string) []error {
	allowed := map[string]bool{}
	for _, k := range keys {
		allowed[k] = true
	}
	var errs []error
	for k := range obj {
		if !allowed[k] {
			errs = append(errs, &UnknownFieldError{Path: p + "." + k})
		}
	}
	return errs
}
func joinSorted(errs []error) error {
	sort.Slice(errs, func(i, j int) bool { return errs[i].Error() < errs[j].Error() })
	return errors.Join(errs...)
}
