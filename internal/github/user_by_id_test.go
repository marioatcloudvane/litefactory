package github_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
)

func TestUserByIDRetainsImmutableIdentity(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, `{"id":42,"login":"renamed","type":"User"}`))
	user, err := f.Client.UserByID(t.Context(), 42)
	if err != nil || user.ID != 42 || user.Login != "renamed" || !user.IsHuman {
		t.Fatalf("lookup: %+v %v", user, err)
	}
	if got := f.Only(t).Path; got != "/user/42" {
		t.Fatalf("path %q", got)
	}
}

func TestUserByIDRejectsIdentityMismatch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, respondJSON(http.StatusOK, `{"id":43,"login":"different","type":"User"}`))
	if _, err := f.Client.UserByID(t.Context(), 42); !errors.Is(err, github.ErrUnexpectedStatus) {
		t.Fatalf("mismatch accepted: %v", err)
	}
}
