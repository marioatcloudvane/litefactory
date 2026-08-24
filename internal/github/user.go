package github

import (
	"context"
	"fmt"
	"net/http"
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
	var payload userPayload
	if _, err := c.do(ctx, http.MethodGet, c.endpoint("/user", nil), nil, &payload); err != nil {
		return User{}, fmt.Errorf("github: resolve authenticated user: %w", err)
	}
	user := payload.user()
	if user.ID == 0 || user.Login == "" {
		return User{}, fmt.Errorf("github: resolve authenticated user: %w: response carried no id or login",
			ErrUnexpectedStatus)
	}
	return user, nil
}

// userPayload is GitHub's account shape, as it appears standalone on /user and
// nested under "user" on issues and comments.
type userPayload struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Type  string `json:"type"`
}

// user converts the payload, tolerating the absent author GitHub reports for a
// deleted account.
func (p *userPayload) user() User {
	if p == nil {
		return User{}
	}
	return User{ID: p.ID, Login: p.Login, IsBot: p.Type == "Bot"}
}
