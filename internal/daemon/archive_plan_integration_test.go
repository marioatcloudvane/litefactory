package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/archive"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// Plan history archive and forget on a real migrated SQLite and real temp
// directories. The forge and the git clone are the only stubs.

var planITTables = []string{
	"plan_story_drafts", "plan_story_candidates", "plan_story_effects", "plan_story_authorities",
	"plan_story_completions", "plan_story_owner_edits", "plan_story_recovery_cursors",
	"plan_story_notice_states", "plan_story_notices", "plan_reply_explanations",
	"plan_workflow_states", "plan_workflow_history", "plan_execution_paths",
}

const planITDraftJSON = `{"title":"é世 \"quoted\"","n":9007199254740993}`

// planITIssues answers the closure read per issue number.
type planITIssues struct{ open map[int]bool }

func (f *planITIssues) GetIssue(_ context.Context, r github.Repo, n int) (github.Issue, error) {
	state := "closed"
	if f.open[n] {
		state = "open"
	}
	return github.Issue{ID: int64(n) * 10, Number: n, State: state, APIURL: fmt.Sprintf("https://api.github.com/repos/%s/%s/issues/%d", r.Owner, r.Name, n)}, nil
}

func (x *archiveITFixture) seedPlan(t *testing.T) map[string]int64 {
	t.Helper()
	p := x.parent.TaskRef
	x.exec(t, `INSERT INTO plan_story_drafts(parent,story_key,ordinal,issue_id,issue_number,revision,candidate,stage,draft_json,retired) VALUES(?,?,?,?,?,?,?,?,?,0)`, p, "US-001", 1, 2550, 255, "r1", "c1", "story", planITDraftJSON)
	x.exec(t, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?,?,2)`, p, "US-002")
	x.exec(t, `INSERT INTO plan_story_candidates(parent,request_id,story_key,candidate,request_json) VALUES(?,?,?,?,?)`, p, "req-1", "US-001", "c1", `{"r":1}`)
	x.exec(t, `INSERT INTO plan_story_effects(parent,request_id,phase,outcome,issue_id,issue_number,detail,revision) VALUES(?,?,?,?,?,?,?,?)`, p, "req-1", "apply", "applied", 2550, 255, "done", "r1")
	x.exec(t, `INSERT INTO plan_story_authorities(parent,request_id,authority_json) VALUES(?,?,?)`, p, "req-1", `{"a":"é"}`)
	x.exec(t, `INSERT INTO plan_story_completions(parent,request_id) VALUES(?,?)`, p, "req-1")
	x.exec(t, `INSERT INTO plan_story_owner_edits(parent,story_key,observation_id,candidate,base_revision,observed_revision,observed_title,observed_body,draft_json,stage) VALUES(?,?,?,?,?,?,?,?,?,?)`, p, "US-001", "obs1", "c1", "b", "o", "title", "body\x00ish", "{}", "story")
	x.exec(t, `INSERT INTO plan_story_recovery_cursors(parent,request_id) VALUES(?,?)`, p, "req-1")
	x.exec(t, `INSERT INTO plan_story_notice_states(parent,story_key,material_json) VALUES(?,?,?)`, p, "US-001", `{"m":1}`)
	x.exec(t, `INSERT INTO plan_story_notices(parent,story_key,material_json,delivered,attempts) VALUES(?,?,?,1,2)`, p, "US-001", `{"m":1}`)
	x.exec(t, `INSERT INTO plan_reply_explanations(delivery_key,parent,story_key,child_number,comment_id,reason) VALUES(?,?,?,?,?,?)`, "dk1", p, "US-001", 255, 77, "why")
	x.exec(t, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,?,?)`, p, 2, `{"s":2}`)
	x.exec(t, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,?,?)`, p, 1, `{"s":1}`)
	x.exec(t, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,?,?)`, p, 2, `{"s":2}`)
	x.exec(t, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'graph')`, x.parent.ID)
	x.exec(t, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'legacy')`, x.child.ID)
	// An unrelated task's Plan state must never be touched.
	o := x.other.TaskRef
	x.exec(t, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?,?,1)`, o, "US-900")
	x.exec(t, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,'{}')`, o)
	x.exec(t, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,1,'{}')`, o)
	x.exec(t, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'graph')`, x.other.ID)
	return x.planCounts(t, p)
}

