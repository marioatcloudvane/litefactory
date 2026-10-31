// Package durableworkflow coordinates installed workflows and their durable jobs.
package durableworkflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ProjectMode determines how a caller selects the execution target.
type ProjectMode string

// Project modes define the permitted target selection.
const (
	ProjectFixed    ProjectMode = "fixed"
	ProjectRequired ProjectMode = "required"
	ProjectOptional ProjectMode = "optional"
	ProjectNone     ProjectMode = "none"
)

// Field is a bounded, typed input contract. Unknown fields are always rejected.
type Field struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// Schema describes an adapter's object inputs or results.
type Schema struct {
	Type                 string           `json:"type"`
	Properties           map[string]Field `json:"properties"`
	Required             []string         `json:"required,omitempty"`
	AdditionalProperties bool             `json:"additionalProperties"`
}

// Definition is copied into proposals so later installation changes cannot alter them.
type Definition struct {
	Ref                  string           `json:"workflow_ref"`
	Version              string           `json:"version"`
	Digest               string           `json:"definition_digest"`
	Adapter              string           `json:"adapter"`
	Name                 string           `json:"name"`
	Purpose              string           `json:"purpose"`
	BindingProject       string           `json:"binding_project,omitempty"`
	ProjectMode          ProjectMode      `json:"project_mode"`
	FixedProject         string           `json:"fixed_project,omitempty"`
	Inputs               Schema           `json:"inputs"`
	Continuation         Schema           `json:"continuation"`
	Result               Schema           `json:"result"`
	Examples             []map[string]any `json:"examples"`
	ContinuationExamples []map[string]any `json:"continuation_examples,omitempty"`
	ResultExamples       []map[string]any `json:"result_examples"`
	RequiredPermissions  []string         `json:"required_permissions"`
	DeadlineSeconds      int              `json:"deadline_seconds"`
	Impact               string           `json:"impact"`
}

// Registry holds immutable, explicitly installed definitions.
type Registry struct{ entries map[string]Definition }

func cloneDefinition(d Definition) Definition {
	b, _ := json.Marshal(d)
	var out Definition
	_ = json.Unmarshal(b, &out)
	return out
}
func definitionKey(ref, version string) string { return ref + "\x00" + version }

// NewRegistry validates and freezes installed version contracts.
func NewRegistry(definitions []Definition) (*Registry, error) {
	r := &Registry{entries: map[string]Definition{}}
	for _, d := range definitions {
		if len(d.Ref) > 128 || len(d.Version) > 64 || len(d.Name) > 128 || len(d.Purpose) > 2048 || len(d.BindingProject) > 1024 || len(d.FixedProject) > 1024 || len(d.RequiredPermissions) > 16 {
			return nil, errors.New("workflow metadata exceeds discovery limits")
		}
		if strings.TrimSpace(d.Ref) == "" || strings.TrimSpace(d.Version) == "" || d.Adapter == "" || d.DeadlineSeconds <= 0 || d.DeadlineSeconds > 86400 || len(d.RequiredPermissions) == 0 {
			return nil, errors.New("workflow requires identity, adapter, permissions and bounded deadline")
		}
		switch d.ProjectMode {
		case ProjectFixed:
			if d.FixedProject == "" {
				return nil, errors.New("fixed workflow requires project")
			}
		case ProjectRequired, ProjectOptional, ProjectNone:
			if d.FixedProject != "" {
				return nil, errors.New("only fixed workflows may bind an execution project")
			}
		default:
			return nil, errors.New("invalid workflow project mode")
		}
		if err := validateSchema(d.Inputs); err != nil {
			return nil, err
		}
		if err := validateSchema(d.Continuation); err != nil {
			return nil, err
		}
		if d.Inputs.AdditionalProperties || d.Continuation.AdditionalProperties {
			return nil, errors.New("workflow inputs must reject unknown fields")
		}
		if d.ProjectMode == ProjectFixed || d.ProjectMode == ProjectNone {
			if _, ok := d.Inputs.Properties["project"]; ok {
				return nil, errors.New("workflow cannot expose selectable project input")
			}
		}
		key := definitionKey(d.Ref, d.Version)
		if _, ok := r.entries[key]; ok {
			return nil, errors.New("duplicate workflow version")
		}
		d.Digest = ""
		b, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		if len(b) > 32768 {
			return nil, errors.New("workflow definition exceeds contract limit")
		}
		sum := sha256.Sum256(b)
		d.Digest = hex.EncodeToString(sum[:])
		r.entries[key] = cloneDefinition(d)
	}
	return r, nil
}
func validateSchema(schema Schema) error {
	if schema.Type != "" && schema.Type != "object" {
		return errors.New("workflow schema must describe an object")
	}
	for _, name := range schema.Required {
		if _, ok := schema.Properties[name]; !ok {
			return errors.New("required workflow field is not declared")
		}
	}
	for name, field := range schema.Properties {
		if strings.TrimSpace(name) == "" {
			return errors.New("workflow field requires a name")
		}
		switch field.Type {
		case "string", "boolean", "integer", "number", "object", "array":
		default:
			return errors.New("unsupported workflow field type")
		}
		if len(field.Enum) > 0 && field.Type != "string" {
			return errors.New("workflow enums require string fields")
		}
	}
	return nil
}

