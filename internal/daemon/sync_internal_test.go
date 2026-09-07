package daemon

import (
	"reflect"
	"testing"
)

// TestResolveSync_DecisionTable pins resolveSync's decision table (AC-7.1):
// every shape of label a maintainer can put on an issue, and exactly one of
// the outcomes resolveSync may reach for it. It mirrors
// TestResolveAgent_DecisionTable rung for rung, because resolveSync copies
// resolveAgent's ladder byte for byte (T-7.1).
//
// It calls resolveSync directly with a plain []string — the function takes
// no daemon state and performs no I/O, so nothing here needs a fake or a
// mock.
func TestResolveSync_DecisionTable(t *testing.T) {
	tests := []struct {
		name         string
		labels       []string
		wantResolved bool
		wantValue    bool
		wantRefusal  syncRefusal
		wantMiscased bool
	}{
		{
			name: "no labels at all/the zero value: no refusal, nothing resolved",
		},
		{
			name:   "labels present, none of them a sync label/the zero value",
			labels: []string{"bug", "Mode: Plan", "Coding Agent: Claude", "priority:high"},
		},
		{
			name:         "Sync: On/resolves true",
			labels:       []string{"Sync: On"},
			wantResolved: true,
			wantValue:    true,
		},
		{
			name:         "Sync: Off/resolves false",
			labels:       []string{"Sync: Off"},
			wantResolved: true,
			wantValue:    false,
		},
		{
			name:         "an unrelated label beside a valid one/resolves, ignoring the unrelated label",
			labels:       []string{"bug", "Sync: Off"},
			wantResolved: true,
			wantValue:    false,
		},
		{
			name:         "mis-cased prefix, lowercase value/malformed, prefix flagged",
			labels:       []string{"SYNC: on"},
			wantRefusal:  syncRefusalMalformed,
			wantMiscased: true,
		},
		{
			name:         "fully uppercase prefix/malformed, prefix flagged",
			labels:       []string{"SYNC: ON"},
			wantRefusal:  syncRefusalMalformed,
			wantMiscased: true,
		},
		{
			name:         "mis-cased prefix over an otherwise well-formed value/still the prefix failure",
			labels:       []string{"sync: Off"},
			wantRefusal:  syncRefusalMalformed,
			wantMiscased: true,
		},
		{
			name:        "byte-exact prefix, invalid value/malformed on the value shape, prefix not flagged",
			labels:      []string{"Sync: maybe"},
			wantRefusal: syncRefusalMalformed,
		},
		{
			name:        "empty value/malformed, prefix not flagged",
			labels:      []string{"Sync: "},
			wantRefusal: syncRefusalMalformed,
		},
		{
			name:        "value with the wrong casing/malformed, prefix not flagged",
			labels:      []string{"Sync: on"},
			wantRefusal: syncRefusalMalformed,
		},
		{
			name:   "too short to carry the prefix/ignored entirely",
			labels: []string{"Sync"},
		},
		{
			name:        "two valid labels, same value/ambiguous, neither is chosen",
			labels:      []string{"Sync: On", "Sync: On"},
			wantRefusal: syncRefusalAmbiguous,
		},
		{
			name:        "two valid labels, conflicting values/ambiguous, neither is chosen",
			labels:      []string{"Sync: On", "Sync: Off"},
			wantRefusal: syncRefusalAmbiguous,
		},
		{
			name:        "two labels, mixed casing and reversed order/ambiguous regardless of order or casing",
			labels:      []string{"SYNC: Off", "Sync: On"},
			wantRefusal: syncRefusalAmbiguous,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := resolveSync(tc.labels)

			if decision.Resolved != tc.wantResolved {
				t.Errorf("Resolved = %t, want %t", decision.Resolved, tc.wantResolved)
			}
			if decision.Resolved && decision.Value != tc.wantValue {
				t.Errorf("Value = %t, want %t", decision.Value, tc.wantValue)
			}
			if decision.Refusal != tc.wantRefusal {
				t.Errorf("Refusal = %v, want %v", decision.Refusal, tc.wantRefusal)
			}
			if decision.refused() != (tc.wantRefusal != syncRefusalNone) {
				t.Errorf("refused() = %t, disagrees with Refusal = %v", decision.refused(), decision.Refusal)
			}
			// A decision cannot be both resolved and refused: the zero value
			// covers silence, Resolved covers a clean resolution, and Refusal
			// covers everything else. No case in this table wants more than
			// one of those at once, so this holds for every row.
			if decision.Resolved && decision.refused() {
				t.Errorf("decision is both resolved and refused: %+v", decision)
			}
			if decision.MiscasedPrefix != tc.wantMiscased {
				t.Errorf("MiscasedPrefix = %t, want %t", decision.MiscasedPrefix, tc.wantMiscased)
			}
			// SEC-5/S-9: MiscasedPrefix is set on exactly the prefix-caused
			// malformed refusal, never on the value-caused one and never on
			// ambiguous.
			if tc.wantMiscased && tc.wantRefusal != syncRefusalMalformed {
				t.Fatalf("test table error: %s wants MiscasedPrefix without wanting the malformed refusal", tc.name)
			}
		})
	}
}

