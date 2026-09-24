package report

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// CreationAcceptance carries durable historical admission values, never mutable
// approval text or a new deadline. Publication is descriptive context only.
type CreationAcceptance struct {
	Hash, Relationship   string
	CreatedAt, ExpiresAt time.Time
	Publication          CreationPublication
}

func (a *CreationAcceptance) valid() bool {
	if a == nil {
		return false
	}
	hash, err := hex.DecodeString(a.Hash)
	return err == nil && len(hash) == 32 && strings.ToLower(a.Hash) == a.Hash &&
		(a.Relationship == "linked" || a.Relationship == "attach") &&
		!a.CreatedAt.IsZero() && !a.ExpiresAt.IsZero() && a.ExpiresAt.Equal(a.CreatedAt.Add(30*time.Minute)) &&
		a.CreatedAt.Year() >= 1 && a.ExpiresAt.Year() <= 9999
}

func (a CreationAcceptance) body() string {
	return fmt.Sprintf("Creation approval was accepted and recorded for proposal `%s` with relationship `%s`. Approval alone started no publication.\n\nThe original approval-comment timestamp was **%s**; the stored original expiry was **%s**. An eligible publishing attempt must bind before that deadline. This is a historical acknowledgment and does not establish that the approval is still usable or the proposal is still current.\n\n%s", a.Hash, a.Relationship, a.CreatedAt.UTC().Format(time.RFC3339Nano), a.ExpiresAt.UTC().Format(time.RFC3339Nano), CreationGuidance(a.Publication))
}
