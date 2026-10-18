package doctor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

type failingInstructionSource func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error)

func (f failingInstructionSource) Capture(ctx context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
	return f(ctx, role)
}

func oversizedDoctorCapture(t *testing.T, role string, size int) *instructionpackage.RoleCapture {
	t.Helper()
	pkg, err := instructionpackage.Load(t.Context(), fstest.MapFS{
		"metadata.yaml":                           {Data: []byte("schema: 1\nversion: oversized-fixture\n")},
		"catalogue.yaml":                          {Data: []byte("- doctor-diagnosis\n- doctor-repair\n")},
		"agents/doctor-diagnosis.md":              {Data: []byte(strings.Repeat("d", size))},
		"agents/doctor-repair.md":                 {Data: []byte(strings.Repeat("r", size))},
		"skills/doctor-field-guide/SKILL.md":      {Data: []byte("---\nname: doctor-field-guide\ndescription: Test guide.\n---\nGuide.\n")},
		"skills/doctor-repair-authority/SKILL.md": {Data: []byte("---\nname: doctor-repair-authority\ndescription: Test authority.\n---\nAuthority.\n")},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), role)
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestDoctorCaptureFailureNeverLaunchesDiagnosis(t *testing.T) {
	for _, failure := range []string{"cancelled", "wrong-role", "retention-fenced"} {
		t.Run(failure, func(t *testing.T) {
			s := doctorIOService(t)
			runner := &retainedResultRunner{t: t}
			s.Runner = runner
			ref, err := Target("acme/api", 406)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			s.Instructions = failingInstructionSource(func(_ context.Context, role string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
				switch failure {
				case "cancelled":
					cancel()
					return nil, nil, context.Canceled
				case "wrong-role":
					return testDoctorCapture(t, "doctor-repair"), nil, nil
				default:
					if err := s.DB.StopDoctorConversation(t.Context(), ref.String()); err != nil {
						t.Fatal(err)
					}
					return testDoctorCapture(t, role), nil, nil
				}
			})
			conversation, err := s.Diagnose(ctx, DiagnoseRequest{Target: ref, Instructions: "Explain", SourceID: "failure-" + failure})
			if err == nil || runner.calls != 0 {
				t.Fatalf("failure launched provider: %v calls=%d", err, runner.calls)
			}
			if failure == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
			if failure == "retention-fenced" && !errors.Is(err, store.ErrDoctorFenced) {
				t.Fatalf("lost retention fence: %v", err)
			}
			if len(conversation.Attempts) != 1 {
				t.Fatalf("missing failed attempt: %+v", conversation)
			}
			if conversation.Attempts[0].State != "failed" && conversation.Attempts[0].State != "interrupted" {
				t.Fatalf("unsettled attempt: %+v", conversation.Attempts[0])
			}
			if _, err := s.DB.DoctorInstructionCapture(t.Context(), conversation.Attempts[0].ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("failed capture retained: %v", err)
			}
		})
	}
}

func TestDoctorOversizedCapturedInstructionsAreRejected(t *testing.T) {
	diagnosis := oversizedDoctorCapture(t, "doctor-diagnosis", 524288)
	if _, err := BuildPrompt(PromptContext{Instructions: "Explain", Target: "github:acme/api#406", Manifest: PathManifest{TaskRef: "github:acme/api#406", Live: true}}, diagnosis); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("oversized diagnosis: %v", err)
	}
	repair := oversizedDoctorCapture(t, "doctor-repair", 131072)
	if _, err := buildRepairPrompt(repair, nil); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("oversized repair: %v", err)
	}
}

type instructionFailureEnvironment struct{ excludes, stops int }

func (*instructionFailureEnvironment) Preflight(context.Context, naming.Ref) error { return nil }
func (e *instructionFailureEnvironment) Exclude(context.Context, naming.Ref, string) (string, error) {
	e.excludes++
	return "should-not-exclude", nil
}
func (e *instructionFailureEnvironment) StopRun(context.Context, naming.Ref, string, int) error {
	e.stops++
	return nil
}

func TestDoctorBadRepairCapturePrecedesOperationalEffects(t *testing.T) {
	for _, failure := range []string{"invalid", "oversized"} {
		t.Run(failure, func(t *testing.T) {
			s, ref, presentation, path := fileRepairFixture(t, false)
			environment := &instructionFailureEnvironment{}
			s.RepairEnvironment = environment
			s.RepairRunner = repairFixtureRunner{}
			s.RestartRoute = &repairFixtureRestart{}
			s.Instructions = failingInstructionSource(func(context.Context, string) (*instructionpackage.RoleCapture, []instructionpackage.Diagnostic, error) {
				if failure == "invalid" {
					return nil, nil, errors.New("unusable captured package")
				}
				return oversizedDoctorCapture(t, "doctor-repair", 131072), nil, nil
			})
			operation, err := s.Apply(t.Context(), ApplyRequest{Target: ref, SourceID: "bad-capture", Principal: "local-owner", PresentationID: presentation})
			if err == nil || environment.excludes != 0 || environment.stops != 0 {
				t.Fatalf("bad capture allowed effect: %v excludes=%d stops=%d", err, environment.excludes, environment.stops)
			}
			if operation.State == "reserved" || operation.State == "running" {
				t.Fatalf("repair not settled: %+v", operation)
			}
			body, err := os.ReadFile(path)
			if err != nil || string(body) != "original issue notes\n" {
				t.Fatalf("unexpected file effect: %v", err)
			}
		})
	}
}
