package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
)

// Version identifies the coverage selection and evidence contract.
const Version = 1

// Result kinds keep factual execution separate from inherited coverage.
const (
	Executed    = "executed-pass"
	Inherited   = "inherited-pass"
	PreExisting = "pre-existing-failure"
	Transient   = "transient"
	Unresolved  = "unresolved"
	Failed      = "failed"
)

// Unit is one independently selectable package, file, project or target under
// one full command's conditions. Dependencies include test dependencies.
type Unit struct {
	Key          string   `json:"key"`
	Command      int      `json:"command"`
	Runner       string   `json:"runner"`
	Selector     string   `json:"selector"`
	Files        []string `json:"files"`
	Dependencies []string `json:"dependencies"`
}

// Inventory describes one bounded snapshot, never a filename heuristic.
type Inventory struct {
	Commit     string `json:"commit"`
	Conditions string `json:"conditions"`
	Units      []Unit `json:"units"`
	Complete   bool   `json:"complete"`
	Reason     string `json:"reason,omitempty"`
}

// Observation retains exact case identities and diagnostics before display
// truncation. Complete means the parser observed the whole required group.
type Observation struct {
	Unit       string `json:"unit"`
	Case       string `json:"case"`
	Phase      string `json:"phase,omitempty"`
	Status     string `json:"status"`
	Diagnostic string `json:"diagnostic,omitempty"`
	Complete   bool   `json:"complete"`
}

// Attempt retains one immutable execution and its observed identities.
type Attempt struct {
	ID           string        `json:"id"`
	Commit       string        `json:"commit"`
	Purpose      string        `json:"purpose"`
	Conditions   string        `json:"conditions"`
	Observations []Observation `json:"observations"`
	Complete     bool          `json:"complete"`
	Fresh        bool          `json:"fresh"`
}

// Origin always points to the original executed commit and evidence, so a
// recent partial run cannot erase intervening changes.
type Origin struct {
	Unit       string `json:"unit"`
	Commit     string `json:"commit"`
	Evidence   string `json:"evidence"`
	Conditions string `json:"conditions"`
}

// Result assesses one required coverage unit without changing raw attempts.
type Result struct {
	Unit   string        `json:"unit"`
	Kind   string        `json:"kind"`
	Origin Origin        `json:"origin"`
	Cases  []Observation `json:"cases,omitempty"`
	Reason string        `json:"reason,omitempty"`
}

// Comparison links a failure to its retry and pinned-base evidence.
type Comparison struct {
	Unit       string `json:"unit"`
	Case       string `json:"case"`
	Phase      string `json:"phase,omitempty"`
	Original   string `json:"original"`
	Retry      string `json:"retry"`
	Base       string `json:"base"`
	BaseCommit string `json:"base_commit"`
	Kind       string `json:"kind"`
}

// Manifest is the immutable assessment and its full obligation inventory.
type Manifest struct {
	Version          int           `json:"version"`
	Commit           string        `json:"commit"`
	PreviousCommit   string        `json:"previous_commit,omitempty"`
	BaseCommit       string        `json:"base_commit,omitempty"`
	Recipe           string        `json:"recipe"`
	Inventory        Inventory     `json:"inventory"`
	Results          []Result      `json:"results"`
	Attempts         []Attempt     `json:"attempts"`
	RequiredFailures []Observation `json:"required_failures,omitempty"`
	Comparisons      []Comparison  `json:"comparisons,omitempty"`
	Complete         bool          `json:"complete"`
	Outcome          string        `json:"outcome"`
	FullReason       string        `json:"full_reason,omitempty"`
	Duration         time.Duration `json:"duration_ns"`
	FullDuration     time.Duration `json:"full_duration_ns,omitempty"`
}

// Changes contains all paths changed since each original coverage anchor,
// including removed paths and both names of renames. Proven is ancestry proof.
type Changes struct {
	Anchor string
	Paths  []string
	Proven bool
}

// PlanInput supplies compatible inventory and original-anchor changes.
type PlanInput struct {
	Current  Inventory
	Previous *Manifest
	Commands []config.BugCheckCommand
	Changes  map[string]Changes
	Full     bool
	Recipe   string
}

// Work selects one bounded unit for an execution phase.
type Work struct {
	Unit    Unit
	Cases   []string
	Purpose string
}

// Plan contains ordered execution and retained coverage obligations.
type Plan struct {
	Work       []Work
	Inherited  []Result
	Unresolved []Result
	Full       bool
	Reason     string
}

// Digest computes a content identity for JSON-compatible evidence facts.
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
