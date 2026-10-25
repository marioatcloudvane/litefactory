package daemon

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestUnavailableDoctorApplyReportsRetainedRepair(t *testing.T) {
	for _, state := range []string{"absent", "active", "settled"} {
		t.Run(state, func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			session, err := d.db.GetOrCreateSession(ctx, repo, 406)
			if err != nil {
				t.Fatal(err)
			}
			var repair store.DoctorRepair
			if state != "absent" {
				ref, err := naming.ParseRef(session.TaskRef)
				if err != nil {
					t.Fatal(err)
				}
				evidence, err := d.db.ReadDoctorEvidence(ctx, ref)
				if err != nil {
					t.Fatal(err)
				}
				approveEffect(t, d.db, session, store.DoctorRepairStep{Number: 1, Kind: "poll_flag", Target: "session:" + strconv.FormatInt(session.ID, 10), Before: "0", After: "1"}, evidence)
				if state == "settled" {
					if err := d.db.StopDoctorRepair(ctx, session.TaskRef, "Fixture repair stopped"); err != nil {
						t.Fatal(err)
					}
				}
				repair, err = d.db.ReadDoctorRepair(ctx, session.TaskRef)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := d.db.AdmitDoctorCommand(ctx, store.DoctorCommand{RepoID: repo.ID, TaskRef: session.TaskRef, IssueID: 10406, IssueNumber: 406, CommentID: 200, ActorID: 7, CreatedAt: d.now(), Operation: "apply"}); err != nil {
				t.Fatal(err)
			}
			if err := d.doctorControls(ctx); err != nil {
				t.Fatal(err)
			}
			if err := d.publishDoctor(ctx); err != nil {
				t.Fatal(err)
			}
			if len(forge.posts) != 1 {
				t.Fatalf("posts=%d", len(forge.posts))
			}
			body := forge.posts[0].Body
			if !strings.Contains(body, "did not approve or perform a repair") || strings.Count(body, "litefactory logs") != 1 || !strings.Contains(body, report.TaskInspectionCommand(session.TaskRef)) || strings.Contains(body, "/doctor <instructions>") {
				t.Fatalf("unusable apply report: %s", body)
			}
			if state != "absent" {
				if !strings.Contains(body, repair.ID) || !strings.Contains(body, repair.State) {
					t.Fatalf("missing retained repair: %s", body)
				}
				after, err := d.db.ReadDoctorRepair(ctx, session.TaskRef)
				if err != nil || after.State != repair.State {
					t.Fatalf("report changed repair: %+v %v", after, err)
				}
			}
			t.Log(body)
		})
	}
}

func TestReportRecoveryLostResponseNamesRetainedExecution(t *testing.T) {
	db, _, session := recoveryDatabase(t)
	ctx := t.Context()
	segment, err := db.GetOrCreateSegment(ctx, session.ID, 1, "implement", store.AgentClaude)
	if err != nil {
		t.Fatal(err)
	}
	run, err := db.AllocateRun(ctx, store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: store.AgentClaude, LogPath: func(int) string { return "fixture.log" }})
	if err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if _, err := db.FinishRun(ctx, run.ID, store.RunOutcome{Status: store.RunSucceeded, ExitCode: &zero, SemanticOutcome: "succeeded"}); err != nil {
		t.Fatal(err)
	}
	d, logs, source := recoveryUnitFixture()
	d.db = db
	source.SessionID = session.ID
	source.RunID = run.ID
	source.Task = session.TaskRef
	source.Key = recoveryKey(session.ID, source.TriggerID)
	d.recoveryFailure(ctx, source, time.Now(), &turn.DeliveryError{Key: "retained-report", State: "uncertain", Cause: errors.New("response lost")})
	body := logs.String()
	for _, want := range []string{"Run 1", "retained succeeded outcome", "uncertain", "automatic reconciliation", "no owner action", "retained-report", session.TaskRef} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in diagnostic: %s", want, body)
		}
	}
	saved, err := db.GetRun(ctx, run.ID)
	if err != nil || !saved.Succeeded() {
		t.Fatalf("execution changed: %+v %v", saved, err)
	}
	t.Log(body)
}
