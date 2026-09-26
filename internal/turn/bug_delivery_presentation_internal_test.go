package turn

import (
	"reflect"
	"testing"
)

func TestBugDeliveryPresentationOnlyExplicitEvidenceRequests(t *testing.T) {
	for _, tc := range []struct {
		body string
		want []string
	}{
		{"analysis", []string{"analysis"}},
		{"show regression, focused, full", []string{"regression", "focused", "full"}},
		{"/bug-review abc clarify show analysis, repair", []string{"analysis", "repair"}},
		{"Why does the analysis miss ordering?", nil},
		{"/bug-review other clarify analysis", nil},
		{"analysis, analysis", nil},
		{"show analysis and approve this", nil},
		{"/bug-review abc repair analysis", nil},
		{"", nil},
	} {
		t.Run(tc.body, func(t *testing.T) {
			got := bugRequestedEvidenceSections(tc.body, "abc")
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
