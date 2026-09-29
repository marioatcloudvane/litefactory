package config

import "path/filepath"

// DoctorControlDir owns the private instance-authenticated local control socket.
func (p Paths) DoctorControlDir() string { return filepath.Join(p.home, "dc") }

// DoctorHostRegistryPath points to the currently held daemon instance.
func (p Paths) DoctorHostRegistryPath() string {
	return filepath.Join(p.DoctorControlDir(), "host.json")
}

// DoctorDispatchDir retains one-dispatch markers independently of live sockets.
func (p Paths) DoctorDispatchDir() string { return filepath.Join(p.home, "doctor", "dispatch") }
