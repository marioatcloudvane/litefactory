package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/cloudvane/litefactory/internal/doctor"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

func approveEffect(t *testing.T, db *store.DB, session store.Session, step store.DoctorRepairStep, evidence store.DoctorEvidence) store.DoctorRepair {
	t.Helper()
	ctx := t.Context()
	conversation, e := db.BeginDoctorAttempt(ctx, session.TaskRef, store.DoctorAttempt{ID: "effects", SourceID: "effect-source", OwnerText: "fixture", PromptVersion: doctor.PromptVersion, GuideVersion: doctor.GuideVersion})
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(evidence)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: "effects", Provider: "claude", ConfiguredModel: "fixture", Evidence: string(encoded)}); e != nil {
		t.Fatal(e)
	}
	binding, e := db.BindDoctorResultAttempt(ctx, "effects")
	if e != nil {
		t.Fatal(e)
	}
	diagnosis := doctor.Diagnosis{Findings: []doctor.Finding{{Explanation: "repair", Evidence: []string{step.Target}, Uncertainty: "fixture"}}, Proposal: []doctor.RepairStep{{Number: 1, Target: step.Target, Precondition: "exact", Change: "repair", Reason: "owner approved", Reversibility: "backup", Rollback: "restore", Action: &store.DoctorRepairAction{Kind: step.Kind, Before: step.Before, After: step.After}}}}
	snapshot, e := json.Marshal(diagnosis)
	if e != nil {
		t.Fatal(e)
	}
	manifest, e := json.Marshal(diagnosis.Proposal)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.RecordResult(ctx, store.ResultRecord{Binding: binding, RequestID: "effects-result", Payload: snapshot, Snapshot: snapshot, Diagnostics: []byte(`{"valid":true,"errors":[]}`), Retired: []byte(`[]`), Accepted: true}); e != nil {
		t.Fatal(e)
	}
	if e = db.AdoptDoctorResult(ctx, binding, "effects-result", manifest); e != nil {
		t.Fatal(e)
	}
	proposal, _, e := db.DoctorProposalReference(ctx, conversation.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.PresentDoctorProposal(ctx, session.TaskRef, "effects-presentation", proposal); e != nil {
		t.Fatal(e)
	}
	op, e := db.ReserveDoctorRepair(ctx, store.DoctorApproval{ID: "effects-repair", SourceID: "owner-apply", Principal: "local-owner", TaskRef: session.TaskRef, PresentationID: "effects-presentation"})
	if e != nil {
		t.Fatal(e)
	}
	if e = db.AcknowledgeDoctorExclusion(ctx, op.ID, "factory"); e != nil {
		t.Fatal(e)
	}
	return op
}

func TestFactoryDoctorConsumeConfirmedAndUncertain(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		t.Run(map[bool]string{false: "confirmed", true: "lost-reply"}[uncertain], func(t *testing.T) {
			d, forge, repo := doctorDaemonFixture(t)
			ctx := t.Context()
			session, e := d.db.GetOrCreateSession(ctx, repo, 406)
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = d.db.BootstrapWatermark(ctx, session.ID, 10); e != nil {
				t.Fatal(e)
			}
			if _, e = d.db.RecordComment(ctx, store.Comment{SessionID: session.ID, ForgeCommentID: 11, AuthorHandle: "owner", AuthorUserID: 7, Body: "repair", CreatedAt: d.now(), Classification: store.ClassificationInstruction}); e != nil {
				t.Fatal(e)
			}
			ref, e := naming.ParseRef(session.TaskRef)
			if e != nil {
				t.Fatal(e)
			}
			evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
			if e != nil {
				t.Fatal(e)
			}
			step := store.DoctorRepairStep{Number: 1, Kind: "consume_comment", Target: "comment:11", Before: "10", After: "11"}
			op := approveEffect(t, d.db, session, step, evidence)
			if uncertain {
				forge.postErr = errors.New("reply lost")
			}
			adapter := FactoryDoctorRepairEffects{DB: d.db, Comments: forge, BotID: 99, Redactor: d.doctor.service.Redactor}
			result, e := adapter.Execute(ctx, ref, op, step, "claude")
			if uncertain {
				if e == nil {
					t.Fatal("lost receipt accepted")
				}
				wm, _, _ := d.db.GetWatermark(ctx, session.ID)
				if wm.LastCommentID != 10 {
					t.Fatal("unconfirmed reply consumed")
				}
				_, _ = adapter.Execute(ctx, ref, op, step, "claude")
				if len(forge.posts) != 1 {
					t.Fatal("uncertain reply replayed")
				}
			} else {
				if e != nil || result.State != "verified" || result.BackupReference == "" {
					t.Fatal(result, e)
				}
				if e = adapter.Verify(ctx, ref, result); e != nil {
					t.Fatal(e)
				}
			}
		})
	}
}

