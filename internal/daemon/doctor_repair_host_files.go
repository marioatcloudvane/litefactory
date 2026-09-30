package daemon

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func doctorPrivateRoot(path string) (*os.Root, error) {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	if canonical != path {
		return nil, errors.New("doctor host directory has unsafe aliases")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || int(stat.Uid) != os.Geteuid() {
		return nil, errors.New("doctor host directory is not owner-private")
	}
	return os.OpenRoot(path)
}
func readDoctorHostRegistry(paths config.Paths) (doctorHostRegistry, error) {
	var out doctorHostRegistry
	root, err := doctorPrivateRoot(paths.DoctorControlDir())
	if err != nil {
		return out, err
	}
	defer root.Close()
	before, err := root.Lstat("host.json")
	if err != nil {
		return out, err
	}
	stat, ok := before.Sys().(*syscall.Stat_t)
	if !ok || !before.Mode().IsRegular() || before.Mode().Perm()&0077 != 0 || int(stat.Uid) != os.Geteuid() || before.Size() > 4096 {
		return out, errors.New("doctor host registry is unsafe")
	}
	file, err := root.Open("host.json")
	if err != nil {
		return out, err
	}
	defer file.Close()
	current, err := file.Stat()
	if err != nil || !os.SameFile(before, current) {
		return out, errors.New("doctor host registry changed")
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil || len(data) > 4096 {
		return out, errors.New("doctor host registry exceeds bound")
	}
	if err := submission.DecodeJSON(data, &out); err != nil {
		return out, err
	}
	if out.Instance == "" || len(out.Instance) > 256 || !validDoctorDescriptor(paths, out.Descriptor) {
		return out, errors.New("doctor host registry identity refused")
	}
	return out, nil
}
func writeDoctorHostRegistry(paths config.Paths, registry doctorHostRegistry) (err error) {
	root, err := doctorPrivateRoot(paths.DoctorControlDir())
	if err != nil {
		return err
	}
	defer root.Close()
	var random [16]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	name := "host-" + hex.EncodeToString(random[:]) + ".tmp"
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = root.Remove(name) }()
	data, err := json.Marshal(registry)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = errors.Join(err, file.Close())
	if err != nil {
		return err
	}
	if err = root.Rename(name, "host.json"); err != nil {
		return err
	}
	return syncDoctorDirectory(paths.DoctorControlDir())
}
func syncDoctorDirectory(path string) error {
	//nolint:gosec // Private factory-owned directory from config.Paths.
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

// The dispatch marker is never removed or reused. Even a failed Start is an
// uncertain external effect and cannot authorize a second process launch.
func claimDoctorHostDispatch(paths config.Paths, intent store.DoctorRestart) error {
	if err := os.MkdirAll(paths.DoctorDispatchDir(), 0700); err != nil {
		return err
	}
	root, err := doctorPrivateRoot(paths.DoctorDispatchDir())
	if err != nil {
		return err
	}
	defer root.Close()
	digest := sha256.Sum256([]byte(intent.RepairID))
	file, err := root.OpenFile(hex.EncodeToString(digest[:])+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("doctor restart dispatch is already retained or unavailable; observe without retry")
	}
	data, encodeErr := json.Marshal(intent)
	if encodeErr == nil {
		_, encodeErr = file.Write(data)
	}
	if encodeErr == nil {
		encodeErr = file.Sync()
	}
	if err = errors.Join(encodeErr, file.Close()); err != nil {
		return err
	}
	return syncDoctorDirectory(paths.DoctorDispatchDir())
}
