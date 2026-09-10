package agent

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// SubmissionContractVersion is independent of the persisted workflow version.
const SubmissionContractVersion = 2

// MaxContributionBytes bounds stage metadata; snapshot bytes are retained separately.
const MaxContributionBytes = 4 << 20

// MaxExplanationBytes bounds each explanation in UTF-8 bytes.
const MaxExplanationBytes = 8192

// MaxExplanations bounds the evidence list for a single finding.
const MaxExplanations = 16

// Contribution contains only agent-authored values. The stage-specific wire
// schema forbids inactive fields as well as all factory envelope identities.
type Contribution struct {
	Capture     *ContributionCapture     `json:"capture,omitempty"`
	Stage       string                   `json:"stage"`
	Story       string                   `json:"story,omitempty"`
	Outcome     string                   `json:"outcome"`
	Reason      string                   `json:"reason,omitempty"`
	Plan        json.RawMessage          `json:"plan,omitempty"`
	Context     *PlanningDiscussion      `json:"context,omitempty"`
	Engineering *EngineeringContribution `json:"engineering,omitempty"`
	Assessment  *AssessmentContribution  `json:"assessment,omitempty"`
}

// ContributionCapture preserves author-reported executed checks with complete
// snapshot bytes. The factory compares files before retaining any claim.
type ContributionCapture struct {
	ImplementationDigest string                     `json:"implementation_digest"`
	Files                []ContributionCaptureFile  `json:"files"`
	Checks               []ContributionCaptureCheck `json:"checks"`
}

// ContributionCaptureFile uses explicit deletion state and base64 JSON bytes.
type ContributionCaptureFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
	Deleted bool   `json:"deleted"`
}

// ContributionCaptureCheck is reported execution, not independent observation.
type ContributionCaptureCheck struct {
	ID                   string `json:"id"`
	Name                 string `json:"name"`
	Passed               bool   `json:"passed"`
	Evidence             string `json:"evidence"`
	ImplementationDigest string `json:"implementation_digest"`
}

// PlanningDiscussion is identity-free saved Plan working context.
type PlanningDiscussion struct {
	Scope       string `json:"scope"`
	Decisions   string `json:"decisions"`
	Assumptions string `json:"assumptions"`
	Questions   string `json:"questions"`
}

// EngineeringContribution references factory-retained records, never mutable paths or claimed
// digests. The consumer supplies the actual retained provenance in scope.
type EngineeringContribution struct {
	SnapshotID string   `json:"snapshot_id"`
	CheckIDs   []string `json:"check_ids"`
}

// EvidenceExplanation relates one factual explanation to retained check records.
type EvidenceExplanation struct {
	CheckIDs []string `json:"check_ids"`
	Summary  string   `json:"summary"`
}

// ContributionFinding cannot waive obligations or create accepted state.
type ContributionFinding struct {
	Name           string                `json:"name"`
	Status         string                `json:"status"`
	Classification string                `json:"classification"`
	Evidence       []EvidenceExplanation `json:"evidence"`
}

// AssessmentContribution references immutable engineering evidence for review.
type AssessmentContribution struct {
	SnapshotID string                `json:"snapshot_id"`
	Criteria   []ContributionFinding `json:"criteria"`
	Reviews    []ContributionFinding `json:"reviews"`
}

// ContributionScope is trusted factory input. ValidatePlan is a pure consumer
// seam so the neutral agent package need not depend on the Plan content owner.
// It must validate both the complete typed plan and its rendered byte budget.
type ContributionScope struct {
	ContractVersion int
	WorkflowVersion int
	Stage           string
	Stories         map[string]ContributionStory
	ValidatePlan    func(json.RawMessage) ContributionDiagnostics
}

// ContributionStory is the authorized frozen source and retained evidence inventory.
type ContributionStory struct {
	Revision             string
	SnapshotID           string
	ImplementationDigest string
	SnapshotRetained     bool
	Criteria             []string
	ExecutionRequired    []string
	Checks               map[string]RetainedContributionCheck
}

