package agentresult

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxOperations bounds a single atomic revision.
const MaxOperations = 256

// Target is a typed field selector, never a JSON Pointer. Story selects the
// parent of nested Plan fields; ID selects one item within a collection.
type Target struct {
	Field string `json:"field"`
	Story string `json:"story,omitempty"`
	ID    string `json:"id,omitempty"`
}

// Operation changes authored content only. Value retains omission information.
type Operation struct {
	Op     string          `json:"op"`
	Target Target          `json:"target"`
	Value  json.RawMessage `json:"value,omitempty"`
	Order  []string        `json:"order,omitempty"`
}

// MarshalJSON preserves an explicit empty reorder list while omitting unused order.
func (op Operation) MarshalJSON() ([]byte, error) {
	type wireOperation struct {
		Op     string          `json:"op"`
		Target Target          `json:"target"`
		Value  json.RawMessage `json:"value,omitempty"`
		Order  *[]string       `json:"order,omitempty"`
	}
	var order *[]string
	if op.Order != nil {
		order = &op.Order
	}
	return json.Marshal(wireOperation{Op: op.Op, Target: op.Target, Value: op.Value, Order: order})
}

// UpdateInput carries optimistic concurrency independently of schema version.
type UpdateInput struct {
	ExpectedRevision int64       `json:"expected_revision"`
	Operations       []Operation `json:"operations"`
}

// RetiredIDs is factory-retained, collection-local identity history.
type RetiredIDs map[string][]string

// PreparedUpdate is a private full candidate, not a durable write or decision.
type PreparedUpdate struct {
	Snapshot Snapshot
	Retired  RetiredIDs
}

// UpdateFault attaches safe typed targets to operation diagnostics.
type UpdateFault struct {
	OperationIndex int    `json:"operation_index"`
	Target         Target `json:"target"`
}

// UpdateDiagnostics adds machine-readable concurrency and operation context.
type UpdateDiagnostics struct {
	Diagnostics
	ExpectedRevision int64         `json:"expected_revision"`
	CurrentRevision  int64         `json:"current_revision"`
	Operations       []UpdateFault `json:"operations,omitempty"`
}

// DecodeUpdate rejects unbounded, duplicate, null and unknown input before use.
func DecodeUpdate(data []byte) (UpdateInput, Diagnostics) {
	d := newDiagnostics()
	var input UpdateInput
	if len(data) > MaxBytes || !utf8.Valid(data) {
		add(&d, "$", "limit", "bounded UTF-8 JSON", "invalid input", "Reduce input to the contribution byte ceiling.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
	} else {
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		if err := scan(dec, nil, "$", 0, &d); err != nil {
			add(&d, "$", "json", "bounded valid JSON", "invalid input", "Correct syntax and keep depth at most 32.")
			d.Complete = false
			d.UncheckedPaths = append(d.UncheckedPaths, "$")
		} else if _, err := dec.Token(); err != io.EOF {
			add(&d, "$", "json", "one JSON value", "trailing input", "Remove trailing input.")
		}
		if len(d.Diagnostics) == 0 {
			dec = json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&input); err != nil {
				add(&d, "$", "structure", "typed update fields", "invalid or unknown member", "Use only the documented update, operation and target fields.")
			}
		}
	}
	if len(d.Diagnostics) == 0 && (input.ExpectedRevision < 1 || len(input.Operations) == 0 || len(input.Operations) > MaxOperations) {
		add(&d, "$", "limit", "positive revision and 1–256 operations", "invalid bounds", "Supply a positive revision and bounded nonempty operations.")
	}
	if len(d.Diagnostics) == 0 {
		for i, op := range input.Operations {
			path := fmt.Sprintf("$.operations[%d]", i)
			if err := operationValue(op); err != nil {
				add(&d, path, "operation", "valid operation members", "invalid shape", err.Error())
			}
			known := false
			for _, mode := range []string{"research", "plan", "implement", "validate", "spike"} {
				if allowed(op.Target, EditTargets(mode), mode) {
					known = true
					break
				}
			}
			if !known {
				add(&d, path+".target", "target", "allowlisted typed selector", "invalid target", "Use documented field names and stable IDs, with a parent story for nested Plan fields.")
			}
		}
	}
	finish(&d)
	return input, d
}

