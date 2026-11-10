package codex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
)

// DoctorIsolation stays closed pending the owner's authentic isolation decision.
// Mapping a read-only shell profile is not certification of the installed binary.
func (r *Runner) DoctorIsolation(context.Context) (string, error) {
	return "", agent.ErrDoctorIsolationUnverified
}

// linkDoctorAuth symlinks the operator's Codex auth.json into the private
// home so subscription logins authenticate. A link, not a copy, keeps token
// refreshes on the operator's file. A missing source is skipped.
func (r *Runner) linkDoctorAuth(home string) error {
	var target string
	if codexHome := envValue(r.environ, "CODEX_HOME"); codexHome != "" {
		target = filepath.Join(codexHome, "auth.json")
	} else if userHome := envValue(r.environ, "HOME"); userHome != "" {
		target = filepath.Join(userHome, ".codex", "auth.json")
	} else {
		return nil
	}
	switch _, err := os.Stat(target); {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("stat codex auth: %w", err)
	}
	if err := os.Symlink(target, filepath.Join(home, "auth.json")); err != nil {
		return fmt.Errorf("link codex auth: %w", err)
	}
	return nil
}

func boundedDoctorDetail(detail string) string {
	const limit = 512
	detail = strings.TrimSpace(detail)
	if len(detail) > limit {
		detail = strings.ToValidUTF8(detail[:limit], "") + "…"
	}
	return detail
}
