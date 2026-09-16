// One-off US-009 inventory. Run from this repository with go run <this-file>.
// This is session tooling, not a litefactory command. All database access uses
// existing read-only store accessors; no migrations or remote operations occur.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

type cohort struct {
	Task           string   `json:"task"`
	SessionID      int64    `json:"session_id"`
	State          string   `json:"session_state"`
	Active         bool     `json:"active"`
	Pending        bool     `json:"owner_pending"`
	Managed        bool     `json:"managed"`
	Generation     int      `json:"generation"`
	Workflow       int      `json:"workflow_version"`
	Content        int      `json:"content_version"`
	Submission     int      `json:"submission_version"`
	FeatureState   string   `json:"feature_state"`
	Unfinished     []string `json:"unfinished_stories"`
	UpgradeState   string   `json:"upgrade_state"`
	UpgradeScope   string   `json:"upgrade_scope_hash,omitempty"`
	UpgradeRefusal string   `json:"upgrade_refusal,omitempty"`
	Next           string   `json:"next_step"`
}

// Project identifiers/states only; never serialize store rows containing text,
// provider context, bodies, capabilities, or submission payloads.
type record struct {
	Kind       string `json:"kind"`
	ID         int64  `json:"id,omitempty"`
	RunID      int64  `json:"run_id,omitempty"`
	Task       string `json:"task,omitempty"`
	Generation int    `json:"generation,omitempty"`
	State      string `json:"state,omitempty"`
	Reference  string `json:"reference,omitempty"`
}

type report struct {
	Database string    `json:"database"`
	Started  time.Time `json:"started_at"`
	Finished time.Time `json:"finished_at"`
	Boundary int64     `json:"session_boundary"`
	Cohorts  []cohort  `json:"cohorts"`
	Records  []record  `json:"records"`
	Errors   []string  `json:"errors"`
	Coverage []string  `json:"coverage_limits"`
	Gates    []record  `json:"removal_gates"`
}

func main() {
	path := flag.String("db", "", "existing database; default config.Locate().DBPath()")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "unexpected positional arguments")
		os.Exit(1)
	}
	if *path == "" {
		paths, err := config.Locate()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		*path = paths.DBPath()
	}
	abs, err := filepath.Abs(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	r := collect(ctx, abs)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(r); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if len(r.Errors) != 0 {
		os.Exit(2)
	}
}

func collect(ctx context.Context, path string) report {
	r := report{Database: path, Started: time.Now().UTC(), Cohorts: []cohort{}, Records: []record{}, Errors: []string{},
		Coverage: []string{
			"Read-only observation interval, not an atomic snapshot: the daemon may advance records between accessor calls.",
			"Registered canonical session owners, including closed owners; managed child sessions are folded into owner activity/debt by the existing store accessor.",
			"Current managed generations plus active StoryProgress generation bindings. Historical attempt bindings, orphan managed features without sessions, child continuation details and scheduler queues are not exhaustively enumerated.",
			"Existing accessors enumerate selected pending effects, not all publication/revision/acceptance/recovery ledgers. Owner_pending is supplementary checkout debt, not a complete drain verdict.",
			"Unmanaged sessions, future admissions, supported restart behavior and uncertain remote effects require factory/operator disposition. No forge calls are made.",
			"All three readers remain required until separate complete compatibility and admission-boundary evidence is supplied. Exit 0 means inventory reads completed, never permission to remove a reader.",
		},
	}
	for _, reader := range []string{"Plan v1 file handoff", "Implement event-file ingress", "Validate v1 assessment parser"} {
		r.Gates = append(r.Gates, record{Kind: reader, State: "unknown; retain", Reference: "cohort candidates and pending records are evidence for follow-up, not deletion authority"})
	}
	db, err := store.OpenMeterReader(ctx, path)
	if err != nil {
		r.Errors = append(r.Errors, "open read-only database: "+err.Error())
		r.Finished = time.Now().UTC()
		return r
	}
	r.read(ctx, db)
	if err := db.Close(); err != nil {
		r.Errors = append(r.Errors, "close: "+err.Error())
	}
	r.Finished = time.Now().UTC()
	return r
}

func (r *report) note(label string, err error) bool {
	if err == nil {
		return false
	}
	r.Errors = append(r.Errors, label+": "+err.Error())
	return true
}