// TestResolveSync_SiblingLabelsDoNotInterfere confirms that resolving one
// Sync: label is unaffected by unrelated labels sharing the same slice,
// in either direction: an unrelated label never contributes to ambiguity,
// and its presence or absence never changes the outcome for the sync label
// itself.
func TestResolveSync_SiblingLabelsDoNotInterfere(t *testing.T) {
	alone := resolveSync([]string{"Sync: On"})
	withSiblings := resolveSync([]string{"bug", "Mode: Plan", "Sync: On", "Coding Agent: Claude"})

	if alone != withSiblings {
		t.Errorf("decision changed depending on unrelated sibling labels: alone=%+v, withSiblings=%+v",
			alone, withSiblings)
	}
}

// TestSyncDecision_NeverCarriesRawLabelText is a structural guarantee for
// SEC-5/S-9: syncDecision has no field capable of carrying untrusted label
// text onward at all, so there is no accessor a refusal could leak through
// even by future accident. This is checked by reflection rather than by
// reading the implementation, because the property being pinned is the
// *shape* of the type, not what today's resolveSync happens to put in it.
func TestSyncDecision_NeverCarriesRawLabelText(t *testing.T) {
	typ := reflect.TypeOf(syncDecision{})

	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		switch field.Type.Kind() {
		case reflect.Bool, reflect.Int:
			// Resolved/Value/MiscasedPrefix are bool, Refusal is a private
			// int-backed enum: none of these can hold arbitrary text.
		default:
			t.Errorf("field %s has kind %s, which could carry arbitrary text; "+
				"syncDecision must only ever carry bools and enums", field.Name, field.Type.Kind())
		}
	}
}

// TestSyncDecision_MalformedAndAmbiguousLeakNoLabelText exercises the two
// refusal paths directly and confirms neither field on the resulting
// decision reproduces any of the raw label text it was refused over
// (SEC-5/S-9), belt-and-braces alongside the structural check above.
func TestSyncDecision_MalformedAndAmbiguousLeakNoLabelText(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
	}{
		{"miscased prefix", []string{"SYNC: super-secret-value"}},
		{"invalid value", []string{"Sync: super-secret-value"}},
		{"ambiguous", []string{"Sync: On", "Sync: super-secret-value"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decision := resolveSync(tc.labels)

			if !decision.refused() {
				t.Fatalf("test table error: %s did not produce a refusal", tc.name)
			}
			// There is no string field on syncDecision at all (see
			// TestSyncDecision_NeverCarriesRawLabelText), so there is
			// nowhere the literal label text could have gone. This assertion
			// documents that the two facts a refusal does carry - which rung
			// of the ladder, and whether the prefix itself was mis-cased -
			// say nothing about "super-secret-value".
			if decision.Refusal == syncRefusalNone {
				t.Errorf("Refusal is the zero value on a refused decision: %+v", decision)
			}
		})
	}
}
