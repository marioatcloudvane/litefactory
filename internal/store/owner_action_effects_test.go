package store

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestOwnerActionEffectContextFencesLateAuthority(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	if err := db.CheckOwnerActionEffect(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock := p.ConfirmNotBefore
	ctx := WithOwnerActionEffect(t.Context(), p.ExecutionID, func() time.Time { return clock })
	if err := db.CheckOwnerActionEffect(ctx); err == nil {
		t.Fatal("unclaimed execution permitted effects")
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", clock); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ClaimOwnerActionExecution(t.Context(), p.ID, clock); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckOwnerActionEffect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.ExecContext(t.Context(), `UPDATE connector_owner SET generation=generation+1`); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckOwnerActionEffect(ctx); !errors.Is(err, ErrConnectorOwner) {
		t.Fatalf("late owner authority accepted: %v", err)
	}
}

func TestOwnerActionPendingProposalFencesProviderSwitch(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	current := Conversation{Transport: "discord", ChannelRef: "200", OwnerID: "100", Name: "owner", Provider: "claude", StateDir: t.TempDir()}
	if _, err := db.EnsureConversation(t.Context(), current, p.CreatedAt); err != nil {
		t.Fatal(err)
	}
	current.Provider = "codex"
	current.StateDir = t.TempDir()
	if _, err := db.EnsureConversation(t.Context(), current, p.ConfirmNotBefore); !errors.Is(err, ErrConversationDrain) {
		t.Fatalf("pending proposal lost provider binding: %v", err)
	}
	if _, err := db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "cancel", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureConversation(t.Context(), current, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerActionDiagnosisRejectsReplacementSessionBeforeEffects(t *testing.T) {
	db, p := ownerActionStoreFixture(t)
	repo, err := db.UpsertRepo(t.Context(), Repo{Path: t.TempDir(), Forge: "github", Owner: "acme", Name: "app"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := db.GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatal(err)
	}
	p.ID, p.RequestKey, p.ExecutionID = "diagnosis", "diagnosis", "owner-action:diagnosis"
	p.RepositoryID, p.RepositoryRef, p.Scope = repo.ID, "github:acme/app", "repository:actions"
	facts, err := json.Marshal(map[string]any{"invocation": map[string]string{"command": "diagnose"}, "target_id": session.TaskRef, "target_revision": OwnerActionSessionRevision(session)})
	if err != nil {
		t.Fatal(err)
	}
	p.Facts = string(facts)
	if err = db.CreateOwnerAction(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	if _, err = db.MutateOwnerAction(t.Context(), p.Principal, p.ID, "confirm", "", p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ClaimOwnerActionExecution(t.Context(), p.ID, p.ConfirmNotBefore); err != nil {
		t.Fatal(err)
	}
	ctx := WithOwnerActionEffect(t.Context(), p.ExecutionID, func() time.Time { return p.ConfirmNotBefore })
	if err = db.CheckOwnerActionEffect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `DELETE FROM sessions WHERE id=?`, session.ID); err != nil {
		t.Fatal(err)
	}
	replacement, err := db.GetOrCreateSession(t.Context(), repo, 42)
	if err != nil {
		t.Fatalf("replacement: %+v %v", replacement, err)
	}
	if _, err = db.sql.ExecContext(t.Context(), `UPDATE sessions SET created_at=? WHERE id=?`, FormatTime(session.CreatedAt.Add(time.Second)), replacement.ID); err != nil {
		t.Fatal(err)
	}
	if err = db.CheckOwnerActionEffect(ctx); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("replacement session authorized: %v", err)
	}
	attempt := DoctorAttempt{ID: "must-not-start", SourceID: p.ExecutionID, OwnerText: "exact", PromptVersion: "v1", GuideVersion: "v1"}
	if _, err = db.BeginDoctorAttempt(ctx, session.TaskRef, attempt); !errors.Is(err, ErrDoctorFenced) {
		t.Fatalf("replacement diagnosis admitted: %v", err)
	}
}
