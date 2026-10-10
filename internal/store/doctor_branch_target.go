package store

import (
	"encoding/hex"
	"strings"
)

// DoctorBranchRepairTarget accepts exact commit inputs and one named task branch.
func DoctorBranchRepairTarget(target, before, after string) bool {
	if !strings.HasPrefix(target, "branch:") || len(target) <= len("branch:") {
		return false
	}
	for _, sha := range []string{before, after} {
		if len(sha) != 40 && len(sha) != 64 {
			return false
		}
		if strings.ToLower(sha) != sha {
			return false
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return false
		}
	}
	return true
}
