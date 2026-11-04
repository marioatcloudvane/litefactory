package loganalysis

import (
	"testing"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/linear"
)

func TestTicketTrackerSelectsLinearThenGitHub(t *testing.T) {
	lc, gc := &linear.Client{}, &github.Client{}
	got, ok := TicketTracker(lc, gc, "acme/widgets")
	if lt, isLinear := got.(LinearTracker); !ok || !isLinear || lt.Client != lc {
		t.Fatalf("linear connection = %#v, %v", got, ok)
	}
	got, ok = TicketTracker(nil, gc, "acme/widgets")
	gt, isGitHub := got.(GitHubTracker)
	if !ok || !isGitHub || gt.Client != gc || gt.Repo != (github.Repo{Owner: "acme", Name: "widgets"}) {
		t.Fatalf("github connection = %#v, %v", got, ok)
	}
	if got, ok = TicketTracker(nil, nil, "acme/widgets"); ok || got != nil {
		t.Fatalf("no client = %#v, %v", got, ok)
	}
}
