package agent

import (
	"io"
	"strings"
	"testing"
)

func TestRegionResponseContract(t *testing.T) {
	req := ConflictResolutionRequest{Branch: "branch", Base: "base", Files: []ConflictFileContent{{Path: "f", Regions: []ConflictRegionContent{{ID: "r", Content: "conflict"}}}}}
	good := `{"version":1,"files":[{"path":"f","regions":[{"id":"r","resolution":""}]}]}`
	if _, err := ParseRegionConflictResolution(good, req); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(good, `"f"`, `"unknown"`, 1), strings.Replace(good, `"r"`, `"unknown"`, 1), strings.Replace(good, `"resolution":""`, `"resolution":null`, 1), strings.Replace(good, `,"resolution":""`, "", 1), strings.Replace(good, `"version":1`, `"version":2`, 1), strings.Replace(good, `"id":"r","resolution":""`, `"id":"r","resolution":"","extra":0`, 1), strings.Replace(good, `{"id":"r","resolution":""}`, `{"id":"r","resolution":""},{"id":"r","resolution":""}`, 1), `{"version":1,"files":[]}`, good + `{}`, strings.Repeat(" ", MaxConflictResponseBytes) + good} {
		if _, err := ParseRegionConflictResolution(bad, req); err == nil {
			t.Fatalf("accepted bad response %.100s", bad)
		}
	}
	var b ConflictOutputBuffer
	content := strings.Repeat("x", MaxConflictResponseBytes+1)
	if _, err := b.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if !b.Exceeded || b.Len() != MaxConflictResponseBytes {
		t.Fatal("output cap failed")
	}
}

func TestConflictOutputCopyAndSharedBudget(t *testing.T) {
	out, stderr := ConflictOutputBuffers()
	if _, err := io.Copy(out, io.LimitReader(zeroConflictReader{}, MaxConflictResponseBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(stderr, strings.NewReader("excess")); err != nil {
		t.Fatal(err)
	}
	if out.Len() != MaxConflictResponseBytes || stderr.Len() != 0 || !stderr.Exceeded {
		t.Fatal("shared stream limit bypassed")
	}
}

type zeroConflictReader struct{}

func (zeroConflictReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestRegionResponseRejectsIncompleteCoverageAtEqualCardinality(t *testing.T) {
	t.Parallel()
	request := ConflictResolutionRequest{Branch: "task", Base: "main", Files: []ConflictFileContent{
		{Path: "first.txt", Regions: []ConflictRegionContent{{ID: "first-1", Content: "first conflict"}, {ID: "first-2", Content: "second conflict"}}},
		{Path: "second.txt", Regions: []ConflictRegionContent{{ID: "second-1", Content: "third conflict"}, {ID: "second-2", Content: "fourth conflict"}}},
	}}
	first := `{"path":"first.txt","regions":[{"id":"first-1","resolution":"one"},{"id":"first-2","resolution":"two"}]}`
	second := `{"path":"second.txt","regions":[{"id":"second-1","resolution":"three"},{"id":"second-2","resolution":"four"}]}`
	valid := `{"version":1,"files":[` + first + `,` + second + `]}`
	if _, err := ParseRegionConflictResolution(valid, request); err != nil {
		t.Fatalf("complete multi-file response refused: %v", err)
	}
	for _, test := range []struct {
		name, response string
	}{
		{"duplicate path with expected file count", `{"version":1,"files":[` + first + `,` + first + `]}`},
		{"duplicate region with expected region count", strings.Replace(valid, `"id":"first-2"`, `"id":"first-1"`, 1)},
		{"missing region in otherwise complete files", strings.Replace(valid, `,{"id":"first-2","resolution":"two"}`, "", 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result, err := ParseRegionConflictResolution(test.response, request)
			if err == nil {
				t.Fatal("incomplete coverage accepted")
			}
			if len(result.Files) != 0 {
				t.Fatalf("partial response salvaged on refusal: %+v", result)
			}
		})
	}
}
