package drift

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

var errAnalysisBusy = errors.New("drift analysis is active")

// ownAnalysis holds a kernel lease for one persisted identity. The stable file
// is never removed: replacing a locked inode would permit a competing owner.
// Locks live in the system temporary directory, outside factory runtime state.
func (s *Service) ownAnalysis(ctx context.Context, id string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	database, err := filepath.EvalSymlinks(s.Store.Path())
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(os.TempDir())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	name := "litefactory-drift-" + digest([]byte(database+"\x00"+id)) + ".lock"
	f, err := root.OpenFile(name, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(fmt.Errorf("invalid drift ownership file"), err, f.Close())
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			err = errAnalysisBusy
		}
		return nil, errors.Join(err, f.Close())
	}
	return func() error { return errors.Join(syscall.Flock(int(f.Fd()), syscall.LOCK_UN), f.Close()) }, nil
}

func (s *Service) reconcile(ctx context.Context, aID string) (err error) {
	release, err := s.ownAnalysis(ctx, aID)
	if errors.Is(err, errAnalysisBusy) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	a, err := s.Store.GetDriftAnalysis(ctx, aID)
	if err != nil {
		return err
	}
	if a.Execution == "running" {
		_, err = s.Store.InterruptDriftAnalysis(ctx, a.ID, a.Generation, "analysis interrupted before an accepted report; no automatic relaunch")
	}
	return err
}