// RetainedContributionCheck comes from captured execution, never JSON claims.
type RetainedContributionCheck struct {
	Story                string
	Revision             string
	SnapshotID           string
	ImplementationDigest string
	Executed             bool
	Passed               bool
	Retained             bool
}

// ContributionDiagnostic describes a defect without echoing untrusted field values.
type ContributionDiagnostic struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Message  string `json:"message"`
}

// ContributionDiagnostics distinguishes invalid input from incomplete traversal.
type ContributionDiagnostics struct {
	Valid          bool                     `json:"valid"`
	Complete       bool                     `json:"complete"`
	Diagnostics    []ContributionDiagnostic `json:"diagnostics"`
	UncheckedPaths []string                 `json:"unchecked_paths"`
}

func (d *ContributionDiagnostics) add(path, code, expected, actual, message string) {
	d.Diagnostics = append(d.Diagnostics, ContributionDiagnostic{path, code, expected, actual, message})
}
func (d *ContributionDiagnostics) finish() {
	slices.SortFunc(d.Diagnostics, func(a, b ContributionDiagnostic) int {
		if n := strings.Compare(a.Path, b.Path); n != 0 {
			return n
		}
		return strings.Compare(a.Code, b.Code)
	})
	slices.Sort(d.UncheckedPaths)
	d.UncheckedPaths = slices.Compact(d.UncheckedPaths)
	d.Valid = d.Complete && len(d.Diagnostics) == 0
}

// PreflightContribution performs exactly the same pure validation as submission.
// No counter, receipt, I/O, or mutable publication state exists in this API.
func PreflightContribution(data []byte, scope ContributionScope) ContributionDiagnostics {
	_, result := ParseContribution(data, scope)
	return result
}

// ParseContribution returns a usable contribution only when every check passed.
// Consumers must retain this gate immediately before any submission mutation.
func ParseContribution(data []byte, scope ContributionScope) (Contribution, ContributionDiagnostics) {
	d := ContributionDiagnostics{Complete: true, Diagnostics: []ContributionDiagnostic{}, UncheckedPaths: []string{}}
	var c Contribution
	if scope.ContractVersion != SubmissionContractVersion || (scope.WorkflowVersion != 1 && scope.WorkflowVersion != 2) {
		d.add("$", "scope", "supported factory contract and workflow", "unsupported", "Use the persisted contract and workflow.")
	}
	if len(data) > MaxContributionBytes {
		d.add("$", "limit", fmt.Sprintf("at most %d bytes", MaxContributionBytes), "oversized", "Reduce submission metadata; retain shared evidence by reference.")
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$")
		d.finish()
		return c, d
	}
	root := readContributionJSON(data, &d)
	if root == nil {
		d.finish()
		return c, d
	}
	stage := root.stringField("stage")
	outcome := root.stringField("outcome")
	schema := contributionWireSchema(stage, outcome)
	schema.check(root, "$", &d)
	if stage != scope.Stage {
		d.add("$.stage", "stage", "current bound stage", "different stage", "Submit only the stage authorized for this run.")
	}
	if (stage == "engineering" && scope.WorkflowVersion != 2) || (stage == "legacy" && scope.WorkflowVersion != 1) || (stage == "validate" && scope.WorkflowVersion != 2) {
		d.add("$.stage", "workflow", "stage allowed by persisted workflow", "incompatible stage", "Use the contribution stage for this workflow.")
	}
	// Shape failures may leave siblings fully inspectable; semantic sections below
	// are decoded separately so one bad field cannot hide another section's errors.
	if err := json.Unmarshal(data, &c); err != nil {
		d.Complete = false
		d.UncheckedPaths = append(d.UncheckedPaths, "$.semantics")
	}
	switch stage {
	case "plan":
		validatePlanContribution(root, outcome, scope, &d)
	case "engineering", "legacy", "validate":
		story, ok := scope.Stories[root.stringField("story")]
		if !ok {
			d.add("$.story", "authorization", "authorized frozen story selector", "unknown selector", "Select a story supplied in the frozen scope.")
		} else {
			story = scopeWithCapture(root, root.stringField("story"), story, &d)
			validateStoryContribution(root, stage, outcome, root.stringField("story"), story, &d)
		}
	}
	d.finish()
	if !d.Valid {
		return Contribution{}, d
	}
	return c, d
}

