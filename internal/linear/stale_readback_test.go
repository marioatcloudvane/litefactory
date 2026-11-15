package linear

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// TestUpdateContentWaitsForStaleReadback reproduces MAR-116 (#1525): Linear
// accepted the first story draft over the shell, but the readback right after
// the mutation still served the shell ("# Customers" behind the story marker),
// so the write failed as "content readback differs". The stored description
// was the draft, with Linear's usual dropped blank lines between list items.
func TestUpdateContentWaitsForStaleReadback(t *testing.T) {
	old := staleReadbackDelays
	staleReadbackDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	t.Cleanup(func() { staleReadbackDelays = old })
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	expected, _ := mar12Pair(t)
	marker, _, _ := strings.Cut(expected, "\n")
	shell := marker + "\n# Customers\n\nCustomers list.\n\n_First draft in progress. This issue is updated with the story draft as soon as it is ready._\n"
	var stored []string
	lines := strings.Split(EncodeContent(expected), "\n")
	for i, line := range lines {
		// Linear stores lists tight, so the stored body is shorter than the
		// metadata line map.
		if line == "" && i > 0 && i+1 < len(lines) && strings.HasPrefix(lines[i-1], "- ") && strings.HasPrefix(lines[i+1], "- ") {
			continue
		}
		stored = append(stored, line)
	}
	for _, stale := range []int32{1, 3, 4} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			output.Reset()
			var mutated atomic.Bool
			var staleReads atomic.Int32
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				var req struct{ Query string }
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Error(err)
					return
				}
				if strings.Contains(req.Query, "issueUpdate") {
					mutated.Store(true)
					_, _ = fmt.Fprint(w, `{"data":{"issueUpdate":{"success":true,"issue":{"id":"uuid-ticket"}}}}`)
					return
				}
				description := EncodeContent(shell)
				if mutated.Load() && staleReads.Add(1) > stale {
					description = strings.Join(stored, "\n")
				}
				encoded, _ := json.Marshal(description)
				raw := strings.Replace(issueJSON("uuid-ticket", "project"), `"description":"Body"`, `"description":`+string(encoded), 1)
				_, _ = fmt.Fprintf(w, `{"data":{"issue":%s}}`, raw)
			})
			ref := ticket.Ref{Identity: c.identity("uuid-ticket"), Locator: c.locator("uuid-ticket")}
			prior, err := c.Read(t.Context(), ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.UpdateContent(t.Context(), ref, prior.Revision, mustBody(t, "Title"), mustBody(t, expected))
			if stale > int32(len(staleReadbackDelays)) {
				// Linear never caught up within the bound: still a conflict.
				var failure *ticket.Failure
				if !errors.As(err, &failure) || failure.Kind != ticket.Conflict {
					t.Fatalf("persistent stale readback accepted: %v", err)
				}
				// The diagnostic is the one MAR-116 logged: the request marker
				// expected on line 2, the shell heading observed there.
				if !strings.Contains(output.String(), `"line":2,"column":1,"expected":"<!-- lf:plan-request digest=`) || !strings.Contains(output.String(), `"observed":"# Customers"`) {
					t.Fatalf("unexpected diagnostic: %s", output.String())
				}
				return
			}
			if err != nil {
				t.Logf("diagnostic: %s", output.String())
				t.Fatalf("stale readback after an accepted write failed the update: %v", err)
			}
			if !ContentEquivalent(expected, got.Body) {
				t.Fatal("returned snapshot is not the written draft")
			}
		})
	}
}
