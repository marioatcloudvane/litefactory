package doctor

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

func TestDoctorStopJoinsAdmittedFileEffectAcrossStoreHandles(t *testing.T) {
	s, ref, id, path := fileRepairFixture(t, true)
	ctx := t.Context()
	tools := &RepairTools{service: s, target: ref, operation: id}
	if err := tools.Backup(ctx, 1); err != nil {
		t.Fatal(err)
	}
	before, err := s.DB.GetSessionByTaskRef(ctx, ref.String())
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Open(ctx, s.DB.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other := &Service{DB: second, Paths: s.Paths, Config: s.Config, Redactor: s.Redactor}
	unlock, err := s.repairEffectLock(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_ = unlock()
		}
	}()
	// Hold the same real advisory lock used by an admitted mutation. A separate
	// service/connection cannot acknowledge Stop while that effect is unsettled.
	bounded, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	err = other.Stop(bounded, ref)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stop bypassed admitted effect: %v", err)
	}
	step, err := tools.step(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- other.Stop(ctx, ref) }()
	// Exercise the actual effect implementation under its already acquired lock.
	if err = s.writeRepairFile(ctx, ref, id, step); err != nil {
		t.Fatal(err)
	}
	if err = unlock(); err != nil {
		t.Fatal(err)
	}
	released = true
	select {
	case err = <-stopped:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop failed to join admitted effect")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != step.After {
		t.Fatalf("admitted effect lost: %q %v", data, err)
	}
	if err = tools.Write(ctx, 1); err == nil {
		t.Fatal("write authority survived Stop")
	}
	held, err := second.DoctorRepairHeld(ctx, ref.String())
	if err != nil || !held {
		t.Fatalf("partial repair released: %v %v", held, err)
	}
	steps, err := second.DoctorRepairSteps(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(steps[0].BackupReference)
	if err != nil || string(backup) != "original issue notes\n" {
		t.Fatalf("backup lost: %q %v", backup, err)
	}
	after, err := second.GetSessionByTaskRef(ctx, ref.String())
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("normal session changed: before=%+v after=%+v err=%v", before, after, err)
	}
}

func TestDoctorAbsentFileCrashReopenPreservesUncertainHoldAndBackup(t *testing.T) {
	for _, created := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-write", true: "third-party-create"}[created], func(t *testing.T) {
			s, ref, id, path := worktreeRepairFixture(t, true)
			ctx := t.Context()
			tools := &RepairTools{service: s, target: ref, operation: id}
			if err := tools.Backup(ctx, 1); err != nil {
				t.Fatal(err)
			}
			if err := s.DB.BeginDoctorFileWrite(ctx, id, 1); err != nil {
				t.Fatal(err)
			}
			if created {
				if err := os.WriteFile(path, []byte("unapproved creator bytes"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			dbPath := s.DB.Path()
			if err := s.DB.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := store.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			resumed := &Service{DB: reopened, Paths: s.Paths, Config: s.Config, Redactor: s.Redactor}
			operation, err := resumed.ReconcileRepair(ctx, ref)
			if err != nil || operation.State != "needs_review" || !operation.Fenced {
				t.Fatalf("reconciliation=%+v %v", operation, err)
			}
			held, err := reopened.DoctorRepairHeld(ctx, ref.String())
			if err != nil || !held {
				t.Fatalf("uncertain hold=%v %v", held, err)
			}
			steps, err := reopened.DoctorRepairSteps(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if steps[0].State != "uncertain" {
				t.Fatalf("uncertain write promoted: %+v", steps[0])
			}
			backup, err := os.ReadFile(steps[0].BackupReference)
			if err != nil || len(backup) != 0 {
				t.Fatalf("absent original backup lost: %q %v", backup, err)
			}
			current, err := os.ReadFile(path)
			if created {
				if err != nil || string(current) != "unapproved creator bytes" {
					t.Fatalf("recovery overwrote foreign bytes: %q %v", current, err)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("recovery replayed absent-file creation: %q %v", current, err)
			}
			late := &RepairTools{service: resumed, target: ref, operation: id}
			if err = late.Write(ctx, 1); err == nil {
				t.Fatal("reopened repair retained write authority")
			}
			if _, err = reopened.ReadDoctorRestart(ctx, id); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("uncertain repair fabricated restart: %v", err)
			}
		})
	}
}