func validatePlanContribution(root *contributionNode, outcome string, scope ContributionScope, d *ContributionDiagnostics) {
	if outcome == "discussion" {
		var p PlanningDiscussion
		if root.decode("context", &p) {
			for _, f := range []struct{ name, value string }{{"scope", p.Scope}, {"decisions", p.Decisions}, {"assumptions", p.Assumptions}, {"questions", p.Questions}} {
				if strings.TrimSpace(f.value) == "" {
					d.add("$.context."+f.name, "blank", "nonblank string", "empty string", "Use a factual value or None.")
				}
			}
		}
	}
	if outcome == "completed" {
		node := root.field("plan")
		if node == nil || node.kind != "object" {
			return
		}
		if scope.ValidatePlan == nil {
			d.add("$.plan", "scope", "configured complete Plan validator", "unavailable", "The factory must supply the content and rendering validator.")
			d.Complete = false
			d.UncheckedPaths = append(d.UncheckedPaths, "$.plan")
			return
		}
		result := scope.ValidatePlan(node.raw)
		for _, item := range result.Diagnostics {
			item.Path = "$.plan" + strings.TrimPrefix(item.Path, "$")
			d.Diagnostics = append(d.Diagnostics, item)
		}
		for _, path := range result.UncheckedPaths {
			d.UncheckedPaths = append(d.UncheckedPaths, "$.plan"+strings.TrimPrefix(path, "$"))
		}
		if !result.Complete {
			d.Complete = false
		}
		if !result.Valid && len(result.Diagnostics) == 0 {
			d.add("$.plan", "invalid", "valid typed Plan", "rejected", "Correct the Plan before submission.")
		}
	}
}

func validateStoryContribution(root *contributionNode, stage, outcome, key string, s ContributionStory, d *ContributionDiagnostics) {
	if outcome == "blocked" || outcome == "invalidated" {
		reason := root.stringField("reason")
		if strings.TrimSpace(reason) == "" || len(reason) > 4096 {
			d.add("$.reason", "length", "nonblank string up to 4096 bytes", "invalid length", "Explain the unfinished or invalidated work.")
		}
		return
	}
	if stage == "engineering" || stage == "legacy" {
		var e EngineeringContribution
		if root.decode("engineering", &e) {
			validateSnapshot(e.SnapshotID, s, "$.engineering.snapshot_id", d)
			if len(e.CheckIDs) == 0 || len(e.CheckIDs) > 256 {
				d.add("$.engineering.check_ids", "count", "1–256 retained checks", "invalid count", "Reference the executed scoped checks.")
			}
			validateCheckReferences(e.CheckIDs, key, s, "$.engineering.check_ids", true, d)
		}
	}
	if stage == "validate" || stage == "legacy" {
		var a AssessmentContribution
		if root.decode("assessment", &a) {
			if s.SnapshotRetained || a.SnapshotID != "" {
				validateSnapshot(a.SnapshotID, s, "$.assessment.snapshot_id", d)
			} else {
				for _, group := range [][]ContributionFinding{a.Criteria, a.Reviews} {
					for _, finding := range group {
						if finding.Status == "satisfied" {
							d.add("$.assessment.snapshot_id", "missing_evidence", "retained snapshot for a proven finding", "unavailable", "Report missing evidence as unverified.")
						}
					}
				}
			}
			validateContributionFindings(a.Criteria, s.Criteria, true, key, s, "$.assessment.criteria", d)
			validateContributionFindings(a.Reviews, []string{"code", "architecture", "plan"}, false, key, s, "$.assessment.reviews", d)
			if stage == "legacy" {
				for _, group := range [][]ContributionFinding{a.Criteria, a.Reviews} {
					for _, f := range group {
						if f.Status != "satisfied" {
							d.add("$.assessment", "unproven", "satisfied legacy verification findings", "unproven finding", "Legacy verified outcomes require passing scoped findings.")
						}
					}
				}
			}
		}
	}
}

