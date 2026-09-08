package agent_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func verifiedContractEvent() agent.StoryEvent {
	content := []byte("package example\n")
	files := []agent.StorySnapshotFile{{Path: "internal/example/example.go", Content: content, SHA256: agent.StoryDigest(content)}}
	digest := agent.SnapshotDigest(files)
	check := agent.StoryCheck{Name: "scoped check", Passed: true, Evidence: "verified against retained snapshot", ImplementationDigest: digest}
	return agent.StoryEvent{Version: 1, Parent: "github:acme/acme-api#141", RunID: 19, Generation: 2, Story: "US-5", IssueID: 9005, Revision: agent.StoryDigest([]byte("frozen specification")), Attempt: 1, Sequence: 1, Kind: "verified", Evidence: &agent.StoryEvidence{ImplementationDigest: digest, Files: files, Acceptance: []agent.StoryCheck{check}, Review: []agent.StoryCheck{check}, Tests: []agent.StoryCheck{check}}}
}

func encodeContractEvent(t *testing.T, event agent.StoryEvent) []byte {
	t.Helper()
	data, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestStoryEventVerificationRequiresBoundEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*agent.StoryEvent)
	}{
		{"missing evidence", func(e *agent.StoryEvent) { e.Evidence = nil }},
		{"missing acceptance", func(e *agent.StoryEvent) { e.Evidence.Acceptance = nil }},
		{"missing review", func(e *agent.StoryEvent) { e.Evidence.Review = nil }},
		{"missing tests", func(e *agent.StoryEvent) { e.Evidence.Tests = nil }},
		{"failed check", func(e *agent.StoryEvent) { e.Evidence.Tests[0].Passed = false }},
		{"empty evidence text", func(e *agent.StoryEvent) { e.Evidence.Review[0].Evidence = " \n" }},
		{"duplicate check", func(e *agent.StoryEvent) { e.Evidence.Tests = append(e.Evidence.Tests, e.Evidence.Tests[0]) }},
		{"check from another snapshot", func(e *agent.StoryEvent) {
			e.Evidence.Acceptance[0].ImplementationDigest = agent.StoryDigest([]byte("other"))
		}},
		{"changed content", func(e *agent.StoryEvent) { e.Evidence.Files[0].Content = []byte("changed") }},
		{"changed manifest", func(e *agent.StoryEvent) { e.Evidence.Files[0].Path = "internal/other.go" }},
		{"no files", func(e *agent.StoryEvent) {
			e.Evidence.Files = nil
			e.Evidence.ImplementationDigest = agent.SnapshotDigest(nil)
		}},
		{"duplicate files", func(e *agent.StoryEvent) {
			e.Evidence.Files = append(e.Evidence.Files, e.Evidence.Files[0])
			bindContractChecks(e)
		}},
		{"nonempty deletion", func(e *agent.StoryEvent) { e.Evidence.Files[0].Deleted = true; bindContractChecks(e) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := verifiedContractEvent()
			tc.mutate(&e)
			if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err == nil {
				t.Fatal("unverified evidence accepted")
			}
		})
	}
	valid := verifiedContractEvent()
	parsed, err := agent.ParseStoryEvent(encodeContractEvent(t, valid))
	if err != nil {
		t.Fatal(err)
	}
	if string(parsed.Evidence.Files[0].Content) != string(valid.Evidence.Files[0].Content) {
		t.Fatal("embedded snapshot bytes lost")
	}
}

func bindContractChecks(e *agent.StoryEvent) {
	e.Evidence.ImplementationDigest = agent.SnapshotDigest(e.Evidence.Files)
	for _, group := range [][]agent.StoryCheck{e.Evidence.Acceptance, e.Evidence.Review, e.Evidence.Tests} {
		for i := range group {
			group[i].ImplementationDigest = e.Evidence.ImplementationDigest
		}
	}
}