// EditTargets enumerates the maximum editable fields. A caller must explicitly
// authorize selections; nil permissions grant no access. Proof is references only.
func EditTargets(mode string) []Target {
	fields := []string{"outcome", "comment", "reason", "recommendation", "questions", "artifacts"}
	switch mode {
	case "research":
		fields = append(fields, "payload.research.request", "payload.research.findings", "payload.research.decisions", "payload.research.assumptions")
	case "plan":
		fields = append(fields, "payload.plan.summary", "payload.plan.context", "payload.plan.stories", "payload.plan.stories.title", "payload.plan.stories.intent", "payload.plan.stories.user_story", "payload.plan.stories.acceptance_criteria", "payload.plan.stories.tasks", "payload.plan.stories.architecture", "payload.plan.stories.dependencies")
	case "implement":
		fields = append(fields, "payload.implement.stories", "payload.implement.aggregate_checks")
	case "validate":
		fields = append(fields, "payload.validate.stories")
	case "spike":
		fields = append(fields, "payload.spike.request", "payload.spike.work_done", "payload.spike.findings", "payload.spike.decisions", "payload.spike.not_done")
	default:
		return nil
	}
	out := make([]Target, len(fields))
	for i, f := range fields {
		out[i] = Target{Field: f}
	}
	return out
}
func allowed(t Target, grants []Target, mode string) bool {
	if !slices.ContainsFunc(EditTargets(mode), func(g Target) bool { return g.Field == t.Field }) || (t.Story != "" && !ValidID(t.Story)) || (t.ID != "" && !ValidID(t.ID)) {
		return false
	}
	nested := strings.HasPrefix(t.Field, "payload.plan.stories.")
	if nested != (t.Story != "") {
		return false
	}
	return slices.ContainsFunc(grants, func(g Target) bool {
		return g.Field == t.Field && (g.Story == "" || g.Story == t.Story) && (g.ID == "" || g.ID == t.ID)
	})
}
func itemKey(v reflect.Value) string {
	if v.Kind() != reflect.Struct {
		return ""
	}
	for _, n := range []string{"ID", "Key", "Story"} {
		f := v.FieldByName(n)
		if f.IsValid() && f.Kind() == reflect.String {
			return f.String()
		}
	}
	return ""
}
func field(v reflect.Value, name string) (reflect.Value, bool) {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if strings.Split(f.Tag.Get("json"), ",")[0] == name {
			return v.Field(i), strings.Contains(f.Tag.Get("json"), "omitempty")
		}
	}
	return reflect.Value{}, false
}
func locate(r *Result, t Target) (reflect.Value, bool) {
	v := reflect.ValueOf(r).Elem()
	optional := false
	for _, part := range strings.Split(t.Field, ".") {
		v, optional = field(v, part)
		if !v.IsValid() {
			return v, false
		}
		if part == "stories" && t.Story != "" {
			found := false
			for i := 0; i < v.Len(); i++ {
				if itemKey(v.Index(i)) == t.Story {
					v = v.Index(i)
					found = true
					break
				}
			}
			if !found {
				return reflect.Value{}, false
			}
		}
	}
	return v, optional
}
func selected(v reflect.Value, id string) int {
	for i := 0; i < v.Len(); i++ {
		if itemKey(v.Index(i)) == id {
			return i
		}
	}
	return -1
}

