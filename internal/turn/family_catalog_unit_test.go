package turn_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

type catalogUnitOwner struct {
	binding  store.ResultBinding
	material []byte
	err      error
}

func (o catalogUnitOwner) Binding() store.ResultBinding        { return o.binding }
func (o catalogUnitOwner) PermissionMaterial() ([]byte, error) { return o.material, o.err }

func catalogUnitOptions(t *testing.T) turn.FamilyDispatcherOptions {
	t.Helper()
	app := feedbackUnitApp()
	feedback, err := turn.NewFeedbackService(app, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Construction inspects immutable material only; this inert handle cannot do I/O.
	receipts, err := turn.NewReceiptService(&store.DB{}, app.binding, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return turn.FamilyDispatcherOptions{Service: app, Feedback: feedback, Receipts: receipts}
}
func catalogUnitRegistration(t *testing.T, o turn.FamilyDispatcherOptions) turn.FamilyRegistration {
	t.Helper()
	return turn.FamilyRegistration{
		Name: "issues_list", Owner: catalogUnitOwner{binding: o.Service.Binding(), material: []byte(`{"repository":"owner/repo"}`)},
		Handle: func(context.Context, string, []byte) (json.RawMessage, error) {
			t.Error("construction dispatched a domain handler")
			return nil, errors.New("unexpected dispatch")
		},
	}
}

func TestFamilyCatalogUnit_RejectsIncompleteConstruction(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *turn.FamilyDispatcherOptions)
	}{
		{"missing application", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Service = nil }},
		{"missing feedback", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Feedback = nil }},
		{"missing receipts", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Receipts = nil }},
		{"missing application digest", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			app, ok := o.Service.(*feedbackUnitApplication)
			if !ok {
				t.Fatalf("unexpected application type %T", o.Service)
			}
			app.digest = ""
		}},
		{"different attempt", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			app, ok := o.Service.(*feedbackUnitApplication)
			if !ok {
				t.Fatalf("unexpected application type %T", o.Service)
			}
			app.binding.Attempt = "other"
		}},
		{"missing owner", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Registrations[0].Owner = nil }},
		{"missing handler", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Registrations[0].Handle = nil }},
		{"foreign owner", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			owner, ok := o.Registrations[0].Owner.(catalogUnitOwner)
			if !ok {
				t.Fatalf("unexpected owner type %T", o.Registrations[0].Owner)
			}
			owner.binding.Task = "github:foreign/repo#1"
			o.Registrations[0].Owner = owner
		}},
		{"invalid owner material", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			owner, ok := o.Registrations[0].Owner.(catalogUnitOwner)
			if !ok {
				t.Fatalf("unexpected owner type %T", o.Registrations[0].Owner)
			}
			owner.material = []byte("not JSON")
			o.Registrations[0].Owner = owner
		}},
		{"unknown tool", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Registrations[0].Name = "grant_creation" }},
		{"duplicate tool", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			o.Registrations = append(o.Registrations, o.Registrations[0])
		}},
		{"replace receipt handler", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Registrations[0].Name = "receipts_get" }},
		{"replace feedback handler", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.Registrations[0].Name = "feedback_preflight" }},
		{"retired creation tool", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			o.Registrations[0].Name = "issues_create_batch"
		}},
		{"nonpositive comment target", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.CommentTargets = []int{0} }},
		{"duplicate comment target", func(t *testing.T, o *turn.FamilyDispatcherOptions) { o.CommentTargets = []int{1, 1} }},
		{"excessive comment targets", func(t *testing.T, o *turn.FamilyDispatcherOptions) {
			for n := 1; n <= 101; n++ {
				o.CommentTargets = append(o.CommentTargets, n)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			o := catalogUnitOptions(t)
			o.Registrations = []turn.FamilyRegistration{catalogUnitRegistration(t, o)}
			tc.change(t, &o)
			digest, err := turn.FamilyPermissionDigest(o)
			if err == nil || digest != "" {
				t.Fatalf("unsafe construction produced digest=%q err=%v", digest, err)
			}
			dispatcher, err := turn.NewFamilyToolDispatcher(nil, o)
			if err == nil || dispatcher != nil {
				t.Fatalf("unsafe dispatcher=%v err=%v", dispatcher, err)
			}
		})
	}
}

func TestFamilyCatalogUnit_RegistrationBindsScope(t *testing.T) {
	t.Parallel()
	o := catalogUnitOptions(t)
	baseline, err := turn.FamilyPermissionDigest(o)
	if err != nil {
		t.Fatal(err)
	}
	registration := catalogUnitRegistration(t, o)
	o.Registrations = []turn.FamilyRegistration{registration}
	registered, err := turn.FamilyPermissionDigest(o)
	if err != nil || registered == baseline {
		t.Fatalf("catalog omitted from binding: digest=%q err=%v", registered, err)
	}
	owner, ok := registration.Owner.(catalogUnitOwner)
	if !ok {
		t.Fatalf("unexpected owner type %T", registration.Owner)
	}
	owner.material = []byte(`{"repository":"owner/repo","targets":[273]}`)
	o.Registrations[0].Owner = owner
	changed, err := turn.FamilyPermissionDigest(o)
	if err != nil || changed == registered {
		t.Fatalf("owner scope omitted from binding: digest=%q err=%v", changed, err)
	}
	failure := errors.New("owner refuses immutable scope")
	owner.err = failure
	o.Registrations[0].Owner = owner
	digest, err := turn.FamilyPermissionDigest(o)
	if !errors.Is(err, failure) || digest != "" {
		t.Fatalf("owner refusal lost: digest=%q err=%v", digest, err)
	}
}
