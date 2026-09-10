package config

import "path/filepath"

// SubmissionDir keeps Unix socket names short while retaining LF_HOME ownership.
// Broker creation refuses configurations exceeding the platform socket bound.
func (p Paths) SubmissionDir() string { return filepath.Join(p.home, "s") }
