package instructionpackage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
)

func captureFixture(t *testing.T, owner string) *RoleCapture {
	t.Helper()
	pkg, err := Load(t.Context(), fstest.MapFS{
		MetadataPath:             {Data: []byte("schema: 1\nversion: v1\n")},
		CataloguePath:            {Data: []byte("- fixture-role\n")},
		"agents/fixture-role.md": {Data: []byte("Exact retained instructions.\n")},
	}, owner)
	if err != nil {
		t.Fatal(err)
	}
	capture, _, err := pkg.Capture(t.Context(), "fixture-role")
	if err != nil {
		t.Fatal(err)
	}
	return capture
}

func TestCaptureDefensiveCopies(t *testing.T) {
	capture := captureFixture(t, t.TempDir()) // Present but empty owner produces a diagnostic.
	before := capture.Snapshot()
	if len(before.Diagnostics) == 0 {
		t.Fatal("fixture did not exercise diagnostics")
	}
	copyData := capture.Snapshot()
	copyData.Files[0].Content[0] ^= 1
	copyData.Files[0].Source = SourceOwner
	copyData.Diagnostics[0].Category = "changed"
	instructions := capture.Instructions()
	instructions[0] ^= 1
	encoded, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := Restore(encoded)
	if err != nil {
		t.Fatal(err)
	}
	for i := range encoded {
		encoded[i] = 0
	}
	if !reflect.DeepEqual(capture.Snapshot(), before) || !reflect.DeepEqual(restored.Snapshot(), before) {
		t.Fatal("public data aliases retained capture")
	}
}

func TestRestoreRejectsCorruption(t *testing.T) {
	capture := captureFixture(t, "")
	cases := map[string]func(*CaptureData){
		"schema":           func(d *CaptureData) { d.Schema++ },
		"role":             func(d *CaptureData) { d.Role = "other-role" },
		"shipped version":  func(d *CaptureData) { d.ShippedVersion = "v2" },
		"shipped digest":   func(d *CaptureData) { d.ShippedDigest = strings.Repeat("a", 64) },
		"effective digest": func(d *CaptureData) { d.EffectiveDigest = strings.Repeat("a", 64) },
		"file bytes":       func(d *CaptureData) { d.Files[0].Content[0] ^= 1 },
		"file path":        func(d *CaptureData) { d.Files[0].Path = "agents/other-role.md" },
		"file hash":        func(d *CaptureData) { d.Files[0].Hash = strings.Repeat("a", 64) },
		"file source":      func(d *CaptureData) { d.Files[0].Source = SourceOwner },
		"diagnostics": func(d *CaptureData) {
			d.Diagnostics = []Diagnostic{{Role: d.Role, File: CataloguePath, Category: "missing", SelectedSource: SourceShipped}}
		},
		"integrity": func(d *CaptureData) { d.Integrity = strings.Repeat("a", 64) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := capture.Snapshot()
			mutate(&data)
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(raw); err == nil {
				t.Fatal("accepted corrupted capture")
			}
		})
	}
}

