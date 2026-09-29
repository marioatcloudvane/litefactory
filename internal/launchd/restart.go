package launchd

import (
	"context"
	"errors"
	"strings"
)

// Restart restarts the already installed service without rewriting its plist.
// Callers own durable dispatch identity: a lost reply must not cause a retry.
func (m *Manager) Restart(ctx context.Context) error {
	label := m.label()
	if label == "" || strings.ContainsAny(label, "/\\\x00\n\r\t ") {
		return errors.New("invalid installed service identity")
	}
	_, err := m.run(ctx, "kickstart", "-k", m.ServiceTarget())
	return err
}