func validateSnapshot(id string, s ContributionStory, path string, d *ContributionDiagnostics) {
	if !s.SnapshotRetained || id == "" || id != s.SnapshotID || !validStoryDigest(s.ImplementationDigest) || !validStoryDigest(s.Revision) {
		d.add(path, "attribution", "retained snapshot for frozen revision", "unavailable or misattributed", "Reference the factory-retained snapshot for this exact story revision.")
	}
}

func validateCheckReferences(ids []string, key string, s ContributionStory, path string, proven bool, d *ContributionDiagnostics) {
	seen := map[string]bool{}
	for i, id := range ids {
		p := fmt.Sprintf("%s[%d]", path, i)
		check, ok := s.Checks[id]
		if seen[id] {
			d.add(p, "duplicate", "unique check reference", "duplicate", "Reference a check once in each explanation.")
		}
		seen[id] = true
		if !ok || !check.Retained || check.Story != key || check.Revision != s.Revision || check.SnapshotID != s.SnapshotID || check.ImplementationDigest != s.ImplementationDigest {
			d.add(p, "attribution", "retained check for frozen revision and snapshot", "unknown or misattributed", "Use an immutable check from the supplied evidence inventory.")
		} else if proven && (!check.Executed || !check.Passed) {
			d.add(p, "unproven", "executed passing check", "failed or unexecuted", "Do not claim proof from missing or failed execution.")
		}
	}
}

func validateContributionFindings(findings []ContributionFinding, names []string, criteria bool, key string, s ContributionStory, path string, d *ContributionDiagnostics) {
	expected := map[string]bool{}
	for _, name := range names {
		expected[name] = true
	}
	seen := map[string]bool{}
	for i, f := range findings {
		p := fmt.Sprintf("%s[%d]", path, i)
		if !expected[f.Name] || seen[f.Name] {
			d.add(p+".name", "coverage", "unique frozen criterion or required review", "unknown or duplicate", "Use exactly the supplied criterion IDs or review names.")
		}
		seen[f.Name] = true
		if f.Status != "satisfied" && f.Status != "unmet" && f.Status != "unverified" {
			d.add(p+".status", "enum", "satisfied, unmet or unverified", "unsupported string", "Report a factual assessment; agents cannot waive obligations.")
		}
		if (f.Classification != "blocking" && f.Classification != "advisory") || (criteria && f.Classification != "blocking") {
			d.add(p+".classification", "enum", "blocking criterion; blocking or advisory review", "unsupported string", "Criteria are blocking; classify review findings explicitly.")
		}
		if len(f.Evidence) < 1 || len(f.Evidence) > MaxExplanations {
			d.add(p+".evidence", "count", "1–16 explanation objects", "invalid count", "Supply attributable explanations as a list.")
		}
		checkCount := 0
		for j, e := range f.Evidence {
			ep := fmt.Sprintf("%s.evidence[%d]", p, j)
			if strings.TrimSpace(e.Summary) == "" || len(e.Summary) > MaxExplanationBytes {
				d.add(ep+".summary", "length", "nonblank string up to 8192 UTF-8 bytes", "invalid length", "Describe the actual result or missing evidence.")
			}
			if len(e.CheckIDs) > 256 {
				d.add(ep+".check_ids", "count", "at most 256 check references", "oversized array", "Reference shared retained checks.")
			}
			validateCheckReferences(e.CheckIDs, key, s, ep+".check_ids", f.Status == "satisfied", d)
			checkCount += len(e.CheckIDs)
		}
		if f.Status == "satisfied" && slices.Contains(s.ExecutionRequired, f.Name) && checkCount == 0 {
			d.add(p+".evidence", "missing_execution", "retained executed proof", "no check references", "Required execution cannot be proven by source commentary.")
		}
	}
	for _, name := range names {
		if !seen[name] {
			d.add(path, "coverage", "finding for "+name, "missing", "Cover every frozen criterion and required review.")
		}
	}
}
