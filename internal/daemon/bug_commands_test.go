package daemon

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/turn"
)

// Every command a Bug comment tells the owner to reply with must reach the
// Bug runtime; the exhaustion decision was once answered as unknown.
func TestBugControlCommandsReachTheBugRuntime(t *testing.T) {
	for _, body := range []string{
		"/approve-bug analysis 1",
		"/bug-review abc clarify why",
		"/retry-bug-tests",
		"/grant-bug-attempt 1 try a temporary home",
		"/stop-bug 1",
	} {
		if name := turn.ParseCommand(body).Name; !isBugControlCommand(name) {
			t.Errorf("%q parses to %q, which is not routed to the Bug runtime", body, name)
		}
	}
	for _, body := range []string{"/implement", "/research go", "please continue"} {
		if name := turn.ParseCommand(body).Name; isBugControlCommand(name) {
			t.Errorf("%q is routed to the Bug runtime", body)
		}
	}
}
