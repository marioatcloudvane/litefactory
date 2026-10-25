package doctor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"encoding/base64"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestRetainedEvidenceRechecksAuthorityAndRefusesSymlink(t *testing.T) {
	s := doctorIOService(t)
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.BeginDoctorAttempt(t.Context(), ref.String(), store.DoctorAttempt{ID: "retained-reader", SourceID: "cli:retained-reader", OwnerText: "Continue", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(s.Paths.DoctorScratchDir(), 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	link := filepath.Join(s.Paths.DoctorScratchDir(), "linked")
	if err = os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareRetainedEvidence(t.Context(), ref, "retained-reader", link); err == nil {
		t.Fatal("symlink invocation root admitted")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("symlink target received evidence")
	}
	dir, err := os.MkdirTemp(s.Paths.DoctorScratchDir(), "valid-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareRetainedEvidence(t.Context(), ref, "retained-reader", dir); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.StopDoctorConversation(t.Context(), ref.String()); err != nil {
		t.Fatal(err)
	}
	next, err := os.MkdirTemp(s.Paths.DoctorScratchDir(), "revoked-")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareRetainedEvidence(t.Context(), ref, "retained-reader", next); !errors.Is(err, store.ErrDoctorFenced) {
		t.Fatalf("revoked retained read = %v", err)
	}
	entries, err = os.ReadDir(next)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatal("revoked reader received evidence")
	}
}

func TestRetainedEvidenceWithholdsChangedCapturedBytes(t *testing.T) {
	s := doctorIOService(t)
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.BeginDoctorAttempt(t.Context(), ref.String(), store.DoctorAttempt{ID: "changed-reader", SourceID: "cli:changed-reader", OwnerText: "Continue", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	first := filepath.Join(s.Paths.DoctorScratchDir(), "first")
	if err = os.MkdirAll(first, 0700); err != nil {
		t.Fatal(err)
	}
	before, err := s.PrepareRetainedEvidence(t.Context(), ref, "changed-reader", first)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.PrepareDoctorAttempt(t.Context(), store.DoctorAttempt{ID: "changed-reader", Evidence: "new captured bytes must not replace old source"}); err != nil {
		t.Fatal(err)
	}
	second := filepath.Join(s.Paths.DoctorScratchDir(), "second")
	if err = os.MkdirAll(second, 0700); err != nil {
		t.Fatal(err)
	}
	after, err := s.PrepareRetainedEvidence(t.Context(), ref, "changed-reader", second)
	if err != nil {
		t.Fatal(err)
	}
	beforeJSON, _ := json.Marshal(before.Manifest)
	afterJSON, _ := json.Marshal(after.Manifest)
	if string(beforeJSON) != string(afterJSON) {
		t.Fatal("recovery replaced old manifest")
	}
	index, err := os.ReadFile(after.Index)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "newer content was withheld") || strings.Contains(string(index), "new captured bytes must not replace old source") {
		t.Fatal("changed bytes were not explicitly withheld")
	}
}

func TestRetainedGuidanceRedactsDecodedResourceClosure(t *testing.T) {
	s := doctorIOService(t)
	ref, err := Target("acme/api", 406)
	if err != nil {
		t.Fatal(err)
	}
	const canary = "doctor-secret-canary-001"
	baseline := testDoctorCapture(t, "doctor-diagnosis")
	for _, file := range baseline.Snapshot().Files {
		destination := filepath.Join(s.Paths.InternalInstructionsDir(), filepath.FromSlash(file.Path))
		if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(destination, file.Content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(s.Paths.InternalInstructionsDir(), "agents", "doctor-diagnosis.md")
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("Captured guidance includes "+canary+" in decoded content.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pkg, err := instructionpackage.New(t.Context(), s.Paths)
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), "doctor-diagnosis")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(capture.Instructions()), canary) {
		t.Fatal("secret canary was not captured; test cannot check decoded redaction")
	}
	raw, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.DB.BeginDoctorAttempt(t.Context(), ref.String(), store.DoctorAttempt{ID: "secret-reader", SourceID: "cli:secret-reader", OwnerText: "Continue", PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.RetainDoctorInstructionCapture(t.Context(), "secret-reader", raw); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Paths.DoctorScratchDir(), "secret-view")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	handoff, err := s.PrepareRetainedEvidence(t.Context(), ref, "secret-reader", dir)
	if err != nil {
		t.Fatal(err)
	}
	var index struct {
		Views []struct {
			Entry  struct{ Kind string }
			File   string
			Format string
		}
	}
	indexBytes, err := os.ReadFile(handoff.Index)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(indexBytes, &index); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, view := range index.Views {
		if view.Entry.Kind != "doctor_guidance" {
			continue
		}
		found = true
		if view.Format != "guidance-text-v1" || view.File == "" {
			t.Fatal("guidance lacks a decoded derived view")
		}
		content, err := os.ReadFile(filepath.Join(dir, view.File))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(content), canary) || strings.Contains(string(content), base64.StdEncoding.EncodeToString(capture.Instructions())) || !strings.Contains(string(content), "[redacted]") {
			t.Fatal("encoded captured guidance bypassed redaction")
		}
	}
	if !found {
		t.Fatal("guidance source missing")
	}
}