// Get returns a copy of one exact installed version.
func (r *Registry) Get(ref, version string) (Definition, error) {
	if r == nil {
		return Definition{}, errors.New("workflow registry unavailable")
	}
	d, ok := r.entries[definitionKey(ref, version)]
	if !ok {
		return Definition{}, errors.New("unknown workflow version")
	}
	return cloneDefinition(d), nil
}

// Definitions returns copies in stable reference and version order.
func (r *Registry) Definitions() []Definition {
	var out []Definition
	for _, d := range r.entries {
		out = append(out, cloneDefinition(d))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref == out[j].Ref {
			return out[i].Version < out[j].Version
		}
		return out[i].Ref < out[j].Ref
	})
	return out
}

// Resolved contains the explicit target and canonical typed inputs.
type Resolved struct {
	Project string         `json:"project,omitempty"`
	Inputs  map[string]any `json:"inputs"`
}

func projectOverride(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			switch strings.ToLower(k) {
			case "project", "project_id", "project_ref", "repository", "repository_id", "repository_ref":
				return true
			}
			if projectOverride(v) {
				return true
			}
		}
	case []any:
		for _, v := range x {
			if projectOverride(v) {
				return true
			}
		}
	}
	return false
}

// ValidateInputs rejects unknown fields and returns a bounded canonical copy.
func ValidateInputs(schema Schema, inputs map[string]any) (map[string]any, error) {
	b, err := json.Marshal(inputs)
	if err != nil {
		return nil, err
	}
	if len(b) > 65536 {
		return nil, errors.New("workflow inputs exceed limit")
	}
	var out map[string]any
	if err = json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	for _, k := range schema.Required {
		if v, ok := out[k]; !ok || v == nil {
			return nil, fmt.Errorf("missing workflow input %s", k)
		}
	}
	for k, v := range out {
		f, ok := schema.Properties[k]
		if !ok {
			return nil, fmt.Errorf("unknown workflow input %s", k)
		}
		valid := false
		switch f.Type {
		case "string":
			s, ok := v.(string)
			valid = ok && strings.TrimSpace(s) != ""
		case "boolean":
			_, valid = v.(bool)
		case "integer":
			n, ok := v.(float64)
			valid = ok && n == float64(int64(n))
		case "number":
			_, valid = v.(float64)
		case "object":
			_, valid = v.(map[string]any)
		case "array":
			_, valid = v.([]any)
		}
		if !valid {
			return nil, fmt.Errorf("invalid workflow input %s", k)
		}
		if len(f.Enum) > 0 {
			s, _ := v.(string)
			valid = false
			for _, allowed := range f.Enum {
				if s == allowed {
					valid = true
				}
			}
			if !valid {
				return nil, fmt.Errorf("invalid workflow input %s", k)
			}
		}
	}
	return out, nil
}

