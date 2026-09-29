package daemon

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
)

// DoctorHostRestart dispatches only a durable claimed token. Live targets use
// authenticated control; a down target must still hold its acquired singleton.
type DoctorHostRestart struct {
	DB          *store.DB
	Paths       config.Paths
	Live        *DoctorLocalControl
	Down        *DoctorRepairEnvironment
	Replacement DoctorReplacement
}

// Dispatch performs one durable claimed recovery handoff without blind retry.
func (r *DoctorHostRestart) Dispatch(ctx context.Context, intent store.DoctorRestart) error {
	saved, err := readDoctorHandoff(ctx, r.DB, intent)
	if err != nil {
		return err
	}
	if strings.HasPrefix(saved.OldInstance, "down:") {
		if r.Down == nil || r.Replacement == nil {
			return errors.New("doctor down recovery route unavailable")
		}
		r.Down.mu.Lock()
		defer r.Down.mu.Unlock()
		if r.Down.operation != saved.RepairID || r.Down.lock == nil || r.Down.lock.file == nil {
			return errors.New("doctor down recovery lost singleton exclusion")
		}
		if err = claimDoctorHostDispatch(r.Paths, saved); err != nil {
			return err
		}
		if err = r.Replacement.StartDoctorReplacement(ctx, saved); err != nil {
			return err
		}
		// The replacement waits on this very lock; release only after successful
		// handoff start. On an uncertain Start response retain exclusion for review.
		err = r.Down.lock.Close()
		r.Down.lock = nil
		r.Down.operation = ""
		return err
	}
	if r.Live == nil {
		return errors.New("doctor live restart control unavailable")
	}
	_, err = r.Live.call(ctx, "doctor_host_restart", doctorHostRequest{Instance: saved.OldInstance, Operation: saved.RepairID, Token: saved.Token})
	return err
}

// Observe asks the authenticated replacement singleton for exact token proof.
func (r *DoctorHostRestart) Observe(ctx context.Context, intent store.DoctorRestart) (string, bool, error) {
	saved, err := readDoctorHandoff(ctx, r.DB, intent)
	if err != nil {
		return "", false, err
	}
	control := r.Live
	if control == nil {
		control = &DoctorLocalControl{Paths: r.Paths}
	}
	observed, err := control.call(ctx, "doctor_host_observe", doctorHostRequest{Operation: saved.RepairID, Token: saved.Token})
	if err != nil {
		return "", false, err
	}
	return observed.Instance, observed.Confirmed && observed.Instance != saved.OldInstance, nil
}

// WaitDoctorRecoveryLock is used only by the same-binary handoff entry point.
// It acquires actual singleton ownership before any startup reconciliation and
// rechecks the exact durable token after waiting. No live lock is stolen.
func WaitDoctorRecoveryLock(ctx context.Context, db *store.DB, paths config.Paths, intent store.DoctorRestart) (*Lock, error) {
	if _, err := readDoctorHandoff(ctx, db, intent); err != nil {
		return nil, err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		lock, err := AcquireLock(paths.LockPath())
		if err == nil {
			if _, err = readDoctorHandoff(ctx, db, intent); err != nil {
				return nil, errors.Join(err, lock.Close())
			}
			return lock, nil
		}
		var busy *AlreadyRunningError
		if !errors.As(err, &busy) {
			return nil, err
		}
		select {
		case <-bounded.Done():
			return nil, bounded.Err()
		case <-ticker.C:
		}
	}
}

// SameBinaryDoctorReplacement pins the factory executable bytes at construction.
// Arguments and environment are factory-owned; no PID, command or credential
// comes from agent text. New daemon credentials resolve through normal startup.
type SameBinaryDoctorReplacement struct {
	executable  string
	digest      [32]byte
	paths       config.Paths
	environment []string
}

// NewSameBinaryDoctorReplacement pins a factory-owned executable for later handoff.
func NewSameBinaryDoctorReplacement(executable string, paths config.Paths, environment []string) (*SameBinaryDoctorReplacement, error) {
	canonical, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(canonical) {
		return nil, errors.New("doctor replacement executable must be absolute")
	}
	digest, err := doctorExecutableDigest(canonical)
	if err != nil {
		return nil, err
	}
	env := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		key, _, ok := strings.Cut(entry, "=")
		// This is another factory process, not an agent. Preserve its trusted
		// environment so configured token_env references can resolve at startup.
		// Only LF_HOME is rebound to the exact installation being recovered.
		if ok && key != config.EnvHome {
			env = append(env, entry)
		}
	}
	env = append(env, config.EnvHome+"="+paths.Home())
	return &SameBinaryDoctorReplacement{executable: canonical, digest: digest, paths: paths, environment: env}, nil
}
func doctorExecutableDigest(path string) ([32]byte, error) {
	var out [32]byte
	before, err := os.Lstat(path)
	if err != nil {
		return out, err
	}
	if !before.Mode().IsRegular() || before.Mode().Perm()&0111 == 0 || before.Size() > 512<<20 {
		return out, errors.New("doctor replacement executable is unsafe")
	}
	//nolint:gosec // Factory executable path resolved and checked above.
	file, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !os.SameFile(before, info) {
		return out, errors.New("doctor replacement executable changed")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, io.LimitReader(file, (512<<20)+1)); err != nil {
		return out, err
	}
	after, err := file.Stat()
	if err != nil || info.Size() != after.Size() || !info.ModTime().Equal(after.ModTime()) {
		return out, errors.New("doctor replacement executable changed during read")
	}
	copy(out[:], hash.Sum(nil))
	return out, nil
}

// StartDoctorReplacement starts the pinned recovery entry point once requested.
func (r *SameBinaryDoctorReplacement) StartDoctorReplacement(ctx context.Context, intent store.DoctorRestart) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	digest, err := doctorExecutableDigest(r.executable)
	if err != nil {
		return err
	}
	if digest != r.digest {
		return errors.New("doctor replacement binary differs from pinned factory executable")
	}
	if intent.RepairID == "" || intent.Token == "" {
		return errors.New("doctor replacement handoff identity missing")
	}
	// Deliberately not CommandContext: the recovery process must survive shutdown
	// of this verified old daemon. The child bounds its own singleton wait.
	//nolint:gosec,noctx // Factory-pinned executable; recovery must outlive the cancelled old daemon.
	command := exec.Command(r.executable, "daemon", "run", "--doctor-repair", intent.RepairID, "--doctor-token", intent.Token)
	command.Env = r.environment
	command.Dir = r.paths.Home()
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer null.Close()
	command.Stdin = null
	command.Stdout = null
	command.Stderr = null
	if err = command.Start(); err != nil {
		return errors.New("doctor replacement start failed; retain uncertain dispatch")
	}
	return command.Process.Release()
}

// NewDoctorHostCapabilities returns local factory capabilities for CLI wiring.
// Missing/stale socket discovery does not prove down: Exclude must acquire the
// actual singleton, otherwise it refuses. Close exclusion after handoff or stop.
func NewDoctorHostCapabilities(db *store.DB, paths config.Paths, replacement DoctorReplacement) (*DoctorRepairEnvironment, *DoctorHostRestart) {
	control := &DoctorLocalControl{Paths: paths}
	environment := &DoctorRepairEnvironment{DB: db, Paths: paths, Replacement: replacement}
	if _, err := readDoctorHostRegistry(paths); err == nil {
		environment.Live = control
	}
	route := &DoctorHostRestart{DB: db, Paths: paths, Live: control, Down: environment, Replacement: replacement}
	return environment, route
}
