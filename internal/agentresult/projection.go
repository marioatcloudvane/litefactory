package agentresult

import (
	"encoding/json"
	"reflect"
)

// Projection contains only authorized content and a full-result revision. It is
// deliberately not a Snapshot and cannot be submitted as complete result data.
type Projection struct {
	ResultID         string           `json:"result_id"`
	Revision         int64            `json:"revision"`
	CompleteSnapshot bool             `json:"complete_snapshot"`
	Selections       []ProjectedValue `json:"selections"`
}

// ProjectedValue carries its exact selector with copied baseline bytes. Identity
// and evidence references inside that selection are retained without expansion.
type ProjectedValue struct {
	Target Target          `json:"target"`
	Value  json.RawMessage `json:"value"`
}

// Project reads within explicit factory grants. A selector never broadens a
// narrower item grant into its siblings or a complete document.
func Project(s Snapshot, selections, authorized []Target, ctx Context) (Projection, Diagnostics) {
	d := newDiagnostics()
	out := Projection{ResultID: s.Meta.ResultID, Revision: s.Meta.ResultRevision, Selections: []ProjectedValue{}}
	if s.Meta.Mode != ctx.Mode || !resultWorkflowMatches(s.Meta, ctx) || s.Meta.SchemaVersion != SchemaVersion || s.Meta.ResultRevision < 1 {
		add(&d, "$", "scope", "bound snapshot", "invalid binding", "Read a snapshot belonging to the authorized mode.")
	}
	for _, t := range selections {
		if !allowed(t, authorized, resultTargetScope(ctx)) {
			add(&d, "$.selections", "authorization", "authorized selection", "forbidden selection", "Request only factory-authorized fields or items.")
			continue
		}
		v, _ := locate(&s.Result, t)
		if v.IsValid() && t.ID != "" {
			if v.Kind() != reflect.Slice {
				v = reflect.Value{}
			} else {
				index := selected(v, t.ID)
				if index < 0 {
					v = reflect.Value{}
				} else {
					v = v.Index(index)
				}
			}
		}
		if !v.IsValid() {
			add(&d, "$.selections", "target", "existing field or item", "missing target", "Read an existing authorized target.")
			continue
		}
		data, err := json.Marshal(v.Interface())
		if err != nil {
			add(&d, "$.selections", "encoding", "serializable value", "invalid baseline", "Use a validated baseline.")
			continue
		}
		out.Selections = append(out.Selections, ProjectedValue{Target: t, Value: data})
	}
	finish(&d)
	if !d.Valid {
		return Projection{}, d
	}
	return out, d
}
