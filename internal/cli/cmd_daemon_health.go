package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhook"
)

// daemonNoDelivery is printed for a repository no valid webhook delivery has
// reached since the daemon started.
const daemonNoDelivery = "none since start"

// readHealthSnapshot reads the snapshot the daemon published, opening and
// closing the database at once like [lastTickAt]: `daemon status` must not hold
// a connection while the daemon is writing. The snapshot is display-only.
func readHealthSnapshot(ctx context.Context, paths config.Paths) (_ store.HealthSnapshot, err error) {
	if _, statErr := os.Stat(paths.DBPath()); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return store.HealthSnapshot{}, Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return store.HealthSnapshot{}, fmt.Errorf("read %s: %w", paths.DBPath(), statErr)
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return store.HealthSnapshot{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		return store.HealthSnapshot{}, err
	}
	return snap, nil
}

// printDaemonHealth writes the webhook listener line, the snapshot's age and
// one block per repository. A snapshot of a daemon that is not running is shown
// under "as of <time>, daemon not running": it is what the last daemon saw.
func printDaemonHealth(w io.Writer, snap store.HealthSnapshot, running bool, now time.Time) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if !snap.Found {
		fmt.Fprintf(tw, "webhook\tno health snapshot has been published yet\n")
		return tw.Flush()
	}
	l := snap.Listener
	switch {
	case l.ListenAddress == "":
		fmt.Fprintf(tw, "webhook listener\toff\n")
	case l.Listening:
		fmt.Fprintf(tw, "webhook listener\t%s%s (bound)\n", l.ListenAddress, webhook.Path)
	default:
		fmt.Fprintf(tw, "webhook listener\t%s%s (not bound)\n", l.ListenAddress, webhook.Path)
	}
	written := l.WrittenAt.UTC().Format(time.RFC3339)
	if running {
		fmt.Fprintf(tw, "snapshot written\t%s\n", written)
	} else {
		fmt.Fprintf(tw, "snapshot\tas of %s, daemon not running\n", written)
	}
	for _, r := range snap.Repos {
		fmt.Fprintf(tw, "repo\t%s/%s\n", r.Owner, r.Name)
		fmt.Fprintf(tw, "  mode\t%s\n", r.WebhookMode)
		fmt.Fprintf(tw, "  last delivery\t%s\n", daemonTimeOr(r.LastDeliveryAt, daemonNoDelivery))
		fmt.Fprintf(tw, "  last poll\t%s\n", daemonTimeOr(r.LastPollFinishedAt, "none since start"))
		fmt.Fprintf(tw, "  next poll\t%s\n", daemonNextPoll(r, now))
		fmt.Fprintf(tw, "  secret\t%s\n", daemonSecret(r, l.Listening))
	}
	return tw.Flush()
}

func daemonTimeOr(t time.Time, none string) string {
	if t.IsZero() {
		return none
	}
	return t.UTC().Format(time.RFC3339)
}

// daemonNextPoll renders the next listing as RFC3339 plus a relative form.
func daemonNextPoll(r store.RepoDiscoveryStatus, now time.Time) string {
	if r.NextPollDueAt.IsZero() {
		if r.PollOwed {
			return "due now (listing owed)"
		}
		return "due at the first listing"
	}
	out := fmt.Sprintf("%s (%s)", r.NextPollDueAt.UTC().Format(time.RFC3339), daemonRelative(r.NextPollDueAt.Sub(now)))
	if r.PollOwed {
		out += ", listing owed"
	}
	return out
}

// daemonRelative renders a signed distance to the minute: "in 42m", "in 1h5m",
// "42m ago", or "now" inside the minute.
func daemonRelative(d time.Duration) string {
	past := d < 0
	if past {
		d = -d
	}
	d = d.Round(time.Minute)
	if d == 0 {
		return "now"
	}
	text := d.String()
	text = strings.TrimSuffix(text, "0s")
	if strings.HasSuffix(text, "h0m") {
		text = strings.TrimSuffix(text, "0m")
	}
	if past {
		return text + " ago"
	}
	return "in " + text
}

// daemonSecret is MISSING only when the listener is on and no secret is cached.
func daemonSecret(r store.RepoDiscoveryStatus, listening bool) string {
	switch {
	case r.SecretPresent:
		return "ok"
	case listening:
		return "MISSING"
	default:
		return "not needed (listener off)"
	}
}