func TestRestoreValidatesResealedCaptureSemantics(t *testing.T) {
	capture := captureFixture(t, "")
	cases := map[string]func(*CaptureData){
		"unsupported schema":                 func(d *CaptureData) { d.Schema++ },
		"unsafe role":                        func(d *CaptureData) { d.Role = "../fixture-role" },
		"blank version":                      func(d *CaptureData) { d.ShippedVersion = " " },
		"invalid shipped digest":             func(d *CaptureData) { d.ShippedDigest = "bad" },
		"extra file":                         func(d *CaptureData) { d.Files = append(d.Files, d.Files[0]) },
		"duplicate files":                    func(d *CaptureData) { d.Files[1] = d.Files[0] },
		"unsorted files":                     func(d *CaptureData) { d.Files[0], d.Files[1] = d.Files[1], d.Files[0] },
		"unknown source":                     func(d *CaptureData) { d.Files[0].Source = "remote" },
		"owner role without owner catalogue": func(d *CaptureData) { d.Files[0].Source = SourceOwner },
		"blank role":                         func(d *CaptureData) { d.Files[0].Content = []byte(" \n") },
		"invalid utf8 role":                  func(d *CaptureData) { d.Files[0].Content = []byte{0xff} },
		"oversize role":                      func(d *CaptureData) { d.Files[0].Content = bytes.Repeat([]byte("x"), MaxFileBytes+1) },
		"catalogue missing role":             func(d *CaptureData) { d.Files[1].Content = []byte("[]\n") },
		"invalid catalogue":                  func(d *CaptureData) { d.Files[1].Content = []byte("- ../unsafe\n") },
		"diagnostic role": func(d *CaptureData) {
			d.Diagnostics = []Diagnostic{{Role: "other-role", File: CataloguePath, Category: "missing", SelectedSource: SourceShipped}}
		},
		"diagnostic file": func(d *CaptureData) {
			d.Diagnostics = []Diagnostic{{Role: d.Role, File: "other", Category: "missing", SelectedSource: SourceShipped}}
		},
		"diagnostic category": func(d *CaptureData) {
			d.Diagnostics = []Diagnostic{{Role: d.Role, File: CataloguePath, Category: "other", SelectedSource: SourceShipped}}
		},
		"diagnostic source": func(d *CaptureData) {
			d.Diagnostics = []Diagnostic{{Role: d.Role, File: CataloguePath, Category: "missing", SelectedSource: SourceOwner}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			data := capture.Snapshot()
			mutate(&data)
			files := map[string][]byte{}
			for i := range data.Files {
				data.Files[i].Hash = hashBytes(data.Files[i].Content)
				files[data.Files[i].Path] = data.Files[i].Content
			}
			data.EffectiveDigest = contentDigest(files)
			data.Integrity = captureIntegrity(data)
			raw, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Restore(raw); err == nil {
				t.Fatal("accepted invalid capture despite correctly computed hashes")
			}
		})
	}
}

func TestRestoreRejectsMalformedEncoding(t *testing.T) {
	capture := captureFixture(t, "")
	valid, err := capture.Encode()
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{
		"empty":             nil,
		"truncated":         valid[:len(valid)-1],
		"trailing document": append(bytes.Clone(valid), []byte(" {}")...),
		"unknown field":     append([]byte(`{"unexpected":1,`), valid[1:]...),
		"oversize encoding": bytes.Repeat([]byte(" "), 2*MaxCaptureBytes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Restore(raw); err == nil {
				t.Fatal("accepted malformed capture encoding")
			}
		})
	}
}

func TestEncodeRejectsInvalidCapture(t *testing.T) {
	capture := captureFixture(t, "")
	capture.data.Files[0].Content[0] ^= 1
	if _, err := capture.Encode(); err == nil {
		t.Fatal("encoded a capture whose retained content no longer matches its hashes")
	}
}

func TestCaptureCancellationIsError(t *testing.T) {
	pkg, err := Load(t.Context(), fstest.MapFS{
		MetadataPath:             {Data: []byte("schema: 1\nversion: v1\n")},
		CataloguePath:            {Data: []byte("- fixture-role\n")},
		"agents/fixture-role.md": {Data: []byte("Instructions.\n")},
	}, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	captured, returnedDiagnostics, err := pkg.Capture(t.Context(), "fixture-role")
	if err != nil || len(returnedDiagnostics) == 0 {
		t.Fatalf("missing owner fixture did not produce fallback: %v", err)
	}
	before := captured.Snapshot()
	returnedDiagnostics[0].Category = "changed"
	if !reflect.DeepEqual(captured.Snapshot(), before) {
		t.Fatal("returned diagnostics alias retained capture")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	capture, diagnostics, err := pkg.Capture(ctx, "fixture-role")
	if !errors.Is(err, context.Canceled) || capture != nil || len(diagnostics) != 0 {
		t.Fatalf("cancelled capture selected fallback: capture=%v diagnostics=%v error=%v", capture, diagnostics, err)
	}

	// Cancel after Capture starts, at the owner reader's first context check.
	// This must take the error path even though a shipped fallback is available.
	readingCtx, cancelReading := context.WithCancel(t.Context())
	defer cancelReading()
	duringRead := &ownerReadMutationContext{Context: readingCtx, mutate: cancelReading}
	capture, diagnostics, err = pkg.Capture(duringRead, "fixture-role")
	if !errors.Is(err, context.Canceled) || capture != nil || len(diagnostics) != 0 {
		t.Fatalf("owner-read cancellation selected fallback: capture=%v diagnostics=%v error=%v", capture, diagnostics, err)
	}
}
