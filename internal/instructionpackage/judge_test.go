package instructionpackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

func TestJudgeClosureCaptureAndFallback(t *testing.T) {
	owner := t.TempDir()
	pkg, err := LoadShipped(t.Context(), owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner, CataloguePath), pkg.files[CataloguePath], 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(owner, JudgeRolePath)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owner, JudgeRolePath), []byte("owner role\n"), 0600); err != nil {
		t.Fatal(err)
	}
	capture, diagnostics, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil || len(diagnostics) != 0 {
		t.Fatalf("capture: %v %v", err, diagnostics)
	}
	originalDigest := capture.Snapshot().EffectiveDigest
	got := capture.Resource(JudgeRolePath)
	if !bytes.HasPrefix(got, []byte("owner ")) {
		t.Fatal("owner role missing")
	}
	got[0] = 'X'
	if capture.Resource(JudgeRolePath)[0] == 'X' {
		t.Fatal("resource accessor exposes capture")
	}
	if err := os.WriteFile(filepath.Join(owner, JudgeRolePath), []byte(" \n"), 0600); err != nil {
		t.Fatal(err)
	}
	fallback, diagnostics, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil || len(diagnostics) != 1 || diagnostics[0].File != JudgeRolePath {
		t.Fatalf("fallback: %v %v", err, diagnostics)
	}
	if !bytes.Equal(fallback.Instructions(), pkg.files[JudgeRolePath]) || fallback.Snapshot().EffectiveDigest == originalDigest {
		t.Fatal("role fallback did not select shipped identity")
	}
	encoded, err := fallback.Encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Instructions(), fallback.Instructions()) {
		t.Fatal("restore changed role")
	}
}

// A capture retained while the challenger still carried its evidence resources
// (judge requests, pinned workflow definitions) must keep restoring.
func TestJudgeLegacyClosureStillRestores(t *testing.T) {
	pkg, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	data := capture.Snapshot()
	files := map[string][]byte{}
	for _, file := range data.Files {
		files[file.Path] = file.Content
	}
	for _, name := range legacyJudgeResourcePaths {
		content := []byte("historical " + name + "\n")
		data.Files = append(data.Files, CapturedFile{Path: name, Source: SourceShipped, Content: content, Hash: hashBytes(content)})
		files[name] = content
	}
	slices.SortFunc(data.Files, func(a, b CapturedFile) int { return strings.Compare(a.Path, b.Path) })
	data.EffectiveDigest = contentDigest(files)
	data.Integrity = captureIntegrity(data)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(encoded)
	if err != nil {
		t.Fatalf("historical judge capture refused: %v", err)
	}
	if !bytes.Equal(restored.Instructions(), capture.Instructions()) {
		t.Fatal("historical capture changed role")
	}
	data.Files = data.Files[:len(data.Files)-1]
	delete(files, legacyJudgeResourcePaths[len(legacyJudgeResourcePaths)-1])
	data.EffectiveDigest = contentDigest(files)
	data.Integrity = captureIntegrity(data)
	encoded, err = json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(encoded); err == nil {
		t.Fatal("partial historical closure accepted")
	}
}

func TestJudgeClosureRefusesIncompleteRetainedCapture(t *testing.T) {
	pkg, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	data := capture.Snapshot()
	for i, file := range data.Files {
		if file.Path == JudgeRolePath {
			data.Files = append(data.Files[:i], data.Files[i+1:]...)
			break
		}
	}
	data.Integrity = captureIntegrity(data)
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(encoded); err == nil {
		t.Fatal("incomplete judge closure accepted")
	}
}

func TestJudgeShippedFidelity(t *testing.T) {
	pkg, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytes.TrimSpace(capture.Instructions())) == 0 {
		t.Fatal("shipped judge role is empty")
	}
}

func TestJudgeResourcesChangeIdentityAndValidate(t *testing.T) {
	pkg, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	fixture := func() fstest.MapFS {
		f := fstest.MapFS{}
		for name, raw := range pkg.files {
			f[name] = &fstest.MapFile{Data: bytes.Clone(raw)}
		}
		return f
	}
	original, _, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{JudgeRolePath} {
		t.Run(name, func(t *testing.T) {
			changed := fixture()
			changed[name].Data = append(changed[name].Data, '\n')
			updated, err := Load(t.Context(), changed, "")
			if err != nil {
				t.Fatal(err)
			}
			capture, _, err := updated.Capture(t.Context(), JudgeRole)
			if err != nil {
				t.Fatal(err)
			}
			if capture.Snapshot().EffectiveDigest == original.Snapshot().EffectiveDigest {
				t.Fatal("resource edit did not change closure identity")
			}
			for _, mode := range []string{"missing", "blank", "bounds"} {
				invalid := fixture()
				switch mode {
				case "missing":
					delete(invalid, name)
				case "blank":
					invalid[name].Data = []byte(" ")
				case "bounds":
					invalid[name].Data = bytes.Repeat([]byte("x"), MaxFileBytes+1)
				}
				if _, err := Load(t.Context(), invalid, ""); err == nil {
					t.Fatalf("invalid shipped %s resource accepted", mode)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := pkg.Capture(ctx, JudgeRole); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled capture = %v", err)
	}
}

func TestJudgeRetainedResourceBounds(t *testing.T) {
	pkg, err := LoadShipped(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), JudgeRole)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{JudgeRolePath} {
		t.Run(name, func(t *testing.T) {
			data := capture.Snapshot()
			files := map[string][]byte{}
			for i := range data.Files {
				file := &data.Files[i]
				if file.Path == name {
					file.Content = bytes.Repeat([]byte("x"), MaxFileBytes+1)
					file.Hash = hashBytes(file.Content)
				}
				files[file.Path] = file.Content
			}
			data.EffectiveDigest = contentDigest(files)
			data.Integrity = captureIntegrity(data)
			encoded, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(encoded); err == nil {
				t.Fatal("oversized retained resource accepted")
			}
		})
	}
}
