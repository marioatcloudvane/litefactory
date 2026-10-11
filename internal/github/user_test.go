package github_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

// TestAuthenticatedUserResolvesTheBotIdentity is AC-0.5. `gh` is confined to
// owner discovery in internal/identity — it is authenticated as Mario, so
// asking it about the bot would resolve the wrong account and make the
// bot != owner check pass wrongly. The bot identity comes from the token, over
// the API, here.
func TestAuthenticatedUserResolvesTheBotIdentity(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK,
		`{"id": 4242, "login": "litefactory-bot", "type": "Bot", "name": "litefactory"}`))

	user, err := f.Client.AuthenticatedUser(t.Context())
	if err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if user.ID != 4242 {
		t.Errorf("ID = %d, want 4242 — identities.user_id is what the self-filter compares", user.ID)
	}
	if user.Login != "litefactory-bot" {
		t.Errorf("Login = %q, want litefactory-bot", user.Login)
	}
	if !user.IsBot {
		t.Error("IsBot = false for an account GitHub typed as Bot")
	}
	if got, want := f.Only(t).Path, "/user"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

// TestAuthenticatedUserOnAMachineAccountIsNotTypedAsABot is the whole reason
// D-I exists. A fine-grained PAT on a machine *user* account reports
// type "User", so the forge's bot flag never fires — if the self-filter trusted
// it, the daemon would answer its own comments in a loop. The numeric id is
// what has to be right here; IsBot is one signal among three.
func TestAuthenticatedUserOnAMachineAccountIsNotTypedAsABot(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusOK,
		`{"id": 777, "login": "acme-machine-user", "type": "User"}`))

	user, err := f.Client.AuthenticatedUser(t.Context())
	if err != nil {
		t.Fatalf("AuthenticatedUser: %v", err)
	}
	if user.IsBot {
		t.Error("IsBot = true for an account GitHub typed as User")
	}
	if !user.IsHuman {
		t.Error("explicit User account type was not retained for authenticated control verification")
	}
	if user.ID != 777 {
		t.Fatalf("ID = %d, want 777 — the id is the identity when the bot flag cannot be trusted", user.ID)
	}
	if user.Login != "acme-machine-user" {
		t.Errorf("Login = %q", user.Login)
	}
}

// TestAuthenticatedUserRefusesAnIdentityWithoutAnID keeps a zero from being
// stored as identities.user_id, where it would compare equal to the next
// missing id and make two different accounts look like the same one.
func TestAuthenticatedUserRefusesAnIdentityWithoutAnID(t *testing.T) {
	t.Parallel()

	responses := map[string]string{
		"no id":            `{"login": "litefactory-bot", "type": "Bot"}`,
		"no login":         `{"id": 4242, "type": "Bot"}`,
		"an empty object":  `{}`,
		"a null id":        `{"id": null, "login": "litefactory-bot"}`,
		"an id of literal": `{"id": 0, "login": "litefactory-bot"}`,
	}

	for name, payload := range responses {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, respondJSON(http.StatusOK, payload))
			user, err := f.Client.AuthenticatedUser(t.Context())
			if err == nil {
				t.Fatalf("AuthenticatedUser returned %+v, want an error", user)
			}
			if !errors.Is(err, github.ErrUnexpectedStatus) {
				t.Errorf("errors.Is(%v, ErrUnexpectedStatus) = false", err)
			}
			if user != (github.User{}) {
				t.Errorf("user = %+v, want the zero value alongside an error", user)
			}
		})
	}
}

// TestAuthenticatedUserSurfacesARejectedCredential covers the message someone
// reads when `init` fails: the token is missing a scope, revoked, or simply
// wrong, and none of those is worth a retry.
func TestAuthenticatedUserSurfacesARejectedCredential(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusUnauthorized, `{"message": "Bad credentials"}`))

	_, err := f.Client.AuthenticatedUser(t.Context())
	if !errors.Is(err, github.ErrUnauthorized) {
		t.Fatalf("AuthenticatedUser error = %v, want ErrUnauthorized", err)
	}
	assertNoToken(t, "the rejected-credential error", err.Error())
}
