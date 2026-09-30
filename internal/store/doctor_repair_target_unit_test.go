package store_test

import (
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorRowRepairCatalogBoundaries(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, kind, target, before, after string
		allowed                           bool
	}{
		{"poll flag/reset", "poll_flag", "session:406", "1", "0", true},
		{"poll flag/set", "poll_flag", "session:406", "0", "1", true},
		{"poll flag/invalid value", "poll_flag", "session:406", "1", "2", false},
		{"unchanged state/refused", "poll_flag", "session:406", "1", "1", false},
		{"run interruption/retains failure", "run_interrupt", "run:406", "running", "failed", true},
		{"completed run/cannot rewrite history", "run_interrupt", "run:406", "succeeded", "failed", false},
		{"session lock/release recorded owner", "session_unlock", "session_lock:406", "daemon-instance", "", true},
		{"session lock/no ownership transfer", "session_unlock", "session_lock:406", "daemon-instance", "different-instance", false},
		{"confirmed report/acknowledge", "report_completion", "report:receipt-key", "confirmed", "completed", true},
		{"uncertain report/no forged acknowledgment", "report_completion", "report:receipt-key", "possibly_sent", "completed", false},
		{"watermark/rewind zero", "watermark_rewind", "watermark:406", "100", "0", true},
		{"watermark/no negative position", "watermark_rewind", "watermark:406", "100", "-1", false},
		{"watermark/no nonnumeric position", "watermark_rewind", "watermark:406", "100", "all", false},
		{"watermark/no overflowing position", "watermark_rewind", "watermark:406", "9223372036854775808", "0", false},
		{"dispatch/cancel reserved", "graph_dispatch_cancel", "graph_dispatch:406", "reserved", "cancelled", true},
		{"dispatch/preserve completed", "graph_dispatch_cancel", "graph_dispatch:406", "completed", "cancelled", false},
		{"wait/cancel allocated", "graph_wait_cancel", "owner_wait:406", "allocated", "cancelled", true},
		{"wait/preserve answered", "graph_wait_cancel", "owner_wait:406", "answered", "cancelled", false},
		{"attempt/cancel pending", "graph_attempt_cancel", "graph_attempt:406", "pending", "cancelled", true},
		{"graph/record failure", "graph_fail", "graph:406", "running", "failed", true},
		{"unknown operation/no arbitrary SQL", "execute_sql", "run:406", "running", "failed", false},
		{"wrong row kind/refused", "run_interrupt", "session:406", "running", "failed", false},
		{"missing separator/refused", "run_interrupt", "run406", "running", "failed", false},
		{"empty row key/refused", "run_interrupt", "run:", "running", "failed", false},
		{"negative row key/refused", "run_interrupt", "run:-1", "running", "failed", false},
		{"zero row key/refused", "run_interrupt", "run:0", "running", "failed", false},
		{"ambiguous numeric alias/refused", "run_interrupt", "run:0406", "running", "failed", false},
		{"foreign path key/refused", "run_interrupt", "run:../406", "running", "failed", false},
		{"SQL-looking key/refused", "run_interrupt", "run:406 OR 1=1", "running", "failed", false},
		{"empty report key/refused", "report_completion", "report:", "confirmed", "completed", false},
		{"report key whitespace/refused", "report_completion", "report:foreign key", "confirmed", "completed", false},
		{"opaque wait key/accepted", "graph_wait_cancel", "owner_wait:wait-406", "waiting", "cancelled", true},

		{"oversized target/refused", "run_interrupt", "run:" + strings.Repeat("4", 513), "running", "failed", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := store.DoctorRowRepairTarget(tt.kind, tt.target, tt.before, tt.after); got != tt.allowed {
				t.Fatalf("catalog allowed=%t; want %t", got, tt.allowed)
			}
		})
	}
}
