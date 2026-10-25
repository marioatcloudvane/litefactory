package drift

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// DiscussionReader exposes only bounded reads of explicitly selected discussions.
type DiscussionReader interface {
	ReadDriftDiscussion(context.Context, github.Repo, int, int) (github.DriftDiscussion, error)
	ReadDriftComments(context.Context, github.Repo, int, int, int, int) (github.DriftComments, error)
}

// Acquirer collects immutable local trees and successive remote observations.
// Remote discussion is neither an atomic snapshot nor an exhaustive history.
type Acquirer struct {
	Store      *store.DB
	Git        *git.Binary
	ClonePath  string
	Discussion DiscussionReader
	Repo       github.Repo
	Redactor   *redact.Redactor
}

// Coverage separates owner exclusions, source gaps and retained source counts.
type Coverage struct {
	Exclusions            []string       `json:"exclusions"`
	Gaps                  []string       `json:"gaps"`
	Enumerated            int            `json:"enumerated"`
	Retained              int            `json:"retained"`
	UnknownRemainder      bool           `json:"unknown_remainder"`
	CollectionStart       time.Time      `json:"collection_start"`
	CollectionEnd         time.Time      `json:"collection_end"`
	DiscussionTermination map[int]string `json:"discussion_termination"`
}

// Acquire suppresses unsafe bodies rather than retaining raw fallback text.
func (ac *Acquirer) Acquire(ctx context.Context, a store.DriftAnalysis, req Request) (json.RawMessage, json.RawMessage, error) {
	c := Coverage{Exclusions: []string{"uncommitted and untracked files", "intermediate commit history", "PR review threads", "linked external sources", "submodule contents and LFS downloads"}, Gaps: []string{}, CollectionStart: time.Now().UTC(), DiscussionTermination: map[int]string{}}
	for _, number := range req.Issues {
		c.DiscussionTermination[number] = "not inspected"
	}
	if len(req.Paths) > 0 {
		c.Exclusions = append(c.Exclusions, "paths outside selected literal prefixes")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(a.Limits.AcquisitionSeconds)*time.Second)
	defer cancel()
	usage := a.Usage
	charge := func(delta store.DriftUsage) error {
		next, err := ac.Store.ChargeDriftBudget(ctx, a.ID, a.Generation, delta, nil, false)
		if err == nil {
			usage = next.Usage
		}
		return err
	}
	finish := func(pin any, err error) (json.RawMessage, json.RawMessage, error) {
		if err != nil {
			c.Gaps = append(c.Gaps, "acquisition stopped before all requested material could be retained")
			c.UnknownRemainder = true
		}
		c.CollectionEnd = time.Now().UTC()
		p, _ := json.Marshal(pin)
		v, _ := json.Marshal(c)
		return p, v, err
	}
	gap := func(message string) { c.Gaps = append(c.Gaps, message) }
	sanitizeMetadata := func(e *store.DriftEvidence) {
		for _, value := range []*string{&e.Locator, &e.Path, &e.URL} {
			body, err := ac.Redactor.Apply(ctx, *value, a.Limits.ItemBytes)
			if err != nil || !utf8.ValidString(*value) {
				*value = "[source metadata suppressed]"
			} else {
				*value = body.String()
			}
		}
	}
	retain := func(e store.DriftEvidence, text string) error {
		sanitizeMetadata(&e)
		e.CollectedAt = time.Now().UTC()
		e.RedactionPolicy = "internal/redact known credentials and secret patterns v1"
		switch {
		case len(text) > a.Limits.ItemBytes:
			e.Omission = "source item exceeds byte limit"
		case !utf8.ValidString(text) || strings.ContainsRune(text, 0):
			e.Omission = "binary or invalid UTF-8 source"
		default:
			body, err := ac.Redactor.Apply(ctx, text, a.Limits.ItemBytes)
			if err != nil {
				e.Omission = "redaction failed; source suppressed"
			} else {
				e.Text = body.String()
				if body.Truncated() {
					e.Omission = "sanitization truncated source"
				}
			}
		}
		if e.Omission != "" {
			gap(e.Locator + ": " + e.Omission)
		}
		if err := ac.Store.AppendDriftEvidence(ctx, a.ID, a.Generation, e); err != nil {
			return err
		}
		c.Retained++
		return nil
	}
	if ac.Git == nil {
		return finish(nil, fmt.Errorf("drift: Git reader unavailable"))
	}
	clone, err := ac.Git.Open(ctx, ac.ClonePath)
	if err != nil {
		return finish(nil, err)
	}
	changes, err := clone.DriftChanges(ctx, req.Base, req.Head, req.Paths, a.Limits.Paths, int(a.Limits.SourceBytes))
	pin := map[string]string{"base": changes.Base, "head": changes.Head}
	if chargeErr := charge(store.DriftUsage{SourceBytes: int64(min(changes.Bytes, int(a.Limits.SourceBytes)))}); chargeErr != nil {
		return finish(pin, chargeErr)
	}
	c.UnknownRemainder = changes.UnknownRemainder
	if err != nil {
		gap("Git comparison unavailable or bounded enumeration exhausted")
		c.UnknownRemainder = true
	}
	c.Enumerated = len(changes.Changes)
	if c.UnknownRemainder {
		gap("changed path allowance reached; unknown remainder")
	}
	for i, change := range changes.Changes {
		if ctx.Err() != nil {
			gap("acquisition interrupted or timed out")
			break
		}
		if err = charge(store.DriftUsage{Paths: 1}); err != nil {
			return finish(pin, err)
		}
		for _, side := range []struct{ name, mode, oid, commit string }{{"base", change.OldMode, change.OldBlob, changes.Base}, {"head", change.NewMode, change.NewBlob, changes.Head}} {
			if side.mode == "000000" {
				continue
			}
			e := store.DriftEvidence{ID: fmt.Sprintf("code-%04d-%s", i+1, side.name), Kind: "code", Locator: side.commit + ":" + change.Path, Path: change.Path, Commit: side.commit, Blob: side.oid}
			if side.mode != "100644" && side.mode != "100755" {
				sanitizeMetadata(&e)
				e.Omission = "nonregular entry; symlinks and gitlinks are excluded"
				e.CollectedAt = time.Now().UTC()
				e.RedactionPolicy = "metadata only; no source body retained"
				gap(e.Locator + ": " + e.Omission)
				if err = ac.Store.AppendDriftEvidence(ctx, a.ID, a.Generation, e); err != nil {
					return finish(pin, err)
				}
				c.Retained++
				continue
			}
			remaining := int(a.Limits.SourceBytes - usage.SourceBytes)
			if remaining < 1 {
				gap("source byte allowance exhausted")
				c.UnknownRemainder = true
				break
			}
			text, readErr := clone.DriftBlob(ctx, side.oid, min(a.Limits.ItemBytes, remaining))
			if readErr != nil {
				if err = charge(store.DriftUsage{SourceBytes: int64(min(a.Limits.ItemBytes, remaining))}); err != nil {
					return finish(pin, err)
				}
			} else if err = charge(store.DriftUsage{SourceBytes: int64(len(text))}); err != nil {
				return finish(pin, err)
			}
			if readErr != nil {
				sanitizeMetadata(&e)
				e.Omission = "blob unavailable or exceeds source item allowance"
				e.CollectedAt = time.Now().UTC()
				e.RedactionPolicy = "metadata only; no source body retained"
				gap(e.Locator + ": " + e.Omission)
				if err = ac.Store.AppendDriftEvidence(ctx, a.ID, a.Generation, e); err != nil {
					return finish(pin, err)
				}
				c.Retained++
				continue
			}
			if err = retain(e, text); err != nil {
				return finish(pin, err)
			}
		}
	}
	selectors := append([]int(nil), req.Issues...)
	sort.Ints(selectors)
	type observedVersion struct {
		Digest    [32]byte
		UpdatedAt time.Time
		Issue     int
	}
	seen := map[int64]observedVersion{}
	for _, number := range selectors {
		c.DiscussionTermination[number] = "unfinished"
		if ac.Discussion == nil {
			gap(fmt.Sprintf("discussion #%d reader unavailable", number))
			continue
		}
		if usage.Requests >= a.Limits.Requests || usage.SourceBytes >= a.Limits.SourceBytes {
			gap("remote request or source byte allowance exhausted")
			break
		}
		if err = charge(store.DriftUsage{Requests: 1}); err != nil {
			return finish(pin, err)
		}
		item, readErr := ac.Discussion.ReadDriftDiscussion(ctx, ac.Repo, number, int(a.Limits.SourceBytes-usage.SourceBytes))
		if err = charge(store.DriftUsage{SourceBytes: int64(item.Bytes)}); err != nil {
			return finish(pin, err)
		}
		if readErr != nil {
			gap(fmt.Sprintf("discussion #%d unavailable: %s", number, safeProviderError(readErr)))
			c.DiscussionTermination[number] = "unavailable"
			continue
		}
		issue := item.Issue
		issueMetadata, _ := json.Marshal(map[string]int64{"issue_id": issue.ID})
		if err = retain(store.DriftEvidence{ID: fmt.Sprintf("issue-%d", number), Kind: "issue", Issue: number, Metadata: issueMetadata, Locator: fmt.Sprintf("%s#%d", ac.Repo, number), URL: issue.HTMLURL, CreatedAt: issue.CreatedAt, UpdatedAt: issue.UpdatedAt}, issue.Body); err != nil {
			return finish(pin, err)
		}
		for page := 1; page > 0; {
			if ctx.Err() != nil || usage.Requests >= a.Limits.Requests || usage.Comments >= a.Limits.Comments || usage.SourceBytes >= a.Limits.SourceBytes {
				gap(fmt.Sprintf("discussion #%d collection allowance exhausted", number))
				c.DiscussionTermination[number] = "limit"
				break
			}
			if err = charge(store.DriftUsage{Requests: 1}); err != nil {
				return finish(pin, err)
			}
			batch, readErr := ac.Discussion.ReadDriftComments(ctx, ac.Repo, number, page, min(100, a.Limits.Comments-usage.Comments), int(a.Limits.SourceBytes-usage.SourceBytes))
			if err = charge(store.DriftUsage{SourceBytes: int64(batch.Bytes)}); err != nil {
				return finish(pin, err)
			}
			if readErr != nil {
				gap(fmt.Sprintf("discussion #%d comments unavailable: %s", number, safeProviderError(readErr)))
				c.DiscussionTermination[number] = "unavailable"
				break
			}
			for _, comment := range batch.Comments {
				if err = charge(store.DriftUsage{Comments: 1}); err != nil {
					return finish(pin, err)
				}
				if previous, ok := seen[comment.ID]; ok {
					if previous.Issue != number {
						gap(fmt.Sprintf("comment %d observed under discussions #%d and #%d; first issue identity retained", comment.ID, previous.Issue, number))
					}
					if previous.Digest != sha256.Sum256([]byte(comment.Body)) || !previous.UpdatedAt.Equal(comment.UpdatedAt) {
						gap(fmt.Sprintf("comment %d changed during collection; first observation retained", comment.ID))
					}
					continue
				}
				seen[comment.ID] = observedVersion{sha256.Sum256([]byte(comment.Body)), comment.UpdatedAt, number}
				if err = retain(store.DriftEvidence{ID: fmt.Sprintf("comment-%d-%d", number, comment.ID), Kind: "comment", Issue: number, CommentID: comment.ID, Locator: comment.HTMLURL, URL: comment.HTMLURL, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt}, comment.Body); err != nil {
					return finish(pin, err)
				}
			}
			if batch.NextPage == 0 {
				c.DiscussionTermination[number] = "page termination observed; edits and deletions may escape detection"
				break
			}
			if batch.NextPage <= page {
				gap(fmt.Sprintf("discussion #%d repeated pagination cursor", number))
				c.DiscussionTermination[number] = "invalid pagination; uninspected remainder"
				break
			}
			page = batch.NextPage
		}
	}
	return finish(pin, nil)
}

// Provider errors can contain response bodies. Retain only safe classification
// and explicit retry timing rather than exposing unredacted upstream text.
func safeProviderError(err error) string {
	var rate *github.RateLimitError
	if errors.As(err, &rate) {
		return fmt.Sprintf("rate limited; retry after %s; reset %s; no retry performed", rate.RetryAfter, rate.Reset.UTC().Format(time.RFC3339))
	}
	var api *github.APIError
	if errors.As(err, &api) {
		return fmt.Sprintf("HTTP %d; unavailable, not evidence of absence; no retry performed", api.StatusCode)
	}
	return fmt.Sprintf("%T; no retry performed", err)
}
