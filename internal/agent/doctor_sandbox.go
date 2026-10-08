package agent

import (
	"errors"
	"path/filepath"
	"strings"
)

// DoctorSandboxVersion identifies the shared diagnosis shell boundary.
const DoctorSandboxVersion = "doctor-read-only-shell-v1"

// DoctorSandbox is factory-owned policy. Only ScratchDirectory is writable;
// native file/search tools are absent. Network and optional git locks are off.
type DoctorSandbox struct {
	Version          string   `json:"version"`
	ScratchDirectory string   `json:"scratch_directory"`
	ReadableRoots    []string `json:"readable_roots"`
	DeniedReadPaths  []string `json:"denied_read_paths"`
}

// Validate refuses missing policy, noncanonical paths and credential overlap.
func (p *DoctorSandbox) Validate(workingDirectory string) error {
	if p == nil || p.Version != DoctorSandboxVersion || p.ScratchDirectory != workingDirectory || len(p.ReadableRoots) == 0 || len(p.DeniedReadPaths) == 0 {
		return errors.New("doctor read-only shell sandbox profile is missing or invalid")
	}
	for _, paths := range [][]string{{p.ScratchDirectory}, p.ReadableRoots, p.DeniedReadPaths} {
		for _, path := range paths {
			if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n*?[]") {
				return errors.New("doctor sandbox path is not canonical")
			}
		}
	}
	for _, path := range p.DeniedReadPaths {
		if path == string(filepath.Separator) || workingDirectory == path || strings.HasPrefix(workingDirectory, path+string(filepath.Separator)) {
			return errors.New("doctor scratch overlaps a credential deny path")
		}
	}
	return nil
}