// ApplyUpdate applies sequential structural edits privately, then validates the
// entire final result. Any rejection returns no candidate or retirement changes.
func ApplyUpdate(s Snapshot, retired RetiredIDs, input UpdateInput, ctx Context, authorized []Target) (PreparedUpdate, UpdateDiagnostics) {
	d := UpdateDiagnostics{Diagnostics: newDiagnostics(), ExpectedRevision: input.ExpectedRevision, CurrentRevision: s.Meta.ResultRevision}
	fail := func(code, msg string) (PreparedUpdate, UpdateDiagnostics) {
		add(&d.Diagnostics, "$", code, "authorized valid update", "rejected", msg)
		finish(&d.Diagnostics)
		return PreparedUpdate{}, d
	}
	// Re-encode typed entrypoints so callers cannot evade input limits or null checks.
	wire, err := json.Marshal(input)
	if err != nil {
		return fail("encoding", "Supply valid JSON operation values.")
	}
	_, checked := DecodeUpdate(wire)
	if !checked.Valid {
		d.Diagnostics = checked
		return PreparedUpdate{}, d
	}
	if s.Meta.Mode != ctx.Mode || s.Meta.SchemaVersion != SchemaVersion || s.Meta.ResultRevision < 1 {
		return fail("scope", "Use a snapshot bound to the trusted mode and schema.")
	}
	if input.ExpectedRevision != s.Meta.ResultRevision {
		for i, op := range input.Operations {
			if allowed(op.Target, authorized, ctx.Mode) {
				d.Operations = append(d.Operations, UpdateFault{OperationIndex: i, Target: op.Target})
			}
		}
		return fail("stale_revision", "Read authorized targets at the current revision and retry with fresh correlation.")
	}
	if s.Meta.ResultRevision == math.MaxInt64 {
		return fail("limit", "Result revision cannot be incremented.")
	}
	r, err := Clone(s.Result)
	if err != nil {
		return fail("encoding", "Supply a serializable baseline.")
	}
	history := RetiredIDs{}
	for k, v := range retired {
		history[k] = slices.Clone(v)
	}
	for i, op := range input.Operations {
		if !allowed(op.Target, authorized, ctx.Mode) {
			d.Operations = append(d.Operations, UpdateFault{OperationIndex: i})
			return fail("authorization", "Use only factory-authorized editable selectors.")
		}
		if err := applyOperation(&r, history, op); err != nil {
			d.Operations = append(d.Operations, UpdateFault{OperationIndex: i, Target: op.Target})
			add(&d.Diagnostics, fmt.Sprintf("$.operations[%d]", i), "operation", "valid typed operation", "invalid target or value", err.Error())
			finish(&d.Diagnostics)
			return PreparedUpdate{}, d
		}
	}
	finalBytes, err := json.Marshal(CreateInput{Result: r})
	if err != nil {
		return fail("encoding", "Supply serializable final content.")
	}
	_, d.Diagnostics = Create(finalBytes, ctx)
	if !d.Valid {
		return PreparedUpdate{}, d
	}
	s.Result = r
	s.Meta.ResultRevision++
	return PreparedUpdate{Snapshot: s, Retired: history}, d
}

