package turn

import (
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

func TestDeliveryNoticeUsesSavedPublisherAfterRestart(t *testing.T) {
	f, forge := featureDeliveryFixture(t, false)
	connection := func(role store.ConnectionRole) store.ConnectionVersion {
		t.Helper()
		version, err := f.db.CurrentConnection(t.Context(), f.storeRepo.ID, role)
		if errors.Is(err, store.ErrNotFound) {
			version, err = f.db.SetConnection(t.Context(), store.ConnectionVersion{RepoID: f.storeRepo.ID, Role: role, Provider: ticket.GitHub, Origin: "https://api.github.com", Destination: f.storeRepo.Slug(), CredentialSource: "env", CredentialEnv: "TOKEN", Enabled: true, Provenance: "explicit", ClonePath: f.storeRepo.Path, RemoteURL: f.storeRepo.RemoteURL, BaseBranch: f.storeRepo.BaseBranch, FeaturePrefix: f.storeRepo.FeaturePrefix, WorktreeRoot: f.storeRepo.WorktreeRoot}, 0)
		}
		mustProgress(t, err)
		return version
	}
	code, tickets := connection(store.CodeConnection), connection(store.TicketConnection)
	ref := ticket.Ref{Identity: ticket.Identity{Provider: tickets.Provider, Origin: tickets.Origin, RemoteID: "1230"}, Locator: ticket.Locator{Provider: tickets.Provider, Origin: tickets.Origin, Container: tickets.Destination, Key: strconv.Itoa(testIssue)}}
	binding, err := f.db.AddTicketBinding(t.Context(), store.TicketBinding{RepoID: f.storeRepo.ID, ConnectionVersionID: tickets.ID, Ref: ref, Enabled: true, Provenance: "explicit"})
	mustProgress(t, err)
	mustProgress(t, f.db.AttachExecutionSnapshot(t.Context(), store.ExecutionSnapshot{SessionID: f.session.ID, RepoID: f.storeRepo.ID, BindingID: binding.ID, CodeVersionID: code.ID, TicketVersionID: tickets.ID, ActorID: 9, ActorLogin: "original-publisher"}))
	restarted, err := NewStoryProgress(f.db, forge, f.opts.StoryProgress.renderer, 999, filepath.Join(t.TempDir(), "restarted-locks"))
	mustProgress(t, err)
	codePublisher, err := restarted.deliveryPublisherID(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if codePublisher != 9 {
		t.Fatalf("original code publisher changed after restart: %d", codePublisher)
	}
	mustProgress(t, f.db.PinExecutionCodePublisher(t.Context(), f.session.ID, 88, "separate-code-publisher"))
	codePublisher, err = restarted.deliveryPublisherID(t.Context(), f.session.TaskRef)
	mustProgress(t, err)
	if codePublisher != 88 {
		t.Fatalf("explicit code publisher lost: %d", codePublisher)
	}
	mustProgress(t, restarted.deliveryNotice(t.Context(), f.session.TaskRef, "proposal", "waiting", "Delivery remains pending."))
	first := len(forge.comments[testIssue])
	mustProgress(t, restarted.deliveryNotice(t.Context(), f.session.TaskRef, "proposal", "waiting", "Delivery remains pending."))
	if first == 0 || len(forge.comments[testIssue]) != first {
		t.Fatalf("saved publisher receipt was not recovered: %d -> %d", first, len(forge.comments[testIssue]))
	}
	if restarted.botID != 999 {
		t.Fatal("saved publisher mutated shared startup identity")
	}
}
