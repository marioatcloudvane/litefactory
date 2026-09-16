package daemon_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/store"
)

// Exercise the actual production constructor with a real temporary store and
// local HTTP forge fixture. Auto-start controls native registration; disabling
// it must still construct fresh shared-result authority and invalidate old leases.
func TestOpenSharedResultAuthorityIndependentOfNativeAutoStart(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		if enabled {
			name = "enabled"
		}
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			session, err := f.db.GetOrCreateSession(t.Context(), f.repo, testIssue)
			if err != nil {
				t.Fatal(err)
			}
			run := f.allocateRunningRun(session, 1)
			binding, err := f.db.BindResultAttempt(t.Context(), store.ResultAttempt{Task: session.TaskRef, Mode: run.Mode, WorkContext: "startup-proof", Attempt: "old-attempt", RunID: run.ID, SchemaVersion: 1, WorkflowVersion: 2, SubmissionVersion: 2})
			if err != nil {
				t.Fatal(err)
			}
			instance := strings.Repeat("a", 64)
			if err := f.db.StartMCPInstance(t.Context(), instance); err != nil {
				t.Fatal(err)
			}
			lease := store.MCPLease{Binding: binding, Instance: instance, PermissionDigest: strings.Repeat("b", 64), ExpiresAt: time.Now().Add(time.Hour)}
			if err := f.db.BindMCPLease(t.Context(), lease); err != nil {
				t.Fatal(err)
			}
			if err := f.db.CheckMCPLease(t.Context(), lease); err != nil {
				t.Fatal(err)
			}
			setup := f.setup(fakeClaude(t))
			setup.Config.Settings.MCPAutoStart = enabled
			loop, _, err := daemon.Open(t.Context(), setup)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := loop.Close(); err != nil {
					t.Error(err)
				}
			}()
			if err := f.db.CheckMCPLease(t.Context(), lease); err == nil {
				t.Fatal("production startup retained obsolete runtime authority")
			}
		})
	}
}
