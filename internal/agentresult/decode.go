package agentresult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

func newDiagnostics() Diagnostics {
	return Diagnostics{Complete: true, Diagnostics: []Diagnostic{}, UncheckedPaths: []string{}}
}
func add(d *Diagnostics, path, code, expected, actual, message string) {
	d.Diagnostics = append(d.Diagnostics, Diagnostic{Path: path, Code: code, Expected: expected, Actual: actual, Message: message})
}
func finish(d *Diagnostics) {
	slices.SortFunc(d.Diagnostics, func(a, b Diagnostic) int {
		if c := strings.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		return strings.Compare(a.Code, b.Code)
	})
	slices.Sort(d.UncheckedPaths)
	d.UncheckedPaths = slices.Compact(d.UncheckedPaths)
	d.Valid = d.Complete && len(d.Diagnostics) == 0
}

// Decode strictly inspects a bounded authored value against a concrete wire type.
// Unknown member names are not echoed: a field name may itself contain a secret.
func Decode(data []byte, dst interface{}) Diagnostics {
	d := newDiagnostics()
	if len(data) > MaxBytes || !utf8.Valid(data) {
		add(&d, "$", "limit", "bounded UTF-8 JSON", "invalid input", "Use valid UTF-8 within the contribution byte ceiling.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
		finish(&d)
		return d
	}
	t := reflect.TypeOf(dst)
	if t == nil || t.Kind() != reflect.Pointer {
		add(&d, "$", "destination", "concrete pointer", "invalid destination", "Supply a typed destination.")
		finish(&d)
		return d
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := scan(dec, t.Elem(), "$", 0, &d); err != nil {
		add(&d, "$", "json", "one valid JSON value", "malformed or too deep", "Correct JSON syntax and keep container depth at most 32.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
	} else if _, err := dec.Token(); err != io.EOF {
		add(&d, "$", "json", "one JSON value", "trailing input", "Remove trailing JSON or text.")
	}
	if len(d.Diagnostics) == 0 {
		if err := json.Unmarshal(data, dst); err != nil {
			add(&d, "$", "type", "representable typed value", "out of range", "Use values within the destination type range.")
		}
	}
	finish(&d)
	return d
}
func scan(dec *json.Decoder, t reflect.Type, path string, depth int, d *Diagnostics) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if tok == nil {
		add(d, path, "null", "non-null value", "null", "Omit optional fields instead of using null.")
		return nil
	}
	delim, container := tok.(json.Delim)
	actual := "string"
	switch tok.(type) {
	case bool:
		actual = "boolean"
	case json.Number:
		actual = "number"
	}
	if container {
		if depth >= MaxDepth {
			return fmt.Errorf("depth limit")
		}
		if delim == '{' {
			actual = "object"
		} else {
			actual = "array"
		}
	}
	expected := "known field"
	if t != nil {
		switch t.Kind() {
		case reflect.Struct:
			expected = "object"
		case reflect.Slice:
			expected = "array"
		case reflect.String:
			expected = "string"
		case reflect.Int, reflect.Int64:
			expected = "integer"
		case reflect.Bool:
			expected = "boolean"
		}
	}
	match := expected == actual || expected == "integer" && actual == "number"
	if !match && t != nil {
		add(d, path, "type", expected, actual, "Supply the documented field type.")
	}
	if !container {
		return nil
	}
	switch delim {
	case '{':
		fields := map[string]reflect.StructField{}
		if t != nil && t.Kind() == reflect.Struct {
			for i := 0; i < t.NumField(); i++ {
				f := t.Field(i)
				if f.PkgPath != "" || f.Tag.Get("json") == "-" {
					continue
				}
				fields[strings.Split(f.Tag.Get("json"), ",")[0]] = f
			}
		}
		seen := map[string]bool{}
		for dec.More() {
			keyTok, e := dec.Token()
			if e != nil {
				return e
			}
			key, ok := keyTok.(string)
			if !ok {
				return fmt.Errorf("object key")
			}
			f, known := fields[key]
			p := path + ".<unknown>"
			var child reflect.Type
			if known {
				p = path + "." + key
				child = f.Type
			} else if t != nil {
				code := "unknown"
				if key == "meta" {
					code = "authorization"
				}
				add(d, p, code, "allowlisted authored member", "unknown member", "Remove fields outside the authored contract.")
			}
			if seen[key] {
				add(d, p, "duplicate", "unique member", "duplicate member", "Supply each object member once.")
			}
			seen[key] = true
			if e = scan(dec, child, p, depth+1, d); e != nil {
				return e
			}
		}
		for name, f := range fields {
			if !seen[name] && !strings.Contains(f.Tag.Get("json"), "omitempty") && !strings.HasSuffix(path, ".contribution.draft") {
				add(d, path+"."+name, "required", "present field", "absent", "Supply the required field.")
			}
		}
	case '[':
		var child reflect.Type
		if t != nil && t.Kind() == reflect.Slice {
			child = t.Elem()
		}
		for i := 0; dec.More(); i++ {
			if e := scan(dec, child, fmt.Sprintf("%s[%d]", path, i), depth+1, d); e != nil {
				return e
			}
		}
	default:
		return fmt.Errorf("unexpected delimiter")
	}
	_, err = dec.Token()
	return err
}

// Create returns no candidate unless both structural and semantic validation finish.
func Create(data []byte, ctx Context) (Result, Diagnostics) {
	var input CreateInput
	d := Decode(data, &input)
	if !d.Valid {
		return Result{}, d
	}
	d = ValidateResult(input.Result, ctx)
	var presence struct {
		Result struct {
			Reason         *string `json:"reason"`
			Recommendation *struct {
				Action *string `json:"action"`
			} `json:"recommendation"`
			Payload struct {
				Plan *struct {
					StoryRequest map[string]json.RawMessage `json:"storyrequest"`
				} `json:"plan"`
			} `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &presence); err != nil {
		add(&d, "$", "encoding", "typed authored input", "invalid", "Correct the input.")
	}
	if input.Result.Outcome == "completed" && presence.Result.Reason != nil && *presence.Result.Reason == "" {
		add(&d, "$.result.reason", "forbidden", "absent reason", "present reason", "Omit reason for completed outcomes.")
	}
	if presence.Result.Recommendation != nil && presence.Result.Recommendation.Action != nil && *presence.Result.Recommendation.Action == "" {
		add(&d, "$.result.recommendation.action", "enum", "action.moveto.planmode or omitted", "empty action", "Omit unused optional actions.")
	}
	// Presence, not value: an empty or repeated factory-owned member is still one
	// the agent tried to supply. Only graph Plan has them; legacy Plan refuses the
	// whole branch elsewhere.
	if ctx.PlanGraph != nil && presence.Result.Payload.Plan != nil {
		for _, name := range planFactoryFields {
			if _, supplied := presence.Result.Payload.Plan.StoryRequest[name]; supplied {
				add(&d, "$.result.payload.plan.storyrequest."+name, "factory_owned", "member absent: the factory binds it", "supplied", "Remove "+name+": the factory fills stage, candidate, request identity, upstream references and shared-decision revision, and an agent never supplies them.")
			}
		}
	}
	finish(&d)
	if !d.Valid {
		return Result{}, d
	}
	return input.Result, d
}

// Clone copies all nested boundary values without sharing slices or pointers.
func Clone(r Result) (Result, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return Result{}, err
	}
	var cloned Result
	err = json.Unmarshal(b, &cloned)
	return cloned, err
}

// Assemble validates factory metadata and copies the validated result. New results
// start at revision one; history callers explicitly supply subsequent revisions.
func Assemble(r Result, meta Metadata, ctx Context) (Snapshot, Diagnostics) {
	d := ValidateResult(r, ctx)
	if meta.SchemaVersion != SchemaVersion || meta.Mode != ctx.Mode || !resultWorkflowMatches(meta, ctx) || meta.ResultRevision < 1 || meta.RunID < 1 || strings.TrimSpace(meta.Task) == "" || strings.TrimSpace(meta.AttemptID) == "" || strings.TrimSpace(meta.ResultID) == "" {
		add(&d, "$.meta", "scope", "complete supported factory metadata", "invalid binding", "Bind the result to trusted identity, mode and positive revision.")
	}
	if (meta.WorkflowVersion != 0 && meta.WorkflowVersion != ctx.WorkflowVersion) || (meta.SubmissionContractVersion != 0 && meta.SubmissionContractVersion != 2) {
		add(&d, "$.meta", "version", "independent supported bound versions", "incompatible metadata", "Preserve the persisted workflow and submission contract versions.")
	}
	finish(&d)
	if !d.Valid {
		return Snapshot{}, d
	}
	cloned, err := Clone(r)
	if err != nil {
		add(&d, "$.result", "encoding", "serializable result", "invalid value", "Correct the result.")
		finish(&d)
		return Snapshot{}, d
	}
	bindPlanGraph(&cloned, ctx)
	return Snapshot{Meta: meta, Result: cloned}, d
}
