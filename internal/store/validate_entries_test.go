package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var validateEntryT0 = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

func validateEntryInput(sessionID int64, commit string) ValidateEntry {
	return ValidateEntry{SessionID: sessionID, Commit: commit, Branch: "lf/1-feature", Source: ValidateSourceOwner, CommentID: 501, AuthorID: 77, Note: "check it"}
}

func TestValidateEntryIsUniquePerParentAndCommit(t *testing.T) {
	db := newTestDB(t)
	sha := strings.Repeat("a1", 20)
	first, created, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, sha), validateEntryT0)
	if err != nil || !created || first.State != ValidateEntryRequested || first.Commit != sha {
		t.Fatalf("first: %+v created=%v %v", first, created, err)
	}
	again := validateEntryInput(1, sha)
	again.CommentID = 502
	second, created, err := db.EnsureValidateEntry(t.Context(), again, validateEntryT0.Add(time.Minute))
	if err != nil || created || second.ID != first.ID || second.CommentID != 501 {
		t.Fatalf("a second start for the same key finds the first entry: %+v created=%v %v", second, created, err)
	}
	other, created, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, strings.Repeat("b2", 20)), validateEntryT0)
	if err != nil || !created || other.ID == first.ID {
		t.Fatalf("a new commit is a new entry: %+v %v", other, err)
	}
	list, err := db.ListValidateEntries(t.Context(), 1)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestValidateEntryRefusalIsRecordedAndReopened(t *testing.T) {
	db := newTestDB(t)
	sha := strings.Repeat("a1", 20)
	e, _, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, sha), validateEntryT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RefuseValidateEntry(t.Context(), e.ID, "Implement is still running.", validateEntryT0); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetValidateEntry(t.Context(), 1, sha)
	if err != nil || got.State != ValidateEntryRefused || got.Reason != "Implement is still running." || got.InstanceID != 0 {
		t.Fatalf("refused: %+v %v", got, err)
	}
	retry := validateEntryInput(1, sha)
	retry.CommentID = 600
	reopened, created, err := db.EnsureValidateEntry(t.Context(), retry, validateEntryT0.Add(time.Hour))
	if err != nil || !created || reopened.State != ValidateEntryRequested || reopened.Reason != "" || reopened.CommentID != 600 {
		t.Fatalf("a later start reopens a refused entry: %+v %v", reopened, err)
	}
}

func TestValidateEntryAdmissionNamesOneInstanceForGood(t *testing.T) {
	db := newTestDB(t)
	sha := strings.Repeat("a1", 20)
	e, _, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, sha), validateEntryT0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitValidateEntry(t.Context(), e.ID, 9, validateEntryT0); err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitValidateEntry(t.Context(), e.ID, 9, validateEntryT0); err != nil {
		t.Fatalf("repeating the same admission is safe: %v", err)
	}
	if err := db.AdmitValidateEntry(t.Context(), e.ID, 10, validateEntryT0); err == nil {
		t.Fatal("an admitted entry never moves to another instance")
	}
	if err := db.RefuseValidateEntry(t.Context(), e.ID, "late", validateEntryT0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an admitted entry is never refused: %v", err)
	}
	got, _, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, sha), validateEntryT0)
	if err != nil || got.State != ValidateEntryAdmitted || got.InstanceID != 9 {
		t.Fatalf("a later start finds the admitted entry: %+v %v", got, err)
	}
}

func TestValidateEntryRejectsBadInput(t *testing.T) {
	db := newTestDB(t)
	for name, in := range map[string]ValidateEntry{
		"short commit":          validateEntryInput(1, "abc"),
		"no session":            validateEntryInput(0, strings.Repeat("a1", 20)),
		"no branch":             {SessionID: 1, Commit: strings.Repeat("a1", 20), Source: ValidateSourceAuto},
		"owner without comment": {SessionID: 1, Commit: strings.Repeat("a1", 20), Branch: "b", Source: ValidateSourceOwner},
	} {
		if _, _, err := db.EnsureValidateEntry(t.Context(), in, validateEntryT0); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := db.GetValidateEntry(t.Context(), 1, strings.Repeat("c3", 20)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing entry: %v", err)
	}
}

func TestValidateEntryReopenOnlyAfterTheInstanceEndedWithoutFinishing(t *testing.T) {
	db := newTestDB(t)
	sha := strings.Repeat("a1", 20)
	e, _, err := db.EnsureValidateEntry(t.Context(), validateEntryInput(1, sha), validateEntryT0)
	if err != nil {
		t.Fatal(err)
	}
	def, err := db.CreateGraphDefinition(t.Context(), GraphDefinition{Team: "builtin", Mode: "validate", ProfileVersion: 1, Path: "validate.dot", Digest: strings.Repeat("ab", 32)})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := db.GetOrCreateSession(t.Context(), mustRepo(t, db), 1)
	if err != nil {
		t.Fatal(err)
	}
	inst, err := db.CreateGraphInstance(t.Context(), GraphInstance{SessionID: sess.ID, DefinitionID: def.ID, CurrentNode: "suite", StartedAt: validateEntryT0})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AdmitValidateEntry(t.Context(), e.ID, inst.ID, validateEntryT0); err != nil {
		t.Fatal(err)
	}
	owner := validateEntryInput(1, sha)
	owner.ID, owner.CommentID = e.ID, 700
	got, err := db.ReopenValidateEntry(t.Context(), owner, validateEntryT0)
	if err != nil || got.State != ValidateEntryAdmitted {
		t.Fatalf("a running instance keeps its entry: %+v %v", got, err)
	}
	if err := db.SetGraphInstanceStatus(t.Context(), inst.ID, GraphFailed, time.Time{}, validateEntryT0); err != nil {
		t.Fatal(err)
	}
	got, err = db.ReopenValidateEntry(t.Context(), owner, validateEntryT0)
	if err != nil || got.State != ValidateEntryRequested || got.InstanceID != 0 || got.CommentID != 700 {
		t.Fatalf("a failed instance frees the entry for an explicit owner start: %+v %v", got, err)
	}
	auto := owner
	auto.Source = ValidateSourceAuto
	if _, err := db.ReopenValidateEntry(t.Context(), auto, validateEntryT0); err == nil {
		t.Fatal("the factory's own start never reopens an entry")
	}
}

func mustRepo(t *testing.T, db *DB) Repo {
	t.Helper()
	repo, err := db.UpsertRepo(t.Context(), Repo{Path: t.TempDir(), Forge: ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return repo
}
