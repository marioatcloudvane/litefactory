package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// The syntax tree preserves duplicate keys. A normal map decode would silently
// erase the very ambiguity the submission boundary must diagnose.
type contributionMember struct {
	name  string
	value *contributionNode
}
type contributionNode struct {
	kind   string
	text   string
	raw    json.RawMessage
	fields []contributionMember
	items  []*contributionNode
}

func (n *contributionNode) field(name string) *contributionNode {
	if n == nil {
		return nil
	}
	for _, f := range n.fields {
		if f.name == name {
			return f.value
		}
	}
	return nil
}
func (n *contributionNode) stringField(name string) string {
	v := n.field(name)
	if v != nil && v.kind == "string" {
		return v.text
	}
	return ""
}
func (n *contributionNode) decode(name string, target interface{}) bool {
	v := n.field(name)
	if v == nil || v.kind != "object" {
		return false
	}
	// Substitute invalid leaf shapes only for diagnostic traversal. The real
	// result is never returned on a shape failure. This keeps an invalid
	// evidence field from hiding an unrelated unknown criterion or bad status.
	shape := schemaForContribution(reflect.TypeOf(target).Elem())
	return json.Unmarshal(shape.diagnosticJSON(v), target) == nil
}

func readContributionJSON(data []byte, result *ContributionDiagnostics) *contributionNode {
	if !utf8.Valid(data) {
		result.add("$", "encoding", "UTF-8 JSON", "invalid UTF-8", "Encode the complete contribution as UTF-8 JSON.")
		result.Complete = false
		result.UncheckedPaths = append(result.UncheckedPaths, "$")
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	node, err := readContributionNode(decoder, data, 0)
	if err == nil {
		if _, extra := decoder.Token(); extra != io.EOF {
			err = fmt.Errorf("trailing or invalid JSON")
		}
	}
	if err != nil {
		result.add("$", "syntax", "one complete JSON object", "malformed JSON", "Correct JSON syntax or excessive nesting before deeper validation.")
		result.Complete = false
		result.UncheckedPaths = append(result.UncheckedPaths, "$")
		return nil
	}
	return node
}
func readContributionNode(decoder *json.Decoder, data []byte, depth int) (*contributionNode, error) {
	if depth > 32 {
		return nil, fmt.Errorf("nesting limit")
	}
	start := decoder.InputOffset()
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	n := &contributionNode{}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			n.kind = "object"
			for decoder.More() {
				nameToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				name, ok := nameToken.(string)
				if !ok {
					return nil, fmt.Errorf("object key")
				}
				item, err := readContributionNode(decoder, data, depth+1)
				if err != nil {
					return nil, err
				}
				n.fields = append(n.fields, contributionMember{name, item})
			}
		case '[':
			n.kind = "array"
			for decoder.More() {
				item, err := readContributionNode(decoder, data, depth+1)
				if err != nil {
					return nil, err
				}
				n.items = append(n.items, item)
			}
		default:
			return nil, fmt.Errorf("unexpected delimiter")
		}
		if _, err := decoder.Token(); err != nil {
			return nil, err
		}
	case string:
		n.kind = "string"
		n.text = value
	case json.Number:
		n.kind = "number"
	case bool:
		n.kind = "boolean"
	case nil:
		n.kind = "null"
	default:
		return nil, fmt.Errorf("unsupported token")
	}
	n.raw = bytes.TrimLeft(data[start:decoder.InputOffset()], " \t\r\n,:")
	return n, nil
}

type contributionSchema struct {
	kind     string
	fields   map[string]*contributionSchema
	element  *contributionSchema
	opaque   bool
	enum     []string
	optional bool
}

func schemaForContribution(t reflect.Type) *contributionSchema {
	s := &contributionSchema{}
	switch t.Kind() {
	case reflect.Struct:
		s.kind = "object"
		s.fields = map[string]*contributionSchema{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			s.fields[strings.Split(f.Tag.Get("json"), ",")[0]] = schemaForContribution(f.Type)
		}
	case reflect.Slice:
		s.kind = "array"
		s.element = schemaForContribution(t.Elem())
	case reflect.String:
		s.kind = "string"
	case reflect.Bool:
		s.kind = "boolean"
	default:
		s.kind = "unsupported"
	}
	return s
}
func contributionWireSchema(stage, outcome string) *contributionSchema {
	str := func(values ...string) *contributionSchema { return &contributionSchema{kind: "string", enum: values} }
	fields := map[string]*contributionSchema{"stage": str("plan", "engineering", "legacy", "validate"), "outcome": str()}
	switch stage {
	case "plan":
		fields["outcome"] = str("discussion", "completed")
		if outcome == "discussion" {
			fields["context"] = schemaForContribution(reflect.TypeFor[PlanningDiscussion]())
		}
		if outcome == "completed" {
			fields["plan"] = &contributionSchema{kind: "object", opaque: true}
		}
	case "engineering", "legacy", "validate":
		fields["story"] = str()
		switch stage {
		case "engineering":
			fields["outcome"] = str("engineering_complete", "blocked", "invalidated")
		case "legacy":
			fields["outcome"] = str("verified", "blocked", "invalidated")
		case "validate":
			fields["outcome"] = str("assessed")
		}
		if outcome == "blocked" || outcome == "invalidated" {
			fields["reason"] = str()
		}
		if outcome == "engineering_complete" || outcome == "verified" {
			fields["engineering"] = schemaForContribution(reflect.TypeFor[EngineeringContribution]())
			capture := schemaForContribution(reflect.TypeFor[ContributionCapture]())
			capture.optional = true
			fields["capture"] = capture
		}
		if outcome == "assessed" || outcome == "verified" {
			fields["assessment"] = schemaForContribution(reflect.TypeFor[AssessmentContribution]())
		}
	}
	return &contributionSchema{kind: "object", fields: fields}
}

