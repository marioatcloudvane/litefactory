package projectread

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

type blockingRepositoryStream struct {
	entered, closed     chan struct{}
	readOnce, closeOnce sync.Once
}

func (s *blockingRepositoryStream) Read([]byte) (int, error) {
	s.readOnce.Do(func() { close(s.entered) })
	<-s.closed
	return 0, io.ErrClosedPipe
}
func (s *blockingRepositoryStream) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func TestRepositoryCancellationReleasesPrivatePreparation(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "reader.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	registered, err := db.UpsertRepo(ctx, store.Repo{Path: t.TempDir(), Forge: store.ForgeGitHub, Owner: "acme", Name: "app", RemoteURL: "git@github.com:acme/app.git", BaseBranch: "main", FeaturePrefix: "lf", WorktreeRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	spill, err := os.CreateTemp(t.TempDir(), "private-source-")
	if err != nil {
		t.Fatal(err)
	}
	name := spill.Name()
	stream := &blockingRepositoryStream{entered: make(chan struct{}), closed: make(chan struct{})}
	redactor := redact.New(redact.Options{})
	views := NewViews()
	t.Cleanup(func() { _ = views.Close() })
	const token = "unfinished-source"
	const commit = "1111111111111111111111111111111111111111"
	views.entries[token] = &repoView{project: registered.ID, commit: commit, path: "file.txt", policy: redactor.PolicyID(), stream: stream, file: spill, hash: sha256.New()}
	reader := Reader{DB: db, Redactor: redactor, Views: views}
	callCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type response struct {
		body json.RawMessage
		err  error
	}
	result := make(chan response, 1)
	go func() {
		body, _, err := reader.Call(callCtx, Project{ID: registered.ID, Name: "acme/app", Commit: commit}, "repo_read", Request{Continuation: token})
		result <- response{body, err}
	}()
	select {
	case <-stream.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("source read did not start")
	}
	cancel()
	select {
	case out := <-result:
		if out.err != nil {
			t.Fatal(out.err)
		}
		var decoded struct {
			Status string `json:"status"`
		}
		if err = json.Unmarshal(out.body, &decoded); err != nil {
			t.Fatal(err)
		}
		if decoded.Status != "unavailable" {
			t.Fatalf("cancelled preparation: %s", out.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled source read remained blocked")
	}
	if _, err = os.Stat(name); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("private source remains: %v", err)
	}
	views.mu.Lock()
	remaining := len(views.entries)
	views.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("cancelled preparation retained %d entries", remaining)
	}
	select {
	case <-stream.closed:
	default:
		t.Fatal("source stream was not closed")
	}
}
