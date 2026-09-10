package agent_test

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
)

func captureFixture(stage string) (agent.ContributionScope, agent.Contribution) {
	scope, c := contributionFixture(stage)
	story := scope.Stories["US-001"]
	story.SnapshotID = ""
	story.ImplementationDigest = ""
	story.SnapshotRetained = false
	story.Checks = map[string]agent.RetainedContributionCheck{}
	scope.Stories["US-001"] = story
	files := []agent.StorySnapshotFile{{Path: "internal/a.go", Content: []byte("package a\n"), SHA256: agent.StoryDigest([]byte("package a\n"))}, {Path: "internal/deleted.go", Content: []byte{}, SHA256: agent.StoryDigest(nil), Deleted: true}}
	digest := agent.SnapshotDigest(files)
	c.Capture = &agent.ContributionCapture{ImplementationDigest: digest, Files: []agent.ContributionCaptureFile{}, Checks: []agent.ContributionCaptureCheck{{ID: "check-1", Name: "go test ./internal/a", Passed: true, Evidence: "ok internal/a", ImplementationDigest: digest}}}
	for _, f := range files {
		c.Capture.Files = append(c.Capture.Files, agent.ContributionCaptureFile{Path: f.Path, Content: base64.StdEncoding.EncodeToString(f.Content), SHA256: f.SHA256, Deleted: f.Deleted})
	}
	return scope, c
}

func TestContributionCaptureEngineeringAndLegacyStages(t *testing.T) {
	t.Parallel()
	for _, stage := range []string{"engineering", "legacy"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			scope, c := captureFixture(stage)
			requireContributionValid(t, contributionJSON(t, c), scope)
		})
	}
	for _, tc := range []struct{ stage, outcome string }{{"engineering", "blocked"}, {"engineering", "invalidated"}, {"legacy", "blocked"}, {"legacy", "invalidated"}, {"validate", "assessed"}, {"plan", "discussion"}} {
		t.Run(tc.stage+"/"+tc.outcome, func(t *testing.T) {
			t.Parallel()
			scope, c := contributionFixture(tc.stage)
			_, captured := captureFixture("engineering")
			c.Capture = captured.Capture
			c.Outcome = tc.outcome
			if tc.outcome == "blocked" || tc.outcome == "invalidated" {
				c.Engineering = nil
				c.Assessment = nil
				c.Reason = "Missing check"
			}
			requireContributionInvalid(t, contributionJSON(t, c), scope, "$.capture")
		})
	}
}

func TestContributionCapturePreservesExactSnapshotAndDeletion(t *testing.T) {
	t.Parallel()
	_, c := captureFixture("engineering")
	evidence, err := c.Capture.SnapshotEvidence()
	if err != nil {
		t.Fatal(err)
	}
	if evidence.ImplementationDigest != c.Capture.ImplementationDigest || len(evidence.Files) != 2 || len(evidence.Tests) != 1 || len(evidence.Acceptance) != 0 || len(evidence.Review) != 0 {
		t.Fatalf("conversion changed evidence dimensions: %+v", evidence)
	}
	if string(evidence.Files[0].Content) != "package a\n" || !evidence.Files[1].Deleted || len(evidence.Files[1].Content) != 0 || evidence.Files[1].SHA256 != agent.StoryDigest(nil) {
		t.Fatal("conversion lost complete bytes/deletion")
	}
	if evidence.Tests[0].Name != c.Capture.Checks[0].Name || evidence.Tests[0].Evidence != c.Capture.Checks[0].Evidence || evidence.Tests[0].ImplementationDigest != c.Capture.ImplementationDigest {
		t.Fatal("conversion lost test attribution")
	}
}