// planCounts counts the family's rows per Plan table (execution paths by session).
func (x *archiveITFixture) planCounts(t *testing.T, parent string) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, tb := range planITTables {
		q := `SELECT COUNT(*) FROM ` + tb + ` WHERE parent=?`
		args := []any{parent}
		if tb == "plan_execution_paths" {
			q = `SELECT COUNT(*) FROM plan_execution_paths WHERE session_id IN (SELECT id FROM sessions WHERE task_ref=? OR issue_number=255)`
		}
		out[tb] = x.count(t, q, args...)
	}
	return out
}

func (x *archiveITFixture) grants(t *testing.T) int64 {
	t.Helper()
	return x.count(t, `SELECT COUNT(*) FROM plan_archive_grants`)
}

func (x *archiveITFixture) otherPlan(t *testing.T) int64 {
	t.Helper()
	return x.count(t, `SELECT (SELECT COUNT(*) FROM plan_story_drafts WHERE parent=?)+(SELECT COUNT(*) FROM plan_workflow_states WHERE parent=?)+(SELECT COUNT(*) FROM plan_workflow_history WHERE parent=?)+(SELECT COUNT(*) FROM plan_execution_paths WHERE session_id=?)`, x.other.TaskRef, x.other.TaskRef, x.other.TaskRef, x.other.ID)
}

func planITSum(m map[string]int64) (n int64) {
	for _, v := range m {
		n += v
	}
	return n
}

func (x *archiveITFixture) expectProtective(t *testing.T) {
	t.Helper()
	for _, tc := range []struct{ table, want string }{
		{"plan_story_drafts", "reserved Plan ordinal"},
		{"plan_story_candidates", "immutable Plan candidate"},
		{"plan_story_effects", "immutable Plan effect"},
		{"plan_story_authorities", "immutable Plan authority"},
		{"plan_story_owner_edits", "immutable owner edit observation"},
		{"plan_workflow_history", "immutable Plan workflow history"},
	} {
		if x.count(t, `SELECT COUNT(*) FROM `+tc.table) == 0 {
			continue
		}
		_, err := x.raw.ExecContext(t.Context(), `DELETE FROM `+tc.table)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("delete from %s: err=%v, want %q", tc.table, err, tc.want)
		}
	}
	if g := x.grants(t); g != 0 {
		t.Fatalf("grant table not empty: %d", g)
	}
}

