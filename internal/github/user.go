package github

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v90/github"
)

// User is a forge account: the author of a comment, or the identity behind the
// bot token.
//
// ID is the identity. Login is a label that GitHub lets people change, and
// IsBot is a flag that a fine-grained PAT on a machine *user* account never
// sets — so the self-filter and the instruction/third-party classification
// compare ID and nothing else (D-I, AC-2.14, AC-2.15).
type User struct {
	// ID is the numeric account id, stable for the life of the account.
	ID int64
	// Login is the handle, for display.
	Login string
	// IsBot is GitHub's own account type flag ("Bot"). It is one signal among
	// three in the self-filter, never the only one.
	IsBot bool
}

// AuthenticatedUser resolves the identity behind the configured token
// (GET /user).
//
// This is how the *bot* handle and numeric id are discovered (AC-0.5). The `gh`
// CLI is confined to owner discovery in internal/identity and is deliberately
// not an API transport here: `gh` is authenticated as Mario, and asking it
// about the bot would resolve the wrong account — the exact confusion the
// bot != owner check exists to catch.
func (c *Client) AuthenticatedUser(ctx context.Context) (User, error) {
	// An empty login is how the SDK addresses GET /user — the account behind
	// the credential — rather than GET /users/{login}, which is a public
	// lookup of somebody else and would answer even for a revoked token.
	payload, _, err := do(ctx, c, func(ctx context.Context) (*gogithub.User, *gogithub.Response, error) {
		return c.api.Users.Get(ctx, "")
	})
	if err != nil {
		return User{}, fmt.Errorf("github: resolve authenticated user: %w", err)
	}
	user := convertUser(payload)
	if user.ID == 0 || user.Login == "" {
		return User{}, fmt.Errorf("github: resolve authenticated user: %w: response carried no id or login",
			ErrUnexpectedStatus)
	}
	return user, nil
}

// convertUser narrows the SDK's account to the three fields M0 reads.
//
// It tolerates the absent author GitHub reports for a deleted account: a nil
// payload is the zero User, whose id is 0 and therefore matches nobody.
func convertUser(payload *gogithub.User) User {
	if payload == nil {
		return User{}
	}
	return User{
		ID:    payload.GetID(),
		Login: payload.GetLogin(),
		IsBot: payload.GetType() == "Bot",
	}
}
