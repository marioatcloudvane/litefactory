package turn_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func receiptUnitBinding() store.ResultBinding {
	return store.ResultBinding{Task: "github:owner/repo#273", RunID: 7, Attempt: "current", WorkContext: "private-context-sentinel", ContextHash: "private-hash-sentinel"}
}

func receiptUnitHistory() store.FamilyScope {
	return store.FamilyScope{Repository: "owner/repo", Task: "github:owner/repo#273", RunID: 6, Attempt: "previous"}
}

func TestReceiptUnit_RejectsIncompleteBinding(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*store.ResultBinding)
	}{
		{"missing task", func(b *store.ResultBinding) { b.Task = "" }},
		{"missing run", func(b *store.ResultBinding) { b.RunID = 0 }},
		{"missing attempt", func(b *store.ResultBinding) { b.Attempt = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			binding := receiptUnitBinding()
			tc.change(&binding)
			got, err := turn.NewReceiptService(&store.DB{}, binding, nil, nil)
			if err == nil || got != nil {
				t.Fatalf("incomplete binding accepted: service=%v err=%v", got, err)
			}
		})
	}
}

func TestReceiptUnit_HistoricalScopeRefusal(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*store.FamilyScope)
	}{
		{"foreign repository", func(s *store.FamilyScope) { s.Repository = "other/repo" }},
		{"foreign task", func(s *store.FamilyScope) { s.Task = "github:owner/repo#999" }},
		{"missing run", func(s *store.FamilyScope) { s.RunID = 0 }},
		{"missing attempt", func(s *store.FamilyScope) { s.Attempt = "" }},
		{"oversized attempt", func(s *store.FamilyScope) { s.Attempt = strings.Repeat("a", 257) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scope := receiptUnitHistory()
			tc.change(&scope)
			// Inert handle: construction must not open or query a database.
			got, err := turn.NewReceiptService(&store.DB{}, receiptUnitBinding(), []store.FamilyScope{scope}, nil)
			if !errors.Is(err, store.ErrResultAuthority) || got != nil {
				t.Fatalf("unauthorized history admitted: service=%v err=%v", got, err)
			}
		})
	}
}

func TestReceiptUnit_ImmutablePermissionMaterial(t *testing.T) {
	t.Parallel()
	history := []store.FamilyScope{receiptUnitHistory()}
	projection := func(context.Context, store.FamilyRequest) (turn.ReceiptDimensions, []turn.PublicationIdentity, error) {
		t.Fatal("permission inspection invoked a domain projection")
		return turn.ReceiptDimensions{}, nil, nil
	}
	projections := map[string]turn.FamilyProjection{"submission": projection}
	service, err := turn.NewReceiptService(&store.DB{}, receiptUnitBinding(), history, projections)
	if err != nil {
		t.Fatal(err)
	}
	before, err := service.PermissionMaterial()
	if err != nil {
		t.Fatal(err)
	}
	history[0].Repository = "foreign/repo"
	projections["creation"] = projection
	delete(projections, "submission")
	after, err := service.PermissionMaterial()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("caller mutation changed permissions: %s -> %s", before, after)
	}
	for _, secret := range []string{"private-context-sentinel", "private-hash-sentinel"} {
		if bytes.Contains(after, []byte(secret)) {
			t.Fatalf("permission material exposed %q", secret)
		}
	}
	var material struct {
		Scopes      []store.FamilyScope
		Projections []string
	}
	if err := json.Unmarshal(after, &material); err != nil {
		t.Fatal(err)
	}
	if len(material.Scopes) != 2 || material.Scopes[0] != receiptUnitHistory() || material.Scopes[1].Attempt != "current" || len(material.Projections) != 1 || material.Projections[0] != "submission" {
		t.Fatalf("incorrect authorized scope snapshot: %s", after)
	}
	after[0] = '!'
	fresh, err := service.PermissionMaterial()
	if err != nil || !bytes.Equal(before, fresh) {
		t.Fatalf("returned bytes alias permissions: %s err=%v", fresh, err)
	}
}

func TestReceiptUnit_PermissionIdentity(t *testing.T) {
	t.Parallel()
	base, err := turn.NewReceiptService(&store.DB{}, receiptUnitBinding(), []store.FamilyScope{receiptUnitHistory()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	want, err := base.PermissionMaterial()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		scopes []store.FamilyScope
		same   bool
	}{
		{"duplicate history retains identity", []store.FamilyScope{receiptUnitHistory(), receiptUnitHistory()}, true},
		{"removing history changes identity", nil, false},
		{"another historical attempt changes identity", []store.FamilyScope{{Repository: "owner/repo", Task: "github:owner/repo#273", RunID: 6, Attempt: "different"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			svc, err := turn.NewReceiptService(&store.DB{}, receiptUnitBinding(), tc.scopes, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := svc.PermissionMaterial()
			if err != nil || bytes.Equal(got, want) != tc.same {
				t.Fatalf("permission identity: %s vs %s err=%v", got, want, err)
			}
		})
	}
}

func TestReceiptUnit_MalformedLookupBeforeObservation(t *testing.T) {
	for _, tc := range []struct{ name, arguments string }{
		{"missing identity", `{}`},
		{"mismatched identity", `{"request_id":"other"}`},
		{"caller history scope", `{"request_id":"known","run_id":6}`},
		{"caller repository", `{"request_id":"known","repository":"foreign/repo"}`},
		{"duplicate identity", `{"request_id":"known","request_id":"other"}`},
		{"trailing document", `{"request_id":"known"} {}`},
		{"oversized identity", `{"request_id":"` + strings.Repeat("a", 129) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A zero service has no I/O owner. Malformed calls must fail before observation.
			var service turn.ReceiptService
			got, err := service.HandleGet(context.Background(), "known", []byte(tc.arguments))
			if err == nil || got != nil {
				t.Fatalf("malformed lookup accepted: %s err=%v", got, err)
			}
		})
	}
}
