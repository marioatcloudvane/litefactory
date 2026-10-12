package turn_test

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

// A ticket dependency must never carry the authority to integrate code.
func TestTicketDependenciesExcludeCodeDelivery(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name       string
		dependency any
	}{
		{"adapter", (*github.Tickets)(nil)},
		{"workflow compatibility", (*turn.TicketWorkflowBridge)(nil)},
		{"publication compatibility", (*turn.TicketPublicationBridge)(nil)},
		{"plan compatibility", (*turn.TicketPlanBridge)(nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, ok := tt.dependency.(turn.CodeDelivery); ok {
				t.Fatal("ticket dependency acquired code integration authority")
			}
			if _, ok := tt.dependency.(turn.BugCodeForge); ok {
				t.Fatal("ticket dependency acquired bug integration authority")
			}
		})
	}
	var _ turn.CodeDelivery = (*github.Client)(nil)
}