func TestArchivePlanIntegrationRoundTripDeletesAndProtects(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	seeded := x.seedPlan(t)
	for _, tb := range planITTables {
		if seeded[tb] == 0 {
			t.Fatalf("table %s not seeded: %v", tb, seeded)
		}
	}
	otherBefore := x.otherPlan(t)
	want := x.liveRows(t)
	skeleton := x.skeleton(t)
	x.expectProtective(t)

	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatalf("pass errors: %v", errs)
	}
	if got := x.planCounts(t, x.parent.TaskRef); planITSum(got) != 0 {
		t.Fatalf("plan rows remain: %v", got)
	}
	if n := x.archivedTotal(t); n != 0 {
		t.Fatalf("%d archived rows remain", n)
	}
	if got := x.otherPlan(t); got != otherBefore {
		t.Fatalf("unrelated plan rows changed: %d -> %d", otherBefore, got)
	}
	if !reflect.DeepEqual(skeleton, x.skeleton(t)) {
		t.Fatal("skeleton changed")
	}
	if g := x.grants(t); g != 0 {
		t.Fatalf("grant table not empty after commit: %d", g)
	}

	files := x.files(t)
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	var got []archive.Row
	contents, err := archive.Read(files[0], func(r archive.Row) error { got = append(got, r); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archiveITRowsEqual(t, got, want)
	for _, tb := range planITTables {
		if contents.Trailer.Counts[tb] != seeded[tb] {
			t.Fatalf("trailer %s=%d, want %d", tb, contents.Trailer.Counts[tb], seeded[tb])
		}
	}
	seen := map[string]bool{}
	for _, r := range got {
		seen[r.Table] = true
		switch r.Table {
		case "plan_story_drafts":
			if c := archiveITColumn(t, r, "draft_json"); archiveITColumn(t, r, "story_key").Value == "US-001" && c.Value != planITDraftJSON {
				t.Fatalf("draft_json changed: %#v", c.Value)
			}
		case "plan_story_owner_edits":
			if c := archiveITColumn(t, r, "observed_body"); fmt.Sprint(c.Value) != "body\x00ish" {
				t.Fatalf("observed_body changed: %#v", c.Value)
			}
		}
	}
	for _, tb := range planITTables {
		if !seen[tb] {
			t.Fatalf("archive lacks %s", tb)
		}
	}
	v := x.lastVisit(t, x.parent.TaskRef)
	rows := v["rows"].(map[string]any)
	for _, tb := range planITTables {
		if rows[tb] != float64(seeded[tb]) {
			t.Fatalf("visit rows %s=%v, want %d", tb, rows[tb], seeded[tb])
		}
	}
	// Triggers are protective again afterwards: a new row cannot be deleted.
	x.exec(t, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?,?,1)`, x.parent.TaskRef, "US-NEW")
	x.exec(t, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,'{}')`, x.parent.TaskRef)
	x.exec(t, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,1,'{}')`, x.parent.TaskRef)
	x.expectProtective(t)
	if _, err := x.raw.ExecContext(t.Context(), `DELETE FROM plan_story_drafts WHERE parent=?`, x.other.TaskRef); err == nil {
		t.Fatal("other task's draft deletable without grant")
	}
}

func TestArchivePlanIntegrationFaultDigestMismatchLeavesPlanIntact(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	seeded := x.seedPlan(t)
	skeleton := x.skeleton(t)
	var mutated []archive.Row
	x.d.archive.BeforeDelete = func(context.Context) error {
		x.exec(t, `UPDATE plan_story_drafts SET draft_json='changed after verify' WHERE story_key='US-001'`)
		mutated = x.liveRows(t)
		x.d.archive.BeforeDelete = nil
		return nil
	}
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	if mutated == nil {
		t.Fatal("seam not reached")
	}
	x.expectUntouched(t, mutated, skeleton, "digest_mismatch")
	if got := x.planCounts(t, x.parent.TaskRef); !reflect.DeepEqual(got, seeded) {
		t.Fatalf("plan rows changed: %v want %v", got, seeded)
	}
	if g := x.grants(t); g != 0 {
		t.Fatalf("grants left: %d", g)
	}
	if len(x.files(t)) != 0 {
		t.Fatalf("stale file kept: %v", x.files(t))
	}
	x.expectProtective(t)
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
	if planITSum(x.planCounts(t, x.parent.TaskRef)) != 0 {
		t.Fatal("retry did not remove plan rows")
	}
}

func TestArchivePlanIntegrationFaultMidDeleteRollsBackGrantsAndRows(t *testing.T) {
	x := newArchiveITFixture(t)
	x.seed(t)
	seeded := x.seedPlan(t)
	before := x.liveRows(t)
	skeleton := x.skeleton(t)
	// plan_execution_paths is deleted last: every earlier Plan table is already
	// deleted under a grant when this fails.
	x.exec(t, `CREATE TRIGGER plan_it_fail BEFORE DELETE ON plan_execution_paths BEGIN SELECT RAISE(ABORT,'injected delete failure'); END`)
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	x.expectUntouched(t, before, skeleton, "delete_failed")
	if got := x.planCounts(t, x.parent.TaskRef); !reflect.DeepEqual(got, seeded) {
		t.Fatalf("plan rows not rolled back: %v want %v", got, seeded)
	}
	if g := x.grants(t); g != 0 {
		t.Fatalf("grants survived rollback: %d", g)
	}
	if len(x.files(t)) != 0 {
		t.Fatalf("file kept after rollback: %v", x.files(t))
	}
	x.expectProtective(t)
	x.exec(t, `DROP TRIGGER plan_it_fail`)
	x.expectArchivedNextPass(t, x.sweep(t).NextDue)
	if planITSum(x.planCounts(t, x.parent.TaskRef)) != 0 || x.grants(t) != 0 {
		t.Fatal("retry left plan rows or grants")
	}
}

// planForgetGit is a clone stand-in: no worktree, no branches by default.
type planForgetGit struct {
	worktree bool
	onOpen   func()
}

func (g *planForgetGit) open(context.Context, store.RetirementOwner) (RetirementGit, error) {
	if g.onOpen != nil {
		g.onOpen()
	}
	return g, nil
}

func (g *planForgetGit) Observe(context.Context) (git.RetirementObservation, error) {
	o := git.RetirementObservation{WorktreeSafe: true}
	if g.worktree {
		o.WorktreeExists, o.WorktreeRegistered = true, true
		o.LocalSHA, o.LocalPreserved, o.LocalMerged = "abc", true, true
	}
	return o, nil
}
func (g *planForgetGit) Remove(context.Context, git.RetirementObservation, string) error {
	return errors.New("forget must never remove resources")
}
func (g *planForgetGit) Absent(context.Context, string) (bool, error) { return !g.worktree, nil }

// planForgetFixture archives the family first, then arms the forget gates.
type planForgetFixture struct {
	*archiveITFixture
	git    *planForgetGit
	issues *planITIssues
	runs   string
	seeded map[string]int64
}

func newPlanForgetFixture(t *testing.T) *planForgetFixture {
	t.Helper()
	x := newArchiveITFixture(t)
	x.seed(t)
	f := &planForgetFixture{archiveITFixture: x, git: &planForgetGit{}, issues: &planITIssues{open: map[int]bool{255: true, 300: true}}, runs: t.TempDir()}
	f.seeded = x.seedPlan(t)
	x.d.archive.Issues = f.issues
	x.d.retirement.OpenGit = f.git.open
	x.d.archive.RunsDir = f.runs
	if errs := x.runPass(t, x.now); len(errs) != 0 {
		t.Fatal(errs)
	}
	if planITSum(x.planCounts(t, x.parent.TaskRef)) != 0 || len(x.files(t)) != 1 {
		t.Fatalf("not archived: %v files=%v", x.planCounts(t, x.parent.TaskRef), x.files(t))
	}
	return f
}

func (f *planForgetFixture) pass(t *testing.T) map[string]any {
	t.Helper()
	if errs := f.runPass(t, f.sweep(t).NextDue); len(errs) != 0 {
		t.Fatal(errs)
	}
	return f.lastVisit(t, f.parent.TaskRef)
}

func (f *planForgetFixture) expectKept(t *testing.T, reason string) {
	t.Helper()
	skeleton := f.skeleton(t)
	v := f.pass(t)
	if v["outcome"] != ArchiveOutcomeKept || v["reason"] != reason {
		t.Fatalf("visit %v, want kept/%s", v, reason)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM sessions WHERE id=?`, f.parent.ID); n != 1 {
		t.Fatalf("skeleton gone under %s", reason)
	}
	if !reflect.DeepEqual(skeleton, f.skeleton(t)) {
		t.Fatalf("skeleton changed under %s", reason)
	}
}