func (r *report) read(ctx context.Context, db *store.DB) {
	boundary, err := db.RetirementBoundary(ctx)
	if r.note("session boundary", err) {
		return
	}
	r.Boundary = boundary
	for after := int64(0); after < boundary; {
		owners, err := db.ListRetirementOwners(ctx, after, boundary, 100)
		if r.note("session owners", err) || len(owners) == 0 {
			break
		}
		for _, owner := range owners {
			after = owner.Session.ID
			r.readOwner(ctx, db, owner)
		}
	}
	progress, err := db.ListStoryProgressRuns(ctx)
	if !r.note("active StoryProgress", err) {
		for _, p := range progress {
			r.Records = append(r.Records, record{Kind: "active_story_progress", RunID: p.RunID, Task: p.Parent, Generation: p.Generation, State: string(p.Status)})
		}
	}
	effects, err := db.ListStoryProgressEffects(ctx)
	if !r.note("pending StoryProgress effects", err) {
		for _, p := range effects {
			r.Records = append(r.Records, record{Kind: "story_progress_effect", ID: p.ID, RunID: p.RunID, Task: p.Parent, Generation: p.Generation, State: p.Kind, Reference: p.Revision})
		}
	}
	receipts, err := db.ListPendingSubmissions(ctx)
	if !r.note("pending submissions", err) {
		for _, p := range receipts {
			r.Records = append(r.Records, record{Kind: "submission_receipt", ID: p.ID, RunID: p.RunID, Reference: p.RequestID, State: fmt.Sprintf("input=%s engineering=%s acceptance=%s effects=%s plan=%s", p.InputState, p.EngineeringState, p.AcceptanceState, p.EffectState, p.PlanState)})
		}
	}
	deliveries, err := db.ListPendingReportDeliveries(ctx)
	if !r.note("pending reports", err) {
		for _, p := range deliveries {
			r.Records = append(r.Records, record{Kind: "report_delivery", RunID: p.RunID, Task: p.Task, Reference: p.Key, State: fmt.Sprintf("possibly_sent=%t comment_id=%d", p.PossiblySent, p.CommentID)})
		}
	}
	var run int64
	var attempt, request string
	for {
		pending, err := db.PendingCommentOperations(ctx, run, attempt, request, 100)
		if r.note("uncertain comments", err) || len(pending) == 0 {
			break
		}
		for _, p := range pending {
			r.Records = append(r.Records, record{Kind: "uncertain_comment", RunID: p.Scope.RunID, Task: p.Scope.Task, State: p.State, Reference: p.RequestID})
			run, attempt, request = p.Scope.RunID, p.Scope.Attempt, p.RequestID
		}
	}
	for after := int64(0); ; {
		pending, err := db.PendingCreationOperations(ctx, after, 100)
		if r.note("pending creations", err) || len(pending) == 0 {
			break
		}
		for _, p := range pending {
			r.Records = append(r.Records, record{Kind: "pending_creation", ID: p.ID, RunID: p.Scope.RunID, Task: p.Scope.Task})
			after = p.ID
		}
	}
}

func (r *report) readOwner(ctx context.Context, db *store.DB, owner store.RetirementOwner) {
	s := owner.Session
	c := cohort{Task: s.TaskRef, SessionID: s.ID, State: string(s.State), Active: owner.Active, Pending: owner.Pending, Unfinished: []string{}, UpgradeState: "none observed", Next: "Resolve unmanaged/restart support before reader removal."}
	defer func() { r.Cohorts = append(r.Cohorts, c) }()
	if r.note(fmt.Sprintf("owner %d", s.ID), owner.ObservationError) {
		return
	}
	feature, err := db.GetManagedFeature(ctx, s.TaskRef)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		r.note(s.TaskRef+" feature", err)
		return
	}
	if err == nil {
		c.Managed = true
		c.Generation = feature.Generation
		c.Workflow = feature.WorkflowVersion
		c.FeatureState = feature.State
		contract, err := db.GetFeatureContract(ctx, s.TaskRef)
		if !r.note(s.TaskRef+" contract", err) {
			c.Content = contract.ContentVersion
			c.Submission = contract.SubmissionVersion
		}
		stories, err := db.ListManagedStories(ctx, s.TaskRef, feature.Generation)
		if !r.note(s.TaskRef+" stories", err) {
			for _, story := range stories {
				if story.State != "completed" {
					c.Unfinished = append(c.Unfinished, story.Key)
				}
			}
		}
		c.Next = "Retain current workflow; confirm old attempts/effects and admission boundary separately."
		if c.Content == 1 || c.Submission == 1 {
			c.Next = "Legacy candidate: complete under existing contract or have the operator run story-contract upgrade <task> --preview, then authorized --apply and a fresh published/approved Plan. Workflow version is preserved."
			if err := db.ContractUpgradeSafety(ctx, s.TaskRef, feature.Generation); err != nil {
				c.UpgradeRefusal = err.Error()
			}
		}
		upgrade, err := db.GetContractUpgrade(ctx, s.TaskRef)
		if err == nil {
			c.UpgradeState = upgrade.State
			c.UpgradeScope = upgrade.ScopeHash
		} else if !errors.Is(err, store.ErrNotFound) {
			r.note(s.TaskRef+" upgrade", err)
		}
	}
	continuations, err := db.ListPendingContinuations(ctx, s.ID)
	if !r.note(s.TaskRef+" continuations", err) {
		for _, p := range continuations {
			r.Records = append(r.Records, record{Kind: "continuation", ID: p.ID, RunID: p.SourceRunID, Task: s.TaskRef, State: p.Status})
		}
	}
	runs, err := db.ListRuns(ctx, s.ID)
	if !r.note(s.TaskRef+" runs", err) {
		for _, p := range runs {
			if !p.Succeeded() || p.FinishedAt.IsZero() {
				r.Records = append(r.Records, record{Kind: "unfinished_or_unsuccessful_run", ID: p.ID, Task: s.TaskRef, State: string(p.Status), Reference: p.Mode})
			}
		}
	}
}
