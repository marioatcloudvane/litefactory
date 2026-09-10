package planbrief

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// DecodeTypedContent validates exact JSON keys, required collections and primitive
// types against a typed content struct. It does not authorize semantic references.
func DecodeTypedContent(data []byte, dst any) error {
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer || t.Elem().Kind() != reflect.Struct {
		return errors.New("content destination must be a struct pointer")
	}
	if len(data) > MaxProposalBytes || !utf8.Valid(data) {
		return errors.New("content exceeds byte budget or is not UTF-8")
	}
	if !json.Valid(data) {
		return errors.New("content: malformed JSON")
	}
	faults := checkContentShape(data, t.Elem(), "$")
	if err := errors.Join(faults...); err != nil {
		return err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return fmt.Errorf("decode content: %w", err)
	}
	return nil
}

func checkContentShape(data []byte, t reflect.Type, path string) []error {
	data = bytes.TrimSpace(data)
	if bytes.Equal(data, []byte("null")) {
		return []error{fmt.Errorf("%s: expected %s, got null", path, t.Kind())}
	}
	switch t.Kind() {
	case reflect.Struct:
		if len(data) == 0 || data[0] != '{' {
			return []error{fmt.Errorf("%s: expected object", path)}
		}
		d := json.NewDecoder(bytes.NewReader(data))
		if _, err := d.Token(); err != nil {
			return []error{err}
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fields[f.Tag.Get("json")] = f.Type
		}
		seen := map[string]bool{}
		var faults []error
		for d.More() {
			token, err := d.Token()
			if err != nil {
				return append(faults, err)
			}
			key, ok := token.(string)
			if !ok {
				return append(faults, fmt.Errorf("%s: expected key", path))
			}
			var value json.RawMessage
			if err := d.Decode(&value); err != nil {
				return append(faults, err)
			}
			if seen[key] {
				faults = append(faults, fmt.Errorf("%s.%s: duplicate key", path, key))
			}
			seen[key] = true
			field, ok := fields[key]
			if !ok {
				faults = append(faults, fmt.Errorf("%s.%s: unknown key", path, key))
				continue
			}
			faults = append(faults, checkContentShape(value, field, path+"."+key)...)
		}
		for i := 0; i < t.NumField(); i++ {
			key := t.Field(i).Tag.Get("json")
			if !seen[key] {
				faults = append(faults, fmt.Errorf("%s.%s: required field missing", path, key))
			}
		}
		return faults
	case reflect.Slice:
		if len(data) == 0 || data[0] != '[' {
			return []error{fmt.Errorf("%s: expected array", path)}
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(data, &entries); err != nil {
			return []error{err}
		}
		var faults []error
		for i, v := range entries {
			faults = append(faults, checkContentShape(v, t.Elem(), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return faults
	case reflect.String:
		var value string
		if err := json.Unmarshal(data, &value); err != nil {
			return []error{fmt.Errorf("%s: expected string", path)}
		}
		if strings.TrimSpace(value) == "" {
			return []error{fmt.Errorf("%s: string must be nonblank", path)}
		}
		return nil
	default:
		return []error{fmt.Errorf("%s: unsupported schema type %s", path, t)}
	}
}

func validateContentGraph(path string, graph map[string][]string) []error {
	keys := make([]string, 0, len(graph))
	for key := range graph {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var faults []error
	for _, key := range keys {
		seen := map[string]bool{}
		for i, dep := range graph[key] {
			_, exists := graph[dep]
			if !exists || dep == key || seen[dep] {
				faults = append(faults, fmt.Errorf("%s.%s.dependencies[%d]: unknown, self or duplicate reference", path, key, i))
			}
			seen[dep] = true
		}
	}
	state := map[string]int{}
	var visit func(string) bool
	visit = func(key string) bool {
		if state[key] == 1 {
			return false
		}
		if state[key] == 2 {
			return true
		}
		state[key] = 1
		for _, dep := range graph[key] {
			if !visit(dep) {
				return false
			}
		}
		state[key] = 2
		return true
	}
	for _, key := range keys {
		if !visit(key) {
			faults = append(faults, fmt.Errorf("%s: dependency cycle", path))
			break
		}
	}
	return faults
}

// validContentUTF8 checks Go values before encoding/json can replace invalid bytes.
func validContentUTF8(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.String:
		return utf8.ValidString(v.String())
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !validContentUTF8(v.Field(i)) {
				return false
			}
		}
	case reflect.Slice:
		for i := 0; i < v.Len(); i++ {
			if !validContentUTF8(v.Index(i)) {
				return false
			}
		}
	}
	return true
}
