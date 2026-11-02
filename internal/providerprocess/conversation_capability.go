package providerprocess

import (
	"errors"
	"os"
	"path/filepath"
)

// PrepareConversationDirectory rejects symlinks before provider-owned state is used.
func PrepareConversationDirectory(state string) (home, scratch string, err error) {
	unlock, err := LockConversation(state)
	if err != nil {
		return "", "", err
	}
	defer unlock()
	for _, name := range []string{"home", "scratch"} {
		path := filepath.Join(state, name)
		if e := os.MkdirAll(path, 0700); e != nil {
			return "", "", e
		}
		info, e := os.Lstat(path)
		if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", "", errors.New("invalid private provider directory")
		}
		//nolint:gosec // Private directories require owner traversal; no group or other access.
		if e = os.Chmod(path, 0700); e != nil {
			return "", "", e
		}
	}
	return filepath.Join(state, "home"), filepath.Join(state, "scratch"), nil
}
