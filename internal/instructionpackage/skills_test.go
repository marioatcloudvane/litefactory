package instructionpackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
)

func closureFixture() fstest.MapFS {
	return fstest.MapFS{
		MetadataPath:                      {Data: []byte("schema: 1\nversion: v1\n")},
		CataloguePath:                     {Data: []byte("[task-advisory]\n")},
		"agents/task-advisory.md":         {Data: []byte("Diagnosis role")},
		"skills/task-advisory/SKILL.md":   {Data: []byte("---\nname: task-advisory\ndescription: Guide\nrequires: [shared/evidence]\nresources: [facts.txt]\n---\nField guide")},
		"skills/task-advisory/facts.txt":  {Data: []byte("Exact facts\n")},
		"skills/shared/evidence/SKILL.md": {Data: []byte("---\nname: evidence\ndescription: Evidence\n---\nEvidence instructions")},
	}
}

func TestRoleClosureCaptureAndWholeFallback(t *testing.T) {
	baseline := closureFixture()
	owner := t.TempDir()
	for name, file := range baseline {
		writeOwnerFixture(t, owner, name, file.Data)
	}
	writeOwnerFixture(t, owner, "agents/task-advisory.md", []byte("Owner diagnosis"))
	pkg, err := Load(t.Context(), baseline, owner)
	if err != nil {
		t.Fatal(err)
	}
	capture, diags, err := pkg.Capture(t.Context(), "task-advisory")
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || string(capture.Instructions()) != "Owner diagnosis" {
		t.Fatalf("owner capture: %v", diags)
	}
	if got := string(capture.SkillInstructions("task-advisory")); !strings.HasPrefix(got, "Field guide") || !strings.Contains(got, "Exact facts\n") || !strings.Contains(got, "Evidence instructions") {
		t.Fatalf("incomplete assembled closure %q", got)
	}
	raw, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	writeOwnerFixture(t, owner, "skills/task-advisory/SKILL.md", []byte("---\nname: task-advisory\ndescription: Guide\nresources: [../escape]\n---\nBad owner guide"))
	restored, err := Restore(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.SkillInstructions("task-advisory"), capture.SkillInstructions("task-advisory")) {
		t.Fatal("restore reread owner files")
	}
	fallback, diags, err := pkg.Capture(t.Context(), "task-advisory")
	if err != nil {
		t.Fatal(err)
	}
	if string(fallback.Instructions()) != "Diagnosis role" || len(diags) != 1 {
		t.Fatalf("incomplete fallback: %v", diags)
	}
	for _, file := range fallback.Snapshot().Files {
		if file.Source != SourceShipped {
			t.Fatal("mixed fallback closure")
		}
	}
	if _, err := fallback.Encode(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := pkg.Capture(ctx, "task-advisory"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestRoleClosureRejectsInvalidShippedAndResealedCaptures(t *testing.T) {
	for _, header := range []string{
		"requires: [task-advisory]",
		"resources: [../escape]",
		"requires: [missing]",
		"requires: [shared/evidence, evidence]",
	} {
		t.Run(header, func(t *testing.T) {
			baseline := closureFixture()
			baseline["skills/task-advisory/SKILL.md"].Data = []byte("---\nname: task-advisory\ndescription: Guide\n" + header + "\n---\nGuide")
			baseline["skills/evidence/SKILL.md"] = &fstest.MapFile{Data: []byte("---\nname: evidence\ndescription: Evidence\n---\nBody")}
			if _, err := Load(t.Context(), baseline, ""); err == nil {
				t.Fatal("invalid shipped closure accepted")
			}
		})
	}
	pkg, err := Load(t.Context(), closureFixture(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), "task-advisory")
	if err != nil {
		t.Fatal(err)
	}
	data := capture.Snapshot()
	for i, file := range data.Files {
		if file.Path == "skills/task-advisory/facts.txt" {
			data.Files = append(data.Files[:i], data.Files[i+1:]...)
			break
		}
	}
	files := map[string][]byte{}
	for _, file := range data.Files {
		files[file.Path] = file.Content
	}
	data.EffectiveDigest = contentDigest(files)
	data.Integrity = captureIntegrity(data)
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(raw); err == nil {
		t.Fatal("resealed capture with missing resource accepted")
	}
}

func TestRoleOwnerClosureMissingAndCycleFallback(t *testing.T) {
	for _, tc := range []struct{ name, file, content string }{
		{"missing resource", "skills/task-advisory/SKILL.md", "---\nname: task-advisory\ndescription: Guide\nresources: [missing.txt]\n---\nOwner guide"},
		{"cycle", "skills/shared/evidence/SKILL.md", "---\nname: evidence\ndescription: Evidence\nrequires: [task-advisory]\n---\nOwner evidence"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			baseline := closureFixture()
			owner := t.TempDir()
			for name, file := range baseline {
				writeOwnerFixture(t, owner, name, file.Data)
			}
			writeOwnerFixture(t, owner, "agents/task-advisory.md", []byte("Owner repair"))
			writeOwnerFixture(t, owner, tc.file, []byte(tc.content))
			pkg, err := Load(t.Context(), baseline, owner)
			if err != nil {
				t.Fatal(err)
			}
			capture, diags, err := pkg.Capture(t.Context(), "task-advisory")
			if err != nil {
				t.Fatal(err)
			}
			if len(diags) != 1 || string(capture.Instructions()) != "Diagnosis role" {
				t.Fatalf("fallback: %v", diags)
			}
			for _, file := range capture.Snapshot().Files {
				if file.Source != SourceShipped {
					t.Fatal("partial owner closure selected")
				}
			}
		})
	}
}

type cancelDuringClosureContext struct {
	context.Context
	checks int
}

func (c *cancelDuringClosureContext) Err() error {
	c.checks++
	if c.checks >= 12 {
		return context.Canceled
	}
	return c.Context.Err()
}
func TestRoleClosureCancellationDuringOwnerRead(t *testing.T) {
	baseline := closureFixture()
	owner := t.TempDir()
	for name, file := range baseline {
		writeOwnerFixture(t, owner, name, file.Data)
	}
	pkg, err := Load(t.Context(), baseline, owner)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &cancelDuringClosureContext{Context: t.Context()}
	capture, diags, err := pkg.Capture(ctx, "task-advisory")
	if !errors.Is(err, context.Canceled) || capture != nil || len(diags) != 0 || ctx.checks < 12 {
		t.Fatalf("cancellation became fallback: capture=%v diagnostics=%v error=%v checks=%d", capture, diags, err, ctx.checks)
	}
}
