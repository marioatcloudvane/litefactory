package harness

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

// The run metadata document, NNNN.meta.json (S-3).
//
// It sits beside NNNN.jsonl in the run directory and answers "what was this
// run" without a database: the exact prompt, the model, the shas either side,
// the outcome. Deleting $LF_HOME loses it and never loses code, which is the
// deal S-1 makes.
//
// Existing prompt and result fields are unredacted local evidence. Newly retained
// stderr and diagnostic fields are bounded and sanitized by internal/turn.
//
// It lives in this package rather than in internal/turn because this package
// owns the run directory: the JSONL and the metadata are one artefact pair with
// one naming rule, and splitting the writers across packages is how the two
// drift apart. turn fills in the fields only it can know — the sessions row id,
// the branch, sha_before, sha_after, the files touched — and calls [WriteMeta].
//
// turn writes it on completion *and*, best effort, from a defer on the abnormal
// paths. A metadata document naming a timeout is worth far more than a missing
// one, so a failure to write it must never take down the turn that produced it.

// metaFileMode is 0600: the document contains the full prompt, which contains
// third-party text and whatever the operator asked for.
const metaFileMode = 0o600

// Meta is the NNNN.meta.json document. Every key S-3 requires is present, and
// the three S-3 calls nullable are pointers so that "unknown" and "zero" stay
// distinguishable in the JSON — a run that never emitted a result event has an
// unknown cost, not a cost of zero.
type Meta struct {
	// Delivery records the retained, credential-free native instruction bundle.
	Delivery *agent.Delivery `json:"delivery,omitempty"`
	// RunOrdinal matches the NNNN stem of this file and the runs row.
	RunOrdinal int `json:"run_ordinal"`
	// TaskRef is the canonical forge:owner/name#issue string.
	TaskRef string `json:"task_ref"`
	// SessionID is the sessions row id. Set by internal/turn.
	SessionID int64 `json:"session_id"`
	// SegmentOrdinal is the conversation segment, 1 at M0.
	SegmentOrdinal int `json:"segment_ordinal"`
	// Agent is which coding-agent tool this run invoked. Set by internal/turn,
	// from the segment row — "claude" for every run this tree can produce
	// today.
	Agent string `json:"agent"`
	// SessionRef is litefactory's own pre-computed identity for this
	// conversation, off the segment row. Set by internal/turn.
	SessionRef string `json:"session_ref"`
	// ClaudeSessionID is a deprecated alias of SessionRef, kept so an older
	// reader of this document — or one that has not been taught about Agent
	// and SessionRef yet — still finds the value it expects. It is populated
	// only when Agent is a Claude run; empty otherwise, per SessionRef's own
	// doc: what claude was passed with --session-id, or --resume on a run
	// that continued an earlier conversation.
	ClaudeSessionID string `json:"claude_session_id"`
	// Resumed says whether this run continued the conversation that id names
	// rather than starting it. It is what joins this document to the rest of
	// the conversation: a resumed run's Prompt is only the delta it sent, and
	// everything said before it is in claude's own transcript.
	Resumed bool `json:"resumed"`
	// Mode is the run mode, "spike" at M0.
	Mode string `json:"mode"`
	// Model is the model named by the init event (AC-3.17).
	Model string `json:"model"`
	// Tools is the surface the init event reported, which reflects --tools and
	// so is the boundary claude actually ran under (SD-2). It is empty for a run
	// that never produced an init event.
	Tools []string `json:"tools"`
	// ToolsRequested is the surface the request carried, before claude had a say
	// in it, and is empty on a run that took the operator's configured default.
	//
	// It is recorded separately from Tools because the two have been observed to
	// differ silently: TodoWrite was requested on every run for the life of the
	// project and never resolved, with no error raised. Only holding both makes
	// that readable after the fact.
	ToolsRequested []string `json:"tools_requested"`
	// ToolProfile is the name of the operator-authored profile the requested
	// surface came from, empty when no profile was selected. It is a validated
	// key from the operator's own config — never text originating from the
	// forge, which reaches this package as nothing at all.
	ToolProfile string `json:"tool_profile"`
	// Prompt is the full prompt as sent, verbatim (AC-3.11).
	Prompt string `json:"prompt"`
	// WorktreePath is the run's working directory.
	WorktreePath string `json:"worktree_path"`
	// Branch is the task branch. Set by internal/turn.
	Branch string `json:"branch"`
	// SHABefore and SHAAfter bracket the run. Set by internal/turn.
	SHABefore string `json:"sha_before"`
	SHAAfter  string `json:"sha_after"`
	// Outcome is the [Outcome] string: succeeded, failed, timed_out, abandoned
	// or no_result. Not required by S-3, but it is what makes exit_code -1
	// readable without parsing the error string.
	Outcome string `json:"outcome"`
	// ExitCode is claude's status, or [SignalExitCode] for a signal or timeout.
	ExitCode int `json:"exit_code"`
	// StartedAt and FinishedAt are RFC 3339 timestamps.
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	// DurationMS is the wall clock across the subprocess.
	DurationMS int64 `json:"duration_ms"`
	// CostUSD is the terminal result event's total_cost_usd, null when no
	// result event arrived.
	CostUSD *float64 `json:"cost_usd"`
	// ResultText is claude's final message. Untrusted content.
	ResultText string `json:"result_text"`
	// FilesTouched is the run's diff file list. Set by internal/turn.
	FilesTouched []string `json:"files_touched"`
	// ResultCommentID is the forge id of the report comment, null until LF-4
	// has posted it.
	ResultCommentID *int64 `json:"result_comment_id"`
	// Error explains a non-success outcome, null on success (S-9).
	Error *string `json:"error"`
	// LogPath is the absolute path of the sibling NNNN.jsonl.
	LogPath string `json:"log_path"`
	// Stderr is a bounded sanitized tail, supplied by internal/turn.
	Stderr string `json:"stderr,omitempty"`
	// Diagnostic retains sanitized failure guidance independently of Error.
	Diagnostic string `json:"diagnostic,omitempty"`
}