func TestContributionCaptureRejectsCorruptSnapshot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*agent.ContributionCapture)
	}{
		{"invalid base64", func(c *agent.ContributionCapture) { c.Files[0].Content = "%%%" }},
		{"wrong content digest", func(c *agent.ContributionCapture) { c.Files[0].SHA256 = strings.Repeat("a", 64) }},
		{"wrong manifest digest", func(c *agent.ContributionCapture) { c.ImplementationDigest = strings.Repeat("a", 64) }},
		{"unsorted paths", func(c *agent.ContributionCapture) { c.Files[0], c.Files[1] = c.Files[1], c.Files[0] }},
		{"duplicate path", func(c *agent.ContributionCapture) { c.Files[1] = c.Files[0] }},
		{"traversal", func(c *agent.ContributionCapture) { c.Files[0].Path = "../private" }},
		{"transport", func(c *agent.ContributionCapture) { c.Files[0].Path = ".litefactory-story-events/1/000001.json" }},
		{"deletion with bytes", func(c *agent.ContributionCapture) { c.Files[0].Deleted = true }},
		{"empty snapshot", func(c *agent.ContributionCapture) { c.Files = []agent.ContributionCaptureFile{} }},
		{"too many files", func(c *agent.ContributionCapture) {
			for len(c.Files) <= 256 {
				c.Files = append(c.Files, c.Files[0])
			}
		}},
		{"failed executed check", func(c *agent.ContributionCapture) { c.Checks[0].Passed = false }},
		{"check digest mismatch", func(c *agent.ContributionCapture) { c.Checks[0].ImplementationDigest = strings.Repeat("c", 64) }},
		{"no check output", func(c *agent.ContributionCapture) { c.Checks[0].Evidence = "" }},
		{"no checks", func(c *agent.ContributionCapture) { c.Checks = []agent.ContributionCaptureCheck{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scope, c := captureFixture("engineering")
			tc.mutate(c.Capture)
			requireContributionInvalid(t, contributionJSON(t, c), scope, "$.capture")
		})
	}
}

func TestContributionCaptureExactCheckReferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path string
		change     func(*agent.ContributionScope, *agent.Contribution)
	}{
		{"duplicate check identity", "$.capture.checks[1].id", func(_ *agent.ContributionScope, c *agent.Contribution) {
			check := c.Capture.Checks[0]
			check.Name = "second test"
			c.Capture.Checks = append(c.Capture.Checks, check)
		}},
		{"blank check identity", "$.capture.checks[0].id", func(_ *agent.ContributionScope, c *agent.Contribution) { c.Capture.Checks[0].ID = "" }},
		{"unknown requested check", "$.engineering.check_ids[0]", func(_ *agent.ContributionScope, c *agent.Contribution) { c.Engineering.CheckIDs = []string{"unknown"} }},
		{"retained snapshot conflict", "$.capture", func(s *agent.ContributionScope, _ *agent.Contribution) {
			story := s.Stories["US-001"]
			story.SnapshotRetained = true
			story.SnapshotID = "another"
			story.ImplementationDigest = strings.Repeat("d", 64)
			s.Stories["US-001"] = story
		}},
		{"retained check conflict", "$.capture.checks[0].id", func(s *agent.ContributionScope, _ *agent.Contribution) {
			story := s.Stories["US-001"]
			story.Checks["check-1"] = agent.RetainedContributionCheck{Story: "US-foreign"}
			s.Stories["US-001"] = story
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			scope, c := captureFixture("engineering")
			tc.change(&scope, &c)
			requireContributionInvalid(t, contributionJSON(t, c), scope, tc.path)
		})
	}
}

func TestContributionCapturePreflightDoesNotRetainClaims(t *testing.T) {
	t.Parallel()
	scope, c := captureFixture("engineering")
	before, err := json.Marshal(scope.Stories)
	if err != nil {
		t.Fatal(err)
	}
	data := contributionJSON(t, c)
	preflight := agent.PreflightContribution(data, scope)
	if !preflight.Valid {
		t.Fatalf("valid capture rejected: %+v", preflight)
	}
	_, submitted := agent.ParseContribution(data, scope)
	if !reflect.DeepEqual(preflight, submitted) {
		t.Fatal("capture preflight diverges from submission validation")
	}
	after, err := json.Marshal(scope.Stories)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("preflight mutated retained scope/check inventory")
	}
	c.Capture = nil
	requireContributionInvalid(t, contributionJSON(t, c), scope, "$.engineering.snapshot_id")
}

func TestContributionCaptureStrictTypesAndPayloadBudget(t *testing.T) {
	t.Parallel()
	scope, c := captureFixture("engineering")
	base := string(contributionJSON(t, c))
	for _, tc := range []struct{ name, data, path string }{
		{"missing deletion flag", strings.Replace(base, `,"deleted":false`, "", 1), "$.capture.files[0].deleted"},
		{"wrong pass type", strings.Replace(base, `"passed":true`, `"passed":"true"`, 1), "$.capture.checks[0].passed"},
		{"wrong base64 type", strings.Replace(base, `"content":"cGFja2FnZSBhCg=="`, `"content":[1,2]`, 1), "$.capture.files[0].content"},
	} {
		t.Run(tc.name, func(t *testing.T) { requireContributionInvalid(t, []byte(tc.data), scope, tc.path) })
	}
	c.Capture.Files[0].Content = base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", agent.MaxContributionBytes)))
	requireContributionInvalid(t, contributionJSON(t, c), scope, "$")
}