func (f *planForgetFixture) runLog(t *testing.T) string {
	t.Helper()
	return filepath.Join(f.runs, f.repo.Forge, "acme", "api", strconv.Itoa(213))
}

func TestArchivePlanIntegrationForgetGatesThenSuccess(t *testing.T) {
	f := newPlanForgetFixture(t)
	other := f.otherPlan(t)

	// archive_file_present: the file is still there.
	f.expectKept(t, ForgetReasonArchive)
	if err := os.RemoveAll(f.taskDir()); err != nil {
		t.Fatal(err)
	}

	// run_logs_present.
	if err := os.MkdirAll(f.runLog(t), 0o700); err != nil {
		t.Fatal(err)
	}
	f.expectKept(t, ForgetReasonRunLogs)
	if err := os.RemoveAll(f.runLog(t)); err != nil {
		t.Fatal(err)
	}

	// worktree_or_branch_present.
	f.git.worktree = true
	f.expectKept(t, ForgetReasonResources)
	f.git.worktree = false

	// not_settled: the issue is not (or no longer) closed.
	f.issues.open[213] = true
	f.expectKept(t, ForgetReasonNotSettled)
	f.issues.open[213] = false

	// not_settled: a child starts running after the settled check, before the delete.
	f.git.onOpen = func() {
		f.exec(t, `UPDATE sessions SET lock_owner='other-worker' WHERE id=?`, f.child.ID)
		f.git.onOpen = nil
	}
	skeleton := f.skeleton(t)
	v := f.pass(t)
	if v["outcome"] != ArchiveOutcomeKept || v["reason"] != ForgetReasonNotSettled {
		t.Fatalf("late lock visit %v", v)
	}
	f.exec(t, `UPDATE sessions SET lock_owner=NULL WHERE id=?`, f.child.ID)
	skeleton["sessions"] = f.skeleton(t)["sessions"]
	if !reflect.DeepEqual(skeleton, f.skeleton(t)) {
		t.Fatal("skeleton changed by refused forget")
	}

	// plan_rows_left: a Plan row appears after the fresh read, before the delete.
	f.git.onOpen = func() {
		f.exec(t, `INSERT INTO plan_story_notice_states(parent,story_key,material_json) VALUES(?,?,?)`, f.parent.TaskRef, "US-LATE", "{}")
		f.git.onOpen = nil
	}
	f.expectKept(t, ForgetReasonPlanRows)
	if n := f.count(t, `SELECT COUNT(*) FROM plan_story_notice_states WHERE story_key='US-LATE'`); n != 1 {
		t.Fatal("late row was lost")
	}
	f.exec(t, `DELETE FROM plan_story_notice_states WHERE story_key='US-LATE'`)

	// Every gate clear: the skeleton goes and nothing of the family remains.
	v = f.pass(t)
	if v["outcome"] != ArchiveOutcomeForgotten {
		t.Fatalf("visit %v, want forgotten", v)
	}
	if rows, _ := v["rows"].(map[string]any); rows["sessions"] == nil || rows["managed_features"] == nil {
		t.Fatalf("forgotten rows %v", v["rows"])
	}
	if n := f.count(t, `SELECT COUNT(*) FROM sessions WHERE id IN (?,?)`, f.parent.ID, f.child.ID); n != 0 {
		t.Fatalf("%d family sessions remain", n)
	}
	for _, q := range []string{
		`SELECT COUNT(*) FROM managed_features WHERE parent_task=?`,
		`SELECT COUNT(*) FROM managed_stories WHERE parent_task=?`,
	} {
		if n := f.count(t, q, f.parent.TaskRef); n != 0 {
			t.Fatalf("%s: %d", q, n)
		}
	}
	if n := f.count(t, `SELECT COUNT(*) FROM plan_execution_paths WHERE session_id IN (?,?)`, f.parent.ID, f.child.ID); n != 0 {
		t.Fatalf("plan_execution_paths %d", n)
	}
	if got := f.planCounts(t, f.parent.TaskRef); planITSum(got) != 0 {
		t.Fatalf("family tables not empty: %v", got)
	}
	if g := f.grants(t); g != 0 {
		t.Fatalf("grants %d", g)
	}
	if got := f.otherPlan(t); got != other {
		t.Fatalf("unrelated plan state changed: %d -> %d", other, got)
	}
	if n := f.count(t, `SELECT COUNT(*) FROM sessions WHERE id=?`, f.other.ID); n != 1 {
		t.Fatal("unrelated session forgotten")
	}

	// Idempotent: another pass and a direct repeat change nothing and do not fail.
	after := f.skeleton(t)
	if errs := f.runPass(t, f.sweep(t).NextDue); len(errs) != 0 {
		t.Fatal(errs)
	}
	if !reflect.DeepEqual(after, f.skeleton(t)) || f.otherPlan(t) != other {
		t.Fatal("repeat pass changed state")
	}
	if _, err := f.db.ForgetArchivedFamily(t.Context(), f.family(), f.now); err == nil {
		t.Fatal("forgetting a forgotten family claimed success")
	}
	if !reflect.DeepEqual(after, f.skeleton(t)) {
		t.Fatal("repeat forget changed state")
	}
}

