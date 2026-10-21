package cli

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestAmendStandardsCategory(t *testing.T) {
	t.Parallel()
	old := standards.Operation{Kind: "category.add", Category: &standards.Category{ID: "c", Name: "Old"}}
	replace := standards.Operation{Kind: "category.replace", Category: &standards.Category{ID: "c", Name: "New"}}
	for _, tc := range []struct {
		name, action string
		op           standards.Operation
		want         []standards.Operation
		fail         bool
	}{
		{"replace pending addition", "replace", replace, []standards.Operation{{Kind: "category.add", Category: replace.Category}}, false},
		{"remove pending addition", "remove", standards.Operation{Kind: "category.remove", ID: "c"}, []standards.Operation{}, false},
		{"reject duplicate addition", "add", old, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := []standards.Operation{old}
			got, err := amendStandardsCategory(input, tc.op, tc.action, "c")
			if (err != nil) != tc.fail || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, %v; want %+v", got, err, tc.want)
			}
			if !reflect.DeepEqual(input, []standards.Operation{old}) {
				t.Fatal("amendment changed original operations")
			}
		})
	}
}

func TestStandardsErrorPolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"invalid change", standards.ErrInvalid, ExitFailure},
		{"stale proposal", store.ErrStandardsConflict, ExitFailure},
		{"owner setup", standards.ErrOwnerRequired, ExitPrecondition},
		{"bad invocation", Usagef(nil, "missing version"), ExitUsage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCode(standardsCommandError(tc.err)); got != tc.want {
				t.Fatalf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
