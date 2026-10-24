package doctor

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

func doctorIOService(t *testing.T) *Service {
	t.Helper()
	db, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "doctor.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Decode(strings.NewReader("agents:\n  claude: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return &Service{DB: db, Paths: config.NewPaths(home), Config: cfg, Redactor: redact.New(redact.Options{Environ: []string{"API_SECRET=doctor-secret-canary-001"}})}
}

func TestDoctorRefusedProviderRetainsRedactedInputAndFreshEvidence(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	first, err := s.Diagnose(ctx, DiagnoseRequest{Target: ref, Instructions: "Explain doctor-secret-canary-001", SourceID: "first"})
	if !errors.Is(err, ErrIsolationUnavailable) {
		t.Fatalf("unverified provider should refuse: %v", err)
	}
	if len(first.Attempts) != 1 {
		t.Fatalf("missing retained attempt: %+v", first)
	}
	a := first.Attempts[0]
	if strings.Contains(a.OwnerText, "doctor-secret-canary-001") || !strings.Contains(a.OwnerText, "[redacted]") {
		t.Fatalf("owner text not redacted: %q", a.OwnerText)
	}
	if a.State != "failed" || a.ConfiguredModel == "" || a.ObservedModel != "" || a.Evidence == "" {
		t.Fatalf("wrong failure history: %+v", a)
	}
	if !strings.Contains(a.Evidence, "No workflow session exists") {
		t.Fatalf("missing source gap: %s", a.Evidence)
	}
	second, err := s.Diagnose(ctx, DiagnoseRequest{Target: ref, Instructions: "Continue", SourceID: "second"})
	if !errors.Is(err, ErrIsolationUnavailable) {
		t.Fatal(err)
	}
	if second.Conversation.ID != first.Conversation.ID || len(second.Attempts) != 2 || second.Attempts[1].Evidence == a.Evidence {
		t.Fatalf("resume lost identity/history/fresh capture: %+v", second)
	}
	path := s.DB.Path()
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	}()
	s.DB = reopened
	saved, err := s.ReadConversation(ctx, ref)
	if err != nil || len(saved.Attempts) != 2 || saved.ApplyAvailable {
		t.Fatalf("reopen: %+v %v", saved, err)
	}
	if err := s.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	third, err := s.Diagnose(ctx, DiagnoseRequest{Target: ref, Instructions: "New conversation", SourceID: "third"})
	if !errors.Is(err, ErrIsolationUnavailable) {
		t.Fatal(err)
	}
	if third.Conversation.Generation != first.Conversation.Generation+1 || third.Conversation.ID == first.Conversation.ID {
		t.Fatalf("stop did not fence generation: %+v", third)
	}
}

func TestDoctorCompetingWriterIsBusyButStopRemainsAvailable(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := s.lock(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := unlock(); err != nil {
			t.Error(err)
		}
	}()
	_, err = s.Diagnose(ctx, DiagnoseRequest{Target: ref, Instructions: "Competing writer", SourceID: "competitor"})
	if !errors.Is(err, store.ErrDoctorBusy) {
		t.Fatalf("competing diagnosis = %v", err)
	}
	if _, err = s.ReadConversation(ctx, ref); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("busy writer created history: %v", err)
	}
	if err := s.Stop(ctx, ref); err != nil {
		t.Fatalf("stop waited on diagnosis lock: %v", err)
	}
	sibling, err := Target("acme/api", 407)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Diagnose(ctx, DiagnoseRequest{Target: sibling, Instructions: "Other issue", SourceID: "sibling"}); !errors.Is(err, ErrIsolationUnavailable) {
		t.Fatalf("other issue blocked by selected lock: %v", err)
	}
}

func TestDoctorResultServiceAdoptionReplayAndStop(t *testing.T) {
	s := doctorIOService(t)
	ctx := t.Context()
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "result-owner", SourceID: "result-source", OwnerText: "Explain", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	e := store.DoctorEvidence{TaskRef: ref.String(), Records: []store.DoctorEvidenceRecord{{Kind: "run", ID: "42", State: "running"}}, Gaps: []string{"Provider observation unavailable"}}
	results, err := BindResults(ctx, s.DB, "result-owner", e, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input := ResultInput{Diagnosis: Diagnosis{Findings: []Finding{{Explanation: "A stranded run mentions doctor-secret-canary-001", Evidence: []string{"run:42"}, Uncertainty: "Process state unobserved"}}, CoverageGaps: e.Gaps, Proposal: []RepairStep{{Number: 1, Target: "run:42", Precondition: "Still running", Change: "Record interrupted", Reason: "Process absent", Reversibility: "Restore saved state", Rollback: "Restore backed-up row", Action: &store.DoctorRepairAction{Kind: "stop_run", Before: "running", After: "failed"}}}}}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	preflight, err := results.Preflight(ctx, data)
	if err != nil || !preflight.Valid {
		t.Fatalf("preflight: %+v %v", preflight, err)
	}
	if saved, err := s.DB.DoctorCurrentResult(ctx, c.ID); err != nil || len(saved) != 0 {
		t.Fatalf("preflight adopted data: %s %v", saved, err)
	}
	receipt, err := results.Submit(ctx, "durable", data)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := results.Submit(ctx, "durable", data)
	if err != nil || replay.ID != receipt.ID {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	if _, err := results.Submit(ctx, "durable", append(append([]byte{}, data...), ' ')); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatalf("changed original bytes accepted: %v", err)
	}
	if bytes.Contains(receipt.Payload, []byte("doctor-secret-canary-001")) {
		t.Fatal("receipt retained raw secret")
	}
	if err := results.Adopt(ctx, "durable"); err != nil {
		t.Fatal(err)
	}
	saved, err := s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || len(saved) == 0 || bytes.Contains(saved, []byte("doctor-secret-canary-001")) {
		t.Fatalf("adopted result: %s %v", saved, err)
	}
	next, err := s.DB.BeginDoctorAttempt(ctx, ref.String(), store.DoctorAttempt{ID: "later-owner", SourceID: "later-source", OwnerText: "Continue", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	if next.ID != c.ID {
		t.Fatal("resume lost conversation")
	}
	after, err := s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || !bytes.Equal(after, saved) {
		t.Fatal("resume lost prior result", err)
	}
	later, err := BindResults(ctx, s.DB, "later-owner", e, s.Redactor)
	if err != nil {
		t.Fatal(err)
	}
	input.ExpectedRevision = 1
	data, err = json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := later.Submit(ctx, "before-stop", data); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(ctx, ref); err != nil {
		t.Fatal(err)
	}
	if err := later.Adopt(ctx, "before-stop"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("stopped result adopted: %v", err)
	}
	if _, err := later.Receipt(ctx, "before-stop"); err != nil {
		t.Fatalf("stop erased receipt: %v", err)
	}
	after, err = s.DB.DoctorCurrentResult(ctx, c.ID)
	if err != nil || !bytes.Equal(after, saved) {
		t.Fatal("stopped late result overwrote prior diagnosis", err)
	}
}
