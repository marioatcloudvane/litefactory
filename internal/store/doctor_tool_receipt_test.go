package store

import (
	"bytes"
	"fmt"
	"testing"
)

func TestDoctorToolReceiptSharesLedgerWithoutAdoptingResult(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()
	session := testSession(t, db, 406)
	if _, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt("tool-attempt")); err != nil {
		t.Fatal(err)
	}
	if err := db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: "tool-attempt", Provider: "claude", ConfiguredModel: "model", Evidence: `{"task_ref":"github:acme/api#406"}`}); err != nil {
		t.Fatal(err)
	}
	binding, err := db.BindDoctorResultAttempt(ctx, "tool-attempt")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"name":"doctor_write","arguments":{"step":1}}`)
	reply := []byte(`{"operation_id":"repair","step":1,"state":"verified"}`)
	record := ResultRecord{Binding: binding, RequestID: "tool-request", Payload: payload, Diagnostics: reply, Accepted: true, DoctorToolReceipt: true}
	receipt, err := db.RecordResult(ctx, record)
	if err != nil || receipt.AppliedRevision != 0 {
		t.Fatal(receipt, err)
	}
	var revision, count int
	if err = db.sql.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=?`, binding.ResultID).Scan(&revision); err != nil || revision != 0 {
		t.Fatal("tool adopted application revision", revision, err)
	}
	if err = db.sql.QueryRowContext(ctx, `SELECT count(*) FROM application_result_revisions WHERE result_id=?`, binding.ResultID).Scan(&count); err != nil || count != 0 {
		t.Fatal("tool created result history", count, err)
	}
	if err = db.StopDoctorConversation(ctx, session.TaskRef); err != nil {
		t.Fatal(err)
	}
	replay, err := db.ReplayResultRequest(ctx, binding, "tool-request", payload)
	if err != nil || replay.ID != receipt.ID || !bytes.Equal(replay.Diagnostics, reply) {
		t.Fatal("lost reply not recoverable after fence", replay, err)
	}
	if _, err = db.ReplayResultRequest(ctx, binding, "tool-request", []byte(`{"name":"doctor_write","arguments":{"step":2}}`)); err == nil {
		t.Fatal("correlation rebound")
	}
}

func TestDoctorProposalKeepReplaceWithdraw(t *testing.T) {
	for _, recovered := range []bool{false, true} {
		t.Run(fmt.Sprint(recovered), func(t *testing.T) {
			db := newTestDB(t)
			ctx := t.Context()
			session := testSession(t, db, 406)
			_, presentation := retainedPollProposal(t, db, session)
			var cid int64
			if err := db.sql.QueryRowContext(ctx, `SELECT id FROM doctor_conversations WHERE task_ref=?`, session.TaskRef).Scan(&cid); err != nil {
				t.Fatal(err)
			}
			original, err := db.DoctorCurrentProposal(ctx, cid)
			if err != nil {
				t.Fatal(err)
			}
			for i, tc := range []struct {
				snapshot string
				proposal []byte
				want     []byte
				valid    bool
			}{
				{`{"proposal":[]}`, nil, original, true},
				{`{"withdraw_proposal":true,"proposal":` + string(original) + `}`, original, original, false},
				{`{"proposal":` + string(original) + `}`, original, original, true},
				{`{"withdraw_proposal":true,"proposal":[]}`, nil, nil, true},
			} {
				attempt := fmt.Sprintf("followup-%d", i)
				if _, err := db.BeginDoctorAttempt(ctx, session.TaskRef, doctorIOAttempt(attempt)); err != nil {
					t.Fatal(err)
				}
				if err := db.PrepareDoctorAttempt(ctx, DoctorAttempt{ID: attempt, Provider: "claude", ConfiguredModel: "test", Evidence: `{}`}); err != nil {
					t.Fatal(err)
				}
				b, err := db.BindDoctorResultAttempt(ctx, attempt)
				if err != nil {
					t.Fatal(err)
				}
				var revision int64
				if err := db.sql.QueryRowContext(ctx, `SELECT revision FROM application_results WHERE id=?`, b.ResultID).Scan(&revision); err != nil {
					t.Fatal(err)
				}
				record := resultRecord(b, attempt, revision)
				record.Snapshot = []byte(tc.snapshot)
				if _, err := db.RecordResult(ctx, record); err != nil {
					t.Fatal(err)
				}
				if recovered {
					err = db.AdoptRecoveredDoctorResult(ctx, b, attempt, tc.proposal)
				} else {
					err = db.AdoptDoctorResult(ctx, b, attempt, tc.proposal)
				}
				if (err == nil) != tc.valid {
					t.Fatalf("adoption %d: %v", i, err)
				}
				if !tc.valid {
					// A rejected adoption leaves the attempt active; end it before the next fixture.
					if err := db.FailDoctorAttempt(ctx, attempt, "failed", "invalid withdrawal"); err != nil {
						t.Fatal(err)
					}
				}
				got, err := db.DoctorCurrentProposal(ctx, cid)
				if err != nil || !bytes.Equal(got, tc.want) {
					t.Fatalf("proposal %s, want %s: %v", got, tc.want, err)
				}
				if tc.valid {
					if err := db.AdoptDoctorResult(ctx, b, attempt, tc.proposal); err == nil {
						t.Fatal("adoption replay must be fenced")
					}
				}
			}
			var history int
			if err := db.sql.QueryRowContext(ctx, `SELECT count(*) FROM doctor_proposals WHERE conversation_id=?`, cid).Scan(&history); err != nil || history != 2 {
				t.Fatalf("history=%d: %v", history, err)
			}
			if _, err := db.ReserveDoctorRepair(ctx, DoctorApproval{ID: "withdrawn", SourceID: "owner", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: presentation}); err == nil {
				t.Fatal("withdrawn proposal remained applicable")
			}
		})
	}
}