// Resolve applies explicit target rules without consulting ambient defaults.
func Resolve(d Definition, project string, inputs map[string]any) (Resolved, error) {
	if projectOverride(inputs) {
		return Resolved{}, errors.New("project must be selected only through the declared target")
	}
	switch d.ProjectMode {
	case ProjectFixed:
		if project != "" {
			return Resolved{}, errors.New("fixed workflow rejects project overrides")
		}
		project = d.FixedProject
	case ProjectRequired:
		if project == "" {
			return Resolved{}, errors.New("workflow requires explicit project")
		}
	case ProjectNone:
		if project != "" {
			return Resolved{}, errors.New("workflow does not accept project")
		}
	case ProjectOptional:
	default:
		return Resolved{}, errors.New("invalid workflow project mode")
	}
	normalized, err := ValidateInputs(d.Inputs, inputs)
	if err != nil {
		return Resolved{}, err
	}
	if d.Adapter == "log-analysis" {
		after, err := time.Parse(time.RFC3339, fmt.Sprint(normalized["after"]))
		if err != nil {
			return Resolved{}, errors.New("after requires absolute RFC3339 time")
		}
		before, err := time.Parse(time.RFC3339, fmt.Sprint(normalized["before"]))
		if err != nil || !before.After(after) {
			return Resolved{}, errors.New("before must follow after")
		}
		normalized["after"] = after.UTC().Format(time.RFC3339Nano)
		normalized["before"] = before.UTC().Format(time.RFC3339Nano)
	}
	return Resolved{Project: project, Inputs: normalized}, nil
}
func object(properties map[string]Field, required ...string) Schema {
	return Schema{Type: "object", Properties: properties, Required: required}
}

// BuiltinDefinitions declares the classified built-in workflow contracts.
func BuiltinDefinitions() []Definition {
	return []Definition{
		{Ref: "log-analysis", Version: "1", Adapter: "log-analysis", Name: "Analyze logs", Purpose: "Analyze repository logs and optionally file confirmed findings", ProjectMode: ProjectRequired, RequiredPermissions: []string{"repository:actions"}, DeadlineSeconds: 1980, Impact: "external", Inputs: object(map[string]Field{"after": {Type: "string"}, "before": {Type: "string"}, "dry_run": {Type: "boolean"}}, "after", "before", "dry_run"), Continuation: object(map[string]Field{}), Result: jobResultSchema(), ResultExamples: []map[string]any{{"job": "job-example", "state": "succeeded", "domain_ref": "analysis-example", "items": []any{map[string]any{"finding_key": "finding-1", "state": "filed", "issue_url": "https://github.com/example/project/issues/1"}}, "delivery": []any{map[string]any{"event": "terminal", "state": "unavailable"}}}}, Examples: []map[string]any{{"project": "example/project", "inputs": map[string]any{"after": "2026-10-30T00:00:00Z", "before": "2026-10-31T00:00:00Z", "dry_run": true}}}},
		{Ref: "standards-authoring", Version: "1", Adapter: "standards-authoring", Name: "Draft standards", Purpose: "Draft proposed standards for a book with optional project context", ProjectMode: ProjectOptional, RequiredPermissions: []string{"standards:management"}, DeadlineSeconds: 960, Impact: "local", Inputs: object(map[string]Field{"intent": {Type: "string"}, "category": {Type: "string"}, "target": {Type: "string"}, "mode": {Type: "string", Enum: []string{"create", "edit"}}}, "intent"), Continuation: object(map[string]Field{"job": {Type: "string"}, "session_revision": {Type: "integer"}, "answers": {Type: "object"}}, "job", "session_revision", "answers"), Result: jobResultSchema(), ContinuationExamples: []map[string]any{{"job": "job-example", "session_revision": 1, "answers": map[string]string{"question-1": "Use focused tests"}}}, ResultExamples: []map[string]any{{"job": "job-example", "state": "waiting_for_input", "domain_ref": "author-example", "session_revision": 1, "questions": []any{map[string]any{"id": "question-1", "text": "Which tests?"}}}}, Examples: []map[string]any{{"inputs": map[string]any{"intent": "Draft testing standards", "category": "testing"}}}},
	}
}

func jobResultSchema() Schema {
	return object(map[string]Field{
		"job": {Type: "string"}, "definition": {Type: "object"}, "project": {Type: "string"}, "state": {Type: "string", Enum: []string{"queued", "running", "waiting_for_input", "succeeded", "failed", "uncertain"}}, "revision": {Type: "integer"}, "created_at": {Type: "string"}, "updated_at": {Type: "string"}, "summary": {Type: "string"}, "domain_ref": {Type: "string"}, "session_revision": {Type: "integer"}, "questions": {Type: "array", Description: "Exact question IDs and text for the saved session revision"}, "items": {Type: "array", Description: "Paged retained findings, confirmed issue links or proposed standards; no raw logs or captured prompts"}, "attempts": {Type: "array"}, "delivery": {Type: "array", Description: "Independent pending, delivered, failed or unavailable notices"}, "next_action": {Type: "string"}, "next_cursor": {Type: "string"}}, "job", "state", "summary", "items", "attempts", "delivery", "next_action")
}
