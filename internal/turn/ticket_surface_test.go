package turn_test

import (
	"reflect"
	"testing"

	"github.com/cloudvane/litefactory/internal/turn"
)

// A ticket workflow may close a ticket after confirmed integration, but it
// cannot choose or mutate the code repository, branch or pull request.
func TestTicketWorkflowDoesNotAcquireCodeDelivery(t *testing.T) {
	ticketType := reflect.TypeOf((*turn.TicketWorkflowBridge)(nil))
	code := reflect.TypeOf((*turn.CodeDelivery)(nil)).Elem()
	for i := range code.NumMethod() {
		method := code.Method(i)
		if _, ok := ticketType.MethodByName(method.Name); ok {
			t.Errorf("ticket workflow exposes code delivery operation %s", method.Name)
		}
	}
	for _, name := range []string{"GetIssue", "CloseCompletedIssue", "CreateIssue", "PostComment", "AddSubIssue"} {
		if _, ok := code.MethodByName(name); ok {
			t.Errorf("code delivery exposes ticket operation %s", name)
		}
	}
}