// NewMeta prefills everything this package knows about a run.
//
// The fields it leaves zero are the ones only internal/turn can supply:
// SessionID, Branch, SHABefore, SHAAfter, FilesTouched and ResultCommentID.
// result may be nil — a run killed before it produced one still gets a metadata
// document, which is the point of writing it best effort.
func NewMeta(req Request, prompt string, result *RunResult) Meta {
	ts := toolSurface(req)
	meta := Meta{
		RunOrdinal:     req.RunOrdinal,
		TaskRef:        req.Ref.String(),
		SegmentOrdinal: req.SegmentOrdinal,
		Resumed:        req.Path.Resuming(),
		Mode:           req.Mode,
		Prompt:         prompt,
		WorktreePath:   req.WorktreePath,
		Outcome:        OutcomeAbandoned.String(),
		ExitCode:       SignalExitCode,
		Tools:          []string{},
		ToolsRequested: append([]string{}, ts.Tools...),
		ToolProfile:    req.Permissions.Profile,
		FilesTouched:   []string{},
	}
	if req.Delivery != nil {
		delivery := req.Delivery.Clone()
		meta.Delivery = &delivery
	}
	if result == nil {
		return meta
	}

	meta.ClaudeSessionID = result.SessionID
	meta.Model = result.Model
	meta.Outcome = result.Outcome.String()
	meta.ExitCode = result.ExitCode
	meta.ResultText = result.ResultText
	meta.LogPath = result.LogPath
	if len(result.Tools) > 0 {
		meta.Tools = append([]string(nil), result.Tools...)
	}
	if !result.StartedAt.IsZero() {
		meta.StartedAt = result.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if !result.FinishedAt.IsZero() {
		meta.FinishedAt = result.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	meta.DurationMS = result.Duration.Milliseconds()
	if result.HasCost || result.CostUSD > 0 {
		cost := result.CostUSD
		meta.CostUSD = &cost
	}
	if result.Reason != "" {
		reason := result.Reason
		meta.Error = &reason
	}
	return meta
}

// MetaPath returns where a run's metadata document belongs.
func MetaPath(home string, ref naming.Ref, ordinal int) (string, error) {
	return naming.RunMetaPath(home, ref, ordinal)
}

// WriteMeta writes the metadata document to path, atomically and 0600.
//
// Atomic because a caller writes this from a defer on the failure paths: a
// crash mid-write must not leave a half-parsed document where a missing one
// would have been honest.
func WriteMeta(path string, meta Meta) error {
	if path == "" || !filepath.IsAbs(path) {
		return invalidRequest("run metadata path %q must be an absolute path", path)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("encode run metadata: %w", err)
	}
	return writeFileAtomic(path, append(data, '\n'), metaFileMode)
}