func TestStoryEventRejectsMalformedProtocol(t *testing.T) {
	t.Parallel()
	valid := string(encodeContractEvent(t, verifiedContractEvent()))
	cases := []struct{ name, data string }{
		{"raw completion", "DONE"}, {"empty", ""}, {"null", "null"},
		{"duplicate identity", strings.Replace(valid, `"issue_id":9005`, `"issue_id":7,"issue_id":9005`, 1)},
		{"duplicate check verdict", strings.Replace(valid, `"passed":true`, `"passed":false,"passed":true`, 1)},
		{"trailing object", valid + `{}`}, {"unknown field", strings.Replace(valid, `"version":1`, `"version":1,"surprise":true`, 1)},
		{"unknown nested field", strings.Replace(valid, `"passed":true`, `"passed":true,"surprise":true`, 1)},
		{"oversized", strings.Repeat(" ", agent.MaxStoryEventBytes+1)},
		{"unsupported version", strings.Replace(valid, `"version":1`, `"version":2`, 1)},
		{"zero run", strings.Replace(valid, `"run_id":19`, `"run_id":0`, 1)},
		{"zero generation", strings.Replace(valid, `"generation":2`, `"generation":0`, 1)},
		{"zero identity", strings.Replace(valid, `"issue_id":9005`, `"issue_id":0`, 1)},
		{"empty story", strings.Replace(valid, `"story":"US-5"`, `"story":""`, 1)},
		{"zero attempt", strings.Replace(valid, `"attempt":1`, `"attempt":0`, 1)},
		{"zero sequence", strings.Replace(valid, `"sequence":1`, `"sequence":0`, 1)},
		{"out of bounds sequence", strings.Replace(valid, `"sequence":1`, `"sequence":4097`, 1)},
		{"unknown kind", strings.Replace(valid, `"kind":"verified"`, `"kind":"done"`, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := agent.ParseStoryEvent([]byte(tc.data)); err == nil {
				t.Fatal("malformed event accepted")
			}
		})
	}
}

func TestStoryEventNonVerificationHasExplicitReason(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"blocked", "invalidated"} {
		t.Run(kind, func(t *testing.T) {
			e := verifiedContractEvent()
			e.Kind = kind
			e.Reason = "later scoped test failed"
			e.Evidence = nil
			if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err != nil {
				t.Fatal(err)
			}
			e.Reason = " "
			if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err == nil {
				t.Fatal("missing explanation accepted")
			}
			e.Reason = "later scoped test failed"
			e.Evidence = verifiedContractEvent().Evidence
			if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err == nil {
				t.Fatal("verification evidence on invalidation accepted")
			}
		})
	}
}

func TestStoryEvidenceRejectsUnsafePaths(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"", "../escape", "/absolute", "a/../escape", "a\\b", ".git/config", "nested/.git/config", agent.StoryEventsDirectory + "/1.json", ".litefactory-plan-proposal.json", "a\nb"} {
		t.Run(path, func(t *testing.T) {
			e := verifiedContractEvent()
			e.Evidence.Files[0].Path = path
			bindContractChecks(&e)
			if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err == nil {
				t.Fatal("unsafe snapshot path accepted")
			}
		})
	}
}

func TestStoryDigestAndSnapshotBinding(t *testing.T) {
	t.Parallel()
	if got := agent.StoryDigest([]byte("abc")); got != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("SHA256 mismatch: %s", got)
	}
	original := verifiedContractEvent()
	digest := original.Evidence.ImplementationDigest
	for _, mutate := range []func(*agent.StorySnapshotFile){func(f *agent.StorySnapshotFile) { f.Path = "other.go" }, func(f *agent.StorySnapshotFile) { f.SHA256 = agent.StoryDigest([]byte("different")) }, func(f *agent.StorySnapshotFile) { f.Deleted = true }} {
		files := append([]agent.StorySnapshotFile(nil), original.Evidence.Files...)
		mutate(&files[0])
		if agent.SnapshotDigest(files) == digest {
			t.Fatal("snapshot digest did not bind manifest metadata")
		}
	}
	e := verifiedContractEvent()
	e.Evidence.Files[0].Content = nil
	e.Evidence.Files[0].SHA256 = agent.StoryDigest(nil)
	e.Evidence.Files[0].Deleted = true
	bindContractChecks(&e)
	if _, err := agent.ParseStoryEvent(encodeContractEvent(t, e)); err != nil {
		t.Fatalf("valid deletion: %v", err)
	}
}