func TestArchivePlanIntegrationReopenStartsFreshPlanState(t *testing.T) {
	f := newPlanForgetFixture(t)
	if err := os.RemoveAll(f.taskDir()); err != nil {
		t.Fatal(err)
	}
	if v := f.pass(t); v["outcome"] != ArchiveOutcomeForgotten {
		t.Fatalf("visit %v", v)
	}
	oldID := f.parent.ID

	s, err := f.db.GetOrCreateSession(t.Context(), f.repo, 213)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == oldID || s.TaskRef != f.parent.TaskRef {
		t.Fatalf("reopened session %+v old %d", s, oldID)
	}
	if got := f.planCounts(t, s.TaskRef); planITSum(got) != 0 {
		t.Fatalf("reopened session inherited plan state: %v", got)
	}
	// Ordinals and revisions restart: nothing of the old history is reserved.
	p := s.TaskRef
	f.exec(t, `INSERT INTO plan_story_drafts(parent,story_key,ordinal) VALUES(?,?,1)`, p, "US-001")
	f.exec(t, `INSERT INTO plan_workflow_states(parent,revision,state_json) VALUES(?,1,'{"fresh":true}')`, p)
	f.exec(t, `INSERT INTO plan_workflow_history(parent,revision,state_json) VALUES(?,1,'{}')`, p)
	f.exec(t, `INSERT INTO plan_execution_paths(session_id,path) VALUES(?,'graph')`, s.ID)
	if n := f.count(t, `SELECT COUNT(*) FROM plan_story_drafts WHERE parent=?`, p); n != 1 {
		t.Fatalf("drafts %d", n)
	}
	f.expectProtective(t)
}
