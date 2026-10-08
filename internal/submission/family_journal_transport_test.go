package submission_test

import (
	"context"
	"encoding/json"
	"github.com/cloudvane/litefactory/internal/submission"
	"github.com/cloudvane/litefactory/internal/testutil"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func familyJournalBroker(t *testing.T, h submission.Handler) *submission.Broker {
	t.Helper()
	b, err := submission.Start(t.Context(), submission.Options{Directory: privateSubmissionDir(t), AllowTools: true, AuthorizedStories: []string{"US-1"}, Handler: h, MaxWorkers: 4, RequestTimeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	})
	return b
}
func familyJournals(t *testing.T, path string) []string {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(filepath.Dir(path), "request-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func TestFamilyJournalSocketIdentityAndCommunicationCompatibility(t *testing.T) {
	b := familyJournalBroker(t, successfulSubmissionHandler)
	c := submission.Client{DescriptorPath: b.DescriptorPath()}
	ids := map[string]bool{}
	for _, name := range []string{"feedback_preflight", "issues_list", "issues_get", "issues_comment", "issues_create_batch", "evidence_submit", "activity_update", "activity_get"} {
		first, err := c.Call(t.Context(), name, json.RawMessage(`{}`))
		if err != nil || first.Error != "" || first.RequestID == "" {
			t.Fatalf("%s: %+v %v", name, first, err)
		}
		// A new client instance reopens the persisted filesystem journal.
		retry, err := (submission.Client{DescriptorPath: b.DescriptorPath()}).Call(t.Context(), name, json.RawMessage(`{}`))
		if err != nil || retry.RequestID != first.RequestID {
			t.Fatalf("reopened %s: %+v %v", name, retry, err)
		}
		if ids[first.RequestID] {
			t.Fatal("family identity collision")
		}
		ids[first.RequestID] = true
	}
	other := familyJournalBroker(t, successfulSubmissionHandler)
	scoped, err := (submission.Client{DescriptorPath: other.DescriptorPath()}).Call(t.Context(), "feedback_preflight", json.RawMessage(`{}`))
	if err != nil || ids[scoped.RequestID] {
		t.Fatal("descriptor scopes collided", err)
	}
	payload := json.RawMessage(`{"story":"US-1"}`)
	legacy, err := submission.Call(t.Context(), b.DescriptorPath(), "submit", payload)
	if err != nil {
		t.Fatal(err)
	}
	modern, err := c.Call(t.Context(), "communication_submit", payload)
	if err != nil || legacy.RequestID != modern.RequestID {
		t.Fatal("communication compatibility lost", err)
	}
	before := len(familyJournals(t, b.DescriptorPath()))
	for _, name := range []string{"receipts_get", "communication_receipt"} {
		r, err := c.Call(t.Context(), name, json.RawMessage(`{"request_id":"`+modern.RequestID+`"}`))
		if err != nil || r.RequestID != modern.RequestID {
			t.Fatal("lookup correlation", err)
		}
	}
	if _, err = c.Call(t.Context(), "communication_preflight", payload); err != nil {
		t.Fatal(err)
	}
	if after := len(familyJournals(t, b.DescriptorPath())); after != before {
		t.Fatalf("lookup/preflight allocated %d journals", after-before)
	}
}

func TestFamilyJournalSocketLostReplyRetainsPretransmissionIdentity(t *testing.T) {
	entered := make(chan string, 1)
	release := make(chan struct{})
	var first atomic.Bool
	var descriptor string
	descriptorReady := make(chan struct{})
	b := familyJournalBroker(t, func(ctx context.Context, r submission.Request) (json.RawMessage, error) {
		if first.CompareAndSwap(false, true) {
			<-descriptorReady
			// Verify persistence at handler entry, before any reply exists.
			names, err := filepath.Glob(filepath.Join(filepath.Dir(descriptor), "request-*.json"))
			if err != nil || len(names) != 1 {
				return nil, os.ErrNotExist
			}
			data, err := os.ReadFile(names[0])
			if err != nil {
				return nil, err
			}
			var journal struct {
				RequestID string `json:"request_id"`
			}
			if err = json.Unmarshal(data, &journal); err != nil {
				return nil, err
			}
			if journal.RequestID != r.RequestID {
				return nil, os.ErrInvalid
			}
			entered <- r.RequestID
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return json.RawMessage(`{"disposition":"confirmed"}`), nil
	})
	descriptor = b.DescriptorPath()
	close(descriptorReady)
	c := submission.Client{DescriptorPath: descriptor}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		r submission.Response
		e error
	}
	done := make(chan result, 1)
	go func() {
		r, e := c.Call(ctx, "issues_comment", json.RawMessage(`{"issue_number":1,"message":"hello"}`))
		done <- result{r, e}
	}()
	var id string
	select {
	case id = <-entered:
	case <-time.After(testutil.Wait):
		t.Fatal("journal not present at handler entry")
	}
	cancel()
	select {
	case lost := <-done:
		if lost.e == nil || lost.r.RequestID != id {
			t.Fatalf("lost reply discarded identity: %+v", lost)
		}
	case <-time.After(testutil.Wait):
		t.Fatal("cancel did not return")
	}
	close(release)
	r, e := c.Call(t.Context(), "issues_comment", json.RawMessage(`{"issue_number":1,"message":"hello"}`))
	if e != nil || r.Error != "" || r.RequestID != id {
		t.Fatalf("lost reply retry: %+v %v", r, e)
	}
	if len(familyJournals(t, descriptor)) != 1 {
		t.Fatal("retry created another journal")
	}
}