func TestFactoryDoctorBranchAdapterRecordsAndRechecks(t *testing.T) {
	d, _, _ := doctorDaemonFixture(t)
	ctx := t.Context()
	fixture := gittest.New(t)
	repo, e := d.db.UpsertRepo(ctx, store.Repo{Path: fixture.Dir, Forge: "github", Owner: "fixture", Name: "repo", RemoteURL: fixture.Origin, BaseBranch: gittest.DefaultBranch, FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	session, e := d.db.GetOrCreateSession(ctx, repo, 539)
	if e != nil {
		t.Fatal(e)
	}
	session, e = d.db.FreezeSessionSlug(ctx, session.ID, "sync", "lf/539-sync")
	if e != nil {
		t.Fatal(e)
	}
	binary, e := git.Resolve(ctx)
	if e != nil {
		t.Fatal(e)
	}
	clone, e := binary.Open(ctx, fixture.Dir)
	if e != nil {
		t.Fatal(e)
	}
	tree, e := git.EnsureWorktree(ctx, clone, git.EnsureWorktreeOptions{Dir: filepath.Join(repo.WorktreeRoot, "task-539"), Branch: session.Branch, BaseBranch: repo.BaseBranch})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.db.SetSessionWorktree(ctx, session.ID, tree.Worktree.Path()); e != nil {
		t.Fatal(e)
	}
	head, e := tree.Worktree.RevParse(ctx, "HEAD", git.RevParseOptions{})
	if e != nil {
		t.Fatal(e)
	}
	ref, e := naming.ParseRef(session.TaskRef)
	if e != nil {
		t.Fatal(e)
	}
	evidence, e := d.db.ReadDoctorEvidence(ctx, ref)
	if e != nil {
		t.Fatal(e)
	}
	evidence.Records = append(evidence.Records, store.DoctorEvidenceRecord{Kind: "branch", ID: session.Branch, State: head, Identity: head})
	step := store.DoctorRepairStep{Number: 1, Kind: "sync_branch", Target: "branch:" + session.Branch, Before: head, After: head}
	op := approveEffect(t, d.db, session, step, evidence)
	adapter := FactoryDoctorRepairEffects{DB: d.db, Git: binary, CloneGate: newCloneGate(), Author: git.Author{Name: "Fixture", Email: "fixture@example.invalid"}}
	for _, bad := range []store.DoctorRepairStep{
		{Number: 99, Kind: step.Kind, Target: step.Target, Before: step.Before, After: step.After},
		{Number: 1, Kind: "consume_comment", Target: step.Target, Before: step.Before, After: step.After},
		{Number: 1, Kind: step.Kind, Target: step.Target, Before: "drift", After: step.After},
	} {
		if _, e := adapter.Execute(ctx, ref, op, bad, "claude"); e == nil {
			t.Fatal("unapproved manifest accepted", bad)
		}
	}
	result, e := adapter.Execute(ctx, ref, op, step, "claude")
	if e != nil {
		t.Fatal(e)
	}
	if result.State != "verified" || result.BackupReference != "git:head:"+head {
		t.Fatal(result)
	}
	if e = adapter.Verify(ctx, ref, result); e != nil {
		t.Fatal(e)
	}
	if _, e = adapter.Execute(context.Background(), ref, op, step, "claude"); e == nil {
		t.Fatal("verified external effect replay accepted")
	}
}
