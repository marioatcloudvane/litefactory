package agentresult

import (
	"encoding/json"
	"reflect"
	"strings"
)

// JSONSchema returns the structural schema generated from the same concrete wire
// types used by Decode. Semantic scope, evidence, UTF-8 byte and graph constraints
// additionally require Create; schema validation alone never admits a result.
func JSONSchema() ([]byte, error) {
	schema := wireSchema(reflect.TypeFor[CreateInput]())
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "urn:litefactory:agent-result:create:1"
	schema["title"] = "Agent-authored work result, application schema 1"
	return json.MarshalIndent(schema, "", "  ")
}
func wireSchema(t reflect.Type) map[string]interface{} {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	s := map[string]interface{}{}
	switch t.Kind() {
	case reflect.Struct:
		s["type"] = "object"
		s["additionalProperties"] = false
		props := map[string]interface{}{}
		requiredFields := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			props[name] = wireSchema(f.Type)
			if !strings.Contains(tag, "omitempty") {
				requiredFields = append(requiredFields, name)
			}
		}
		s["properties"] = props
		if len(requiredFields) > 0 {
			s["required"] = requiredFields
		}
		if t == reflect.TypeFor[Metadata]() {
			props["schema_version"] = map[string]interface{}{"type": "integer", "const": SchemaVersion}
			props["result_revision"] = map[string]interface{}{"type": "integer", "minimum": 1}
			props["mode"] = map[string]interface{}{"type": "string", "enum": []string{"research", "plan", "implement", "validate", "spike"}}
		}
		if t == reflect.TypeFor[Payload]() {
			s["minProperties"] = 1
			s["maxProperties"] = 1
		}
		if t == reflect.TypeFor[Result]() {
			props["outcome"] = map[string]interface{}{"type": "string", "enum": []string{"completed", "incomplete", "blocked"}}
			s["allOf"] = []interface{}{map[string]interface{}{"if": map[string]interface{}{"properties": map[string]interface{}{"outcome": map[string]interface{}{"const": "completed"}}}, "then": map[string]interface{}{"not": map[string]interface{}{"required": []string{"reason"}}}, "else": map[string]interface{}{"required": []string{"reason"}}}}
		}
		if t == reflect.TypeFor[Recommendation]() {
			props["action"] = map[string]interface{}{"type": "string", "const": "action.moveto.planmode"}
		}
	case reflect.Slice:
		s["type"] = "array"
		s["items"] = wireSchema(t.Elem())
	case reflect.String:
		s["type"] = "string"
	case reflect.Int, reflect.Int64:
		s["type"] = "integer"
	case reflect.Bool:
		s["type"] = "boolean"
	}
	return s
}

// SnapshotJSONSchema describes factory-assembled snapshots; it is never an
// authored-input schema and its metadata cannot grant authority.
func SnapshotJSONSchema() ([]byte, error) {
	schema := wireSchema(reflect.TypeFor[Snapshot]())
	schema["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	schema["$id"] = "urn:litefactory:agent-result:snapshot:1"
	schema["title"] = "Factory-assembled work result, application schema 1"
	return json.MarshalIndent(schema, "", "  ")
}
