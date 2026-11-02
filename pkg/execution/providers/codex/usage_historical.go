package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/pkg/execution/agent"
)

// HistoricalWindow is an original finalized execution's exclusive time interval.
// The caller obtains it and the root identity from retained execution evidence, never filenames.
type HistoricalWindow struct{ From, To time.Time }

// HistoricalUsage contains only neutral facts. Raw transcripts never leave the
// provider reader. Sources are retained privately for a second safe read before
// apply; that read never holds a database transaction.
type HistoricalUsage struct {
	Invocations  []agent.UsageInvocation
	Observations []agent.UsageObservation
	Diagnostics  []string
	sources      []historicalSource
	home         string
}
type historicalSource struct {
	path, digest string
	info         fs.FileInfo
}

// CollectHistoricalUsage reads a bounded inventory once, using the live parser.
// It recovers descendants only: existing root totals are deliberately untouched.
func CollectHistoricalUsage(ctx context.Context, home, rootID string, window HistoricalWindow) (result HistoricalUsage, err error) {
	if !filepath.IsAbs(home) || !validUsageID(rootID) || window.From.IsZero() || !window.From.Before(window.To) {
		return result, errors.New("invalid historical usage scope")
	}
	result.home = home
	capture := agent.UsageCapture{Sink: func(_ context.Context, e agent.UsageEvent) error {
		if e.Invocation != nil {
			result.Invocations = append(result.Invocations, *e.Invocation)
		}
		if e.Observation != nil && e.Observation.InvocationID != rootID {
			result.Observations = append(result.Observations, e.Observation.Clone())
		}
		return nil
	}}
	c := newUsageCaptureCollector(ctx, &capture)
	c.prepareRollouts([]string{"HOME=" + home})
	if c.rollouts == nil || c.rollouts.disabled || c.rollouts.root == nil {
		return result, errors.New("historical source inventory unavailable or incomplete")
	}
	defer func() { err = errors.Join(err, c.rollouts.root.Close()) }()
	c.rollouts.baseline = make(map[string]rolloutBaseline)
	c.thread, c.window = rootID, &window
	c.invocation(rootID, "")
	c.collectRollouts(true)
	rootFound := false
	for _, file := range c.rollouts.files {
		if !file.associated {
			continue
		}
		if file.id == rootID {
			rootFound = true
		}
		if file.digest == nil || file.offset != file.info.Size() {
			c.diagnostic("historical_source_incomplete")
			continue
		}
		result.sources = append(result.sources, historicalSource{path: file.path, digest: hex.EncodeToString(file.digest.Sum(nil)), info: file.info})
	}
	if !rootFound {
		c.diagnostic("historical_root_missing")
	}
	for _, o := range result.Observations {
		if o.ActualModel == nil {
			c.diagnostic("model_attribution_unavailable")
		}
	}
	// A missing child or unsupported envelope is not a measured zero.
	if len(result.Observations) == 0 {
		c.diagnostic("historical_descendant_usage_missing")
	}
	result.Diagnostics = slices.Clone(c.coverage.Diagnostics)
	slices.SortFunc(result.Invocations, func(a, b agent.UsageInvocation) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(result.Observations, func(a, b agent.UsageObservation) int {
		return strings.Compare(a.SourceID+"\x00"+a.EventKey, b.SourceID+"\x00"+b.EventKey)
	})
	if err := result.Revalidate(ctx); err != nil {
		return result, err
	}
	return result, nil
}

// Revalidate refuses replaced, modified, incomplete and symlinked sources.
func (h HistoricalUsage) Revalidate(ctx context.Context) (err error) {
	root, err := os.OpenRoot(h.home)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	for _, source := range h.sources {
		path := filepath.Join(".codex/sessions", source.path)
		parts := strings.Split(path, string(filepath.Separator))
		for i := range parts {
			info, e := root.Lstat(filepath.Join(parts[:i+1]...))
			if e != nil {
				return e
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return errors.New("historical source symlink refused")
			}
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		f, e := root.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		if e != nil {
			return e
		}
		e = func() error {
			info, e := f.Stat()
			if e != nil {
				return e
			}
			if !info.Mode().IsRegular() || !os.SameFile(info, source.info) || info.Size() != source.info.Size() {
				return errors.New("historical source replaced")
			}
			digest := sha256.New()
			n, e := io.Copy(digest, io.LimitReader(f, rolloutBytes+1))
			if e != nil {
				return e
			}
			if n > rolloutBytes || hex.EncodeToString(digest.Sum(nil)) != source.digest {
				return errors.New("historical source changed")
			}
			return ctx.Err()
		}()
		if e = errors.Join(e, f.Close()); e != nil {
			return fmt.Errorf("revalidate historical evidence: %w", e)
		}
	}
	return nil
}
