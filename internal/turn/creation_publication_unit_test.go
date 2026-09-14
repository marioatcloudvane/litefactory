package turn

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func TestCreationPublicationService_DecodeExactReviewedEnvelope(t *testing.T) {
	t.Parallel()
	svc := &CreationPublicationService{receipts: &ReceiptService{binding: store.ResultBinding{Task: "github:owner/repo#273"}, scope: store.FamilyScope{Repository: "owner/repo"}}}
	envelope := submission.CreationEnvelope{Version: 1, Repository: "owner/repo", SourceTask: "github:owner/repo#273", Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Exact title ", Body: " exact body\n", LinkIssues: []int{273}}}}
	raw, hash, err := submission.CanonicalCreation(envelope)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		input     []byte
		valid     bool
		authority bool
	}{
		{name: "exact bytes preserved", input: raw, valid: true},
		{name: "foreign repository", input: []byte(strings.Replace(string(raw), `"repository":"owner/repo"`, `"repository":"foreign/repo"`, 1)), authority: true},
		{name: "different source", input: []byte(strings.Replace(string(raw), "repo#273", "repo#274", 1)), authority: true},
		{name: "trailing document", input: append(append([]byte(nil), raw...), []byte(` {}`)...)},
	}
	for _, field := range []string{`"token":"approved"`, `"confirmation_token":"yes"`, `"grant":42`, `"permit":{}`, `"hash":"` + hash + `"`, `"allow_mcp_story_creation":true`, `"comment_volume":1000`} {
		cases = append(cases, struct {
			name      string
			input     []byte
			valid     bool
			authority bool
		}{name: "caller authority rejected/" + field, input: []byte(string(raw[:len(raw)-1]) + "," + field + "}")})
	}
	cases = append(cases, struct {
		name      string
		input     []byte
		valid     bool
		authority bool
	}{name: "nested authority field rejected", input: []byte(strings.Replace(string(raw), `"title":`, `"approved":true,"title":`, 1))})
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := svc.decode(tt.input)
			if tt.valid {
				if err != nil || !reflect.DeepEqual(got, envelope) {
					t.Fatalf("decoded=%+v error=%v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatal("untrusted envelope accepted")
			}
			if tt.authority && !errors.Is(err, store.ErrCreationAuthority) {
				t.Fatalf("scope error=%v", err)
			}
		})
	}
}

func TestCreationPublicationService_PermissionMaterialExcludesPermit(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			t.Parallel()
			svc := &CreationPublicationService{authorizer: &CreationAuthorizer{enabled: enabled, ownerID: 42, botID: 99}}
			raw, err := svc.PermissionMaterial()
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err = json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, map[string]any{"Enabled": enabled, "BotID": float64(99)}) {
				t.Fatalf("unexpected authority material: %s", raw)
			}
			reservation := store.CreationReservation{Permit: &store.CreationPermit{}}
			encoded, err := json.Marshal(reservation)
			if err != nil {
				t.Fatal(err)
			}
			var public map[string]json.RawMessage
			if err = json.Unmarshal(encoded, &public); err != nil {
				t.Fatal(err)
			}
			if len(public) != 1 || public["operation"] == nil {
				t.Fatalf("private permit exposed: %s", encoded)
			}
		})
	}
}
