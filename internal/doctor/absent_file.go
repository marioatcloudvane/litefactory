package doctor

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"syscall"

	"github.com/cloudvane/litefactory/internal/store"
)

func verifyAbsentManifest(step store.DoctorRepairStep) error {
	return verifyDoctorManifest(step, 0, false)
}

func verifyDoctorManifest(step store.DoctorRepairStep, mode uint32, existed bool) (err error) {
	path := strings.TrimSuffix(step.BackupReference, ".data") + ".manifest.json"
	file, err := openDoctorFile(path, false)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, statErr := file.Stat()
	if statErr != nil {
		return statErr
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Nlink != 1 {
		return errors.New("unsafe doctor absence manifest")
	}
	var manifest struct {
		Target, Identity, Digest, AfterDigest, Restoration string
		Mode                                               uint32
		Existed                                            bool
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return err
	}
	if len(data) > 4096 {
		return errors.New("doctor absence manifest exceeds limit")
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Existed != existed || manifest.Mode != mode || manifest.Target != step.Target || manifest.Identity != step.SourceIdentity || manifest.Digest != step.Before || manifest.AfterDigest != digestBytes([]byte(step.After)) || manifest.Restoration == "" {
		return errors.New("doctor absence manifest mismatch")
	}
	return nil
}
