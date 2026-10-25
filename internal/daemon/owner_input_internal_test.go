package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestOwnerInputTargetRequiresExplicitExactIdentity(t *testing.T) {
	for _, tc := range []struct {
		body string
		want store.OwnerInputTarget
		bad  bool
	}{
		{body: "Please tell instance=7 to use ISO dates."},
		{body: "Input-Target: instance=7 node=worker launch=9\nUse ISO dates.", want: store.OwnerInputTarget{InstanceID: 7, Node: "worker", LaunchID: 9}},
		{body: "Input-Target: instance=7 node=worker instance=8", bad: true},
		{body: "Input-Target: node=worker", bad: true},
		{body: "Input-Target: instance=7 mode=implement", bad: true},
	} {
		got, err := ownerInputTarget(tc.body)
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Errorf("target for %q=%+v err=%v", tc.body, got, err)
		}
	}
}