func mergeJSON(before, change json.RawMessage) (json.RawMessage, error) {
	if len(change) == 0 {
		return nil, fmt.Errorf("supply a value")
	}
	if change[0] != '{' {
		return slices.Clone(change), nil
	}
	var old, patch map[string]json.RawMessage
	if len(before) > 0 && before[0] == '{' {
		if err := json.Unmarshal(before, &old); err != nil {
			return nil, fmt.Errorf("decode baseline object: %w", err)
		}
	}
	if err := json.Unmarshal(change, &patch); err != nil {
		return nil, fmt.Errorf("decode supplied object: %w", err)
	}
	if old == nil {
		old = map[string]json.RawMessage{}
	}
	for k, v := range patch {
		if (k == "reason" || k == "action") && bytes.Equal(bytes.TrimSpace(v), []byte(`""`)) {
			return nil, fmt.Errorf("omit unused reason or action with remove instead of an empty value")
		}
		merged, err := mergeJSON(old[k], bytes.TrimSpace(v))
		if err != nil {
			return nil, err
		}
		old[k] = merged
	}
	return json.Marshal(old)
}
func assign(v reflect.Value, raw json.RawMessage, merge bool) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return fmt.Errorf("supply a value")
	}
	if merge {
		before, err := json.Marshal(v.Interface())
		if err != nil {
			return fmt.Errorf("encode baseline: %w", err)
		}
		raw, err = mergeJSON(before, raw)
		if err != nil {
			return err
		}
	}
	next := reflect.New(v.Type())
	d := Decode(raw, next.Interface())
	if !d.Valid {
		return fmt.Errorf("use documented non-null members and types, preserving required fields")
	}
	v.Set(next.Elem())
	return nil
}
func operationValue(op Operation) error {
	hasValue := len(op.Value) > 0
	switch op.Op {
	case "set", "add_item", "update_item":
		if !hasValue || op.Order != nil {
			return fmt.Errorf("supply value only for this operation")
		}
	case "remove", "remove_item":
		if hasValue || op.Order != nil {
			return fmt.Errorf("omit value and order for removal")
		}
	case "reorder":
		if hasValue || op.Order == nil {
			return fmt.Errorf("supply order only for reorder")
		}
	default:
		return fmt.Errorf("use set, remove, add_item, update_item, remove_item or reorder")
	}
	return nil
}
func applyOperation(r *Result, history RetiredIDs, op Operation) error {
	if err := operationValue(op); err != nil {
		return err
	}
	v, optional := locate(r, op.Target)
	if !v.IsValid() {
		return fmt.Errorf("select an existing field or parent story")
	}
	before := identityInventory(*r)
	switch op.Op {
	case "set":
		if op.Target.Field == "reason" && bytes.Equal(bytes.TrimSpace(op.Value), []byte(`""`)) {
			return fmt.Errorf("remove unused reason instead of setting an empty value")
		}
		if op.Target.ID != "" {
			return fmt.Errorf("use update_item for an item selector")
		}
		if err := assign(v, op.Value, true); err != nil {
			return err
		}
	case "remove":
		if op.Target.ID != "" || !optional || v.IsZero() || (v.Kind() == reflect.Slice && v.Len() == 0) {
			return fmt.Errorf("remove requires an existing optional field without an item selector")
		}
		v.SetZero()
	default:
		if v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Struct {
			return fmt.Errorf("item operations require an identified collection")
		}
		index := -1
		if op.Target.ID != "" {
			index = selected(v, op.Target.ID)
		}
		switch op.Op {
		case "add_item":
			if op.Target.ID == "" || index >= 0 {
				return fmt.Errorf("add_item requires an unused stable item ID")
			}
			item := reflect.New(v.Type().Elem()).Elem()
			if err := assign(item, op.Value, false); err != nil {
				return err
			}
			if itemKey(item) != op.Target.ID {
				return fmt.Errorf("selector and value identities must agree")
			}
			v.Set(reflect.Append(v, item))
		case "update_item":
			if index < 0 {
				return fmt.Errorf("update_item requires an existing stable item ID")
			}
			if err := assign(v.Index(index), op.Value, true); err != nil {
				return err
			}
			if itemKey(v.Index(index)) != op.Target.ID {
				return fmt.Errorf("item identity cannot change")
			}
		case "remove_item":
			if index < 0 {
				return fmt.Errorf("remove_item requires an existing stable item ID")
			}
			next := reflect.MakeSlice(v.Type(), 0, v.Len()-1)
			next = reflect.AppendSlice(next, v.Slice(0, index))
			next = reflect.AppendSlice(next, v.Slice(index+1, v.Len()))
			v.Set(next)
		case "reorder":
			if op.Target.ID != "" || len(op.Order) != v.Len() {
				return fmt.Errorf("reorder requires every current key exactly once")
			}
			next := reflect.MakeSlice(v.Type(), 0, v.Len())
			seen := map[string]bool{}
			for _, id := range op.Order {
				at := selected(v, id)
				if at < 0 || seen[id] {
					return fmt.Errorf("reorder requires every current key exactly once")
				}
				seen[id] = true
				next = reflect.Append(next, v.Index(at))
			}
			v.Set(next)
		}
	}
	after := identityInventory(*r)
	for path, ids := range after {
		seen := map[string]bool{}
		for _, id := range ids {
			if !ValidID(id) || seen[id] {
				return fmt.Errorf("collection identities must be unique stable IDs")
			}
			seen[id] = true
			if slices.Contains(history[path], id) && !slices.Contains(before[path], id) {
				return fmt.Errorf("retired collection identities cannot be reused")
			}
		}
	}
	for path, ids := range before {
		for _, id := range ids {
			if !slices.Contains(after[path], id) && !slices.Contains(history[path], id) {
				history[path] = append(history[path], id)
			}
		}
	}
	return nil
}

// Inventory includes nested identified collections so replacing a parent object
// or array cannot bypass retirement of its children.
func identityInventory(r Result) map[string][]string {
	out := map[string][]string{}
	var walk func(reflect.Value, string)
	walk = func(v reflect.Value, path string) {
		for v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return
			}
			v = v.Elem()
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				name := strings.Split(v.Type().Field(i).Tag.Get("json"), ",")[0]
				walk(v.Field(i), path+"."+name)
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				item := v.Index(i)
				id := itemKey(item)
				if id != "" {
					out[path] = append(out[path], id)
					walk(item, path+"["+id+"]")
				}
			}
		}
	}
	walk(reflect.ValueOf(r), "result")
	return out
}