func (s *contributionSchema) check(n *contributionNode, path string, d *ContributionDiagnostics) {
	if n.kind != s.kind {
		d.add(path, "type", s.kind, n.kind, "Use the exact field type specified by the contribution contract.")
		if s.kind == "object" || s.kind == "array" {
			d.Complete = false
			d.UncheckedPaths = append(d.UncheckedPaths, path)
		}
		return
	}
	if s.opaque {
		checkContributionDuplicates(n, path, d)
		return
	}
	switch s.kind {
	case "object":
		seen := map[string]bool{}
		for _, f := range n.fields {
			p := path + "." + f.name
			if seen[f.name] {
				d.add(p, "duplicate", "one field occurrence", "duplicate key", "Remove the duplicate field.")
			}
			seen[f.name] = true
			expected, ok := s.fields[f.name]
			if !ok {
				code, message := "unknown", "Remove this inactive or unknown field."
				if factoryContributionField(f.name) {
					code = "factory_owned"
					message = "Omit this identity; the factory supplies it from the authorized attempt."
				}
				d.add(p, code, "absent", f.value.kind, message)
				checkContributionDuplicates(f.value, p, d)
			} else {
				expected.check(f.value, p, d)
			}
		}
		for name, field := range s.fields {
			if !seen[name] && !field.optional {
				d.add(path+"."+name, "missing", field.kind, "missing", "Supply the required field, including explicit empty collections where allowed.")
				if field.kind == "array" || field.kind == "object" {
					d.Complete = false
					d.UncheckedPaths = append(d.UncheckedPaths, path+"."+name)
				}
			}
		}
	case "array":
		for i, item := range n.items {
			s.element.check(item, fmt.Sprintf("%s[%d]", path, i), d)
		}
	case "string":
		if len(s.enum) > 0 {
			valid := false
			for _, value := range s.enum {
				valid = valid || n.text == value
			}
			if !valid {
				d.add(path, "enum", strings.Join(s.enum, ", "), "unsupported string", "Choose an outcome supported by the authorized stage.")
			}
		}
	}
}

func checkContributionDuplicates(n *contributionNode, path string, d *ContributionDiagnostics) {
	seen := map[string]bool{}
	for _, f := range n.fields {
		p := path + "." + f.name
		if seen[f.name] {
			d.add(p, "duplicate", "one field occurrence", "duplicate key", "Remove the duplicate field.")
		}
		seen[f.name] = true
		checkContributionDuplicates(f.value, p, d)
	}
	for i, item := range n.items {
		checkContributionDuplicates(item, fmt.Sprintf("%s[%d]", path, i), d)
	}
}
func factoryContributionField(name string) bool {
	switch name {
	case "version", "parent", "parent_id", "run", "run_id", "generation", "issue", "issue_id", "issue_number", "revision", "expected_base", "workflow", "workflow_version", "sequence", "attempt", "submission_id", "receipt_id":
		return true
	}
	return false
}

// diagnosticJSON preserves inspectable siblings while replacing ill-typed leaves
// with typed zero values. It cannot grant validity; check already records the
// original defect and ParseContribution returns no value on any diagnostic.
func (s *contributionSchema) diagnosticJSON(n *contributionNode) json.RawMessage {
	if n == nil || n.kind != s.kind {
		switch s.kind {
		case "object":
			return json.RawMessage(`{}`)
		case "array":
			return json.RawMessage(`[]`)
		case "boolean":
			return json.RawMessage(`false`)
		default:
			return json.RawMessage(`""`)
		}
	}
	switch s.kind {
	case "object":
		var b bytes.Buffer
		b.WriteByte('{')
		first := true
		// Input order, rather than map iteration, also retains stable diagnostics.
		seen := map[string]bool{}
		for _, member := range n.fields {
			field, ok := s.fields[member.name]
			if !ok || seen[member.name] {
				continue
			}
			seen[member.name] = true
			if !first {
				b.WriteByte(',')
			}
			first = false
			// JSON string encoding cannot fail.
			key, err := json.Marshal(member.name)
			if err != nil {
				return json.RawMessage(`{}`)
			}
			b.Write(key)
			b.WriteByte(':')
			b.Write(field.diagnosticJSON(member.value))
		}
		b.WriteByte('}')
		return b.Bytes()
	case "array":
		var b bytes.Buffer
		b.WriteByte('[')
		for i, item := range n.items {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(s.element.diagnosticJSON(item))
		}
		b.WriteByte(']')
		return b.Bytes()
	default:
		return n.raw
	}
}
