package naming

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/cloudvane/litefactory/internal/ticket"
)

// QualifiedTaskRef preserves the historical alias and adds a deterministic
// instance identity when that alias is already owned by another ticket.
func QualifiedTaskRef(alias string, identity ticket.Identity) string {
	return alias + "~" + QualifiedTaskSuffix(identity)
}

// QualifiedTaskSuffix gives atomic admission the same suffix as task naming.
func QualifiedTaskSuffix(identity ticket.Identity) string {
	sum := sha256.Sum256([]byte(string(identity.Provider) + "\x00" + string(identity.Origin) + "\x00" + identity.RemoteID))
	return hex.EncodeToString(sum[:8])
}

func qualifiedSuffix(taskRef string, issue int) (string, error) {
	ref, err := ParseRef(taskRef)
	if err != nil {
		return "", err
	}
	if ref.Issue != issue {
		return "", fmt.Errorf("%w: task reference issue differs from path issue", ErrInvalidRef)
	}
	return ref.Qualification, nil
}

// QualifiedWorktreePath derives distinct paths without changing frozen paths.
func QualifiedWorktreePath(root string, issue int, taskRef string) (string, error) {
	path, err := WorktreePath(root, issue)
	if err != nil {
		return "", err
	}
	suffix, err := qualifiedSuffix(taskRef, issue)
	if err != nil {
		return "", err
	}
	if suffix != "" {
		path += "-" + suffix
	}
	return path, nil
}

// QualifiedBranch keeps branch ownership separate for colliding display keys.
func QualifiedBranch(prefix string, issue int, slug, taskRef string) (string, error) {
	branch, err := Branch(prefix, issue, slug)
	if err != nil {
		return "", err
	}
	suffix, err := qualifiedSuffix(taskRef, issue)
	if err != nil {
		return "", err
	}
	if suffix != "" {
		branch += "-" + suffix
	}
	return branch, nil
}
