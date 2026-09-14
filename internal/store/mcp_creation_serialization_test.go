package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCreationReservation_HidesInternalDispatchAuthority(t *testing.T) {
	t.Parallel()
	permit := &CreationPermit{operation: 7, owner: "private-dispatch-owner-never-model-visible", lease: MCPLease{Instance: "private-runtime", PermissionDigest: "private-permissions", Binding: ResultBinding{Attempt: "private-attempt"}}}
	reservation := CreationReservation{Operation: CreationOperation{ID: 7, State: "reserved", Artifacts: 2, ProposalHash: "public-reviewed-content-hash"}, Permit: permit}
	data, err := json.Marshal(reservation)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{permit.owner, permit.lease.Instance, permit.lease.PermissionDigest, permit.lease.Binding.Attempt, "Permit", "owner", "lease"} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatalf("reservation serialized internal authority field %q", secret)
		}
	}
	var projection struct {
		Operation CreationOperation `json:"operation"`
	}
	if err := json.Unmarshal(data, &projection); err != nil || projection.Operation.ID != 7 || projection.Operation.State != "reserved" {
		t.Fatalf("safe operation correlation lost: %+v %v", projection, err)
	}
	typ := reflect.TypeOf(*permit)
	for i := range typ.NumField() {
		if field := typ.Field(i); field.IsExported() {
			t.Fatalf("permit exposes authority field %s", field.Name)
		}
	}
}
