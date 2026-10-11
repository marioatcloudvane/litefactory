package meterview

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

type readerFixture struct {
	repos     []store.Repo
	snapshots int
	err       error
}

func (f *readerFixture) ListRepos(context.Context) ([]store.Repo, error) { return f.repos, nil }
func (f *readerFixture) ListMeterPrices(context.Context) (store.MeterCatalog, error) {
	return store.MeterCatalog{}, nil
}
func (f *readerFixture) NewMeterSnapshot(context.Context, store.MeterFilter, store.MeterSnapshotOptions) (*store.MeterSnapshot, error) {
	f.snapshots++
	return nil, f.err
}

func TestReadRejectsScopeBeforeSnapshot(t *testing.T) {
	for _, tc := range []struct {
		request Request
		repos   []store.Repo
		kind    ErrorKind
	}{
		{Request{Task: 1}, nil, TaskWithoutProject},
		{Request{Project: "owner/repo"}, nil, UnknownProject},
		{Request{Project: "owner/repo"}, []store.Repo{{ID: 1, Owner: "owner", Name: "repo", Forge: "github"}, {ID: 2, Owner: "owner", Name: "repo", Forge: "other"}}, AmbiguousProject},
		{Request{Mode: "invalid"}, nil, InvalidFilter},
	} {
		f := &readerFixture{repos: tc.repos}
		_, err := Read(t.Context(), f, tc.request)
		var typed *Error
		if !errors.As(err, &typed) || typed.Kind != tc.kind || f.snapshots != 0 {
			t.Fatalf("error %v snapshots %d", err, f.snapshots)
		}
	}
}
func TestValidateTimeBounds(t *testing.T) {
	f, err := Validate(Request{From: "2026-01-01", To: "2026-01-02T01:00:00+01:00"}, true)
	if err != nil || f.TimeFrom == nil || f.TimeTo == nil || !f.TimeTo.Equal(f.TimeFrom.Add(24*time.Hour)) {
		t.Fatalf("filter %#v error %v", f, err)
	}
	for _, r := range []Request{{From: "bad"}, {From: "2026-01-02", To: "2026-01-01"}, {From: "2026-01-01", To: "2026-01-01"}, {Model: "bad\nmodel"}, {Agent: "other"}} {
		if _, err := Validate(r, true); err == nil {
			t.Fatalf("accepted %#v", r)
		}
	}
	if _, err := Validate(Request{From: "2026-01-01"}, false); err == nil {
		t.Fatal("CLI dates changed")
	}
}
func TestSnapshotLimitsAreTyped(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, errors.New("store: meter snapshot exceeds limits; narrow scope")} {
		_, got := Read(t.Context(), &readerFixture{err: err}, Request{})
		var typed *Error
		if !errors.As(got, &typed) || typed.Kind != TooLarge {
			t.Fatalf("error %v", got)
		}
	}
}
