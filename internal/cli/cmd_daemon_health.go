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
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/webhook"
)

// daemonNoDelivery is printed for a repository no valid webhook delivery has
// reached since the daemon started.
const daemonNoDelivery = "none since start"

type daemonHealth struct {
	store.HealthSnapshot
	unsupportedDoctor   []store.Repo
	missingModeMappings []string
}

// readHealthSnapshot reads the snapshot the daemon published, opening and
// closing the database at once like [lastTickTime]: `daemon status` must not hold
// a connection while the daemon is writing. The snapshot is display-only.
func readHealthSnapshot(ctx context.Context, paths config.Paths) (_ daemonHealth, err error) {
	if _, statErr := os.Stat(paths.DBPath()); statErr != nil {
		if errors.Is(statErr, fs.ErrNotExist) {
			return daemonHealth{}, Preconditionf("no litefactory database at %s: run `litefactory init` first", paths.DBPath())
		}
		return daemonHealth{}, fmt.Errorf("read %s: %w", paths.DBPath(), statErr)
	}
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return daemonHealth{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		return daemonHealth{}, err
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return daemonHealth{}, err
	}
	health := daemonHealth{HealthSnapshot: snap}
	for _, repo := range repos {
		connection, err := db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return daemonHealth{}, err
		}
		if connection.Enabled && connection.Provider == ticket.Linear {
			health.unsupportedDoctor = append(health.unsupportedDoctor, repo)
			labels, err := connection.Linear.ModeLabels()
			if err != nil {
				return daemonHealth{}, err
			}
			if missing := linear.MissingModeLabels(labels); len(missing) > 0 {
				health.missingModeMappings = append(health.missingModeMappings, fmt.Sprintf("project %d (%s) connection %d (%s): missing mode mappings: %s; add label IDs to --mode-labels, or use a mapped mode", repo.ID, repo.Slug(), connection.ID, connection.Destination, strings.Join(missing, ", ")))
			}
		}
	}
	return health, nil
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
	if l.ChatStatus != "" {
		fmt.Fprintf(tw, "chat\t%s\n", l.ChatStatus)
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

// printDaemonHeartbeat writes what the daemon was doing, each fact with its age:
// the last heartbeat, the last one that completed, whether discovery is running
// or paused and why, and how many runs were in flight. A daemon that is not
// running is shown under "as of <time>, daemon not running": these are the
// last things it recorded, not things it is doing now.
func printDaemonHeartbeat(w io.Writer, snap store.HealthSnapshot, lastCompleted time.Time, running bool, now time.Time) error {
	if !snap.Found {
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	l := snap.Listener
	if !running {
		fmt.Fprintf(tw, "heartbeat\tas of %s, daemon not running\n", l.WrittenAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(tw, "last heartbeat\t%s\n", daemonTimeAge(l.LastHeartbeatStartedAt, "none since start", now))
	fmt.Fprintf(tw, "last completed heartbeat\t%s\n", daemonTimeAge(lastCompleted, "none yet", now))
	if l.DiscoveryPaused {
		resume := "when fresh rate-limit data arrives"
		if !l.ExpectedResumeAt.IsZero() {
			resume = l.ExpectedResumeAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(tw, "discovery\tpaused since %s: %s, expected to resume %s\n",
			daemonTimeAge(l.PausedSince, "an unknown time", now), l.PauseReason, resume)
	} else {
		fmt.Fprintf(tw, "discovery\trunning\n")
		if l.PauseReason != "" {
			fmt.Fprintf(tw, "connection pauses\t%s\n", l.PauseReason)
		}
	}
	if l.RunsHeld != "" {
		fmt.Fprintf(tw, "runs held\t%s\n", l.RunsHeld)
	}
	fmt.Fprintf(tw, "runs in flight\t%d (as of %s)\n", l.RunsInFlight, daemonAge(now.Sub(l.WrittenAt)))
	return tw.Flush()
}

// daemonTimeAge renders a time as RFC3339 with its age: "2026-01-02T03:04:05Z (12s ago)".
func daemonTimeAge(t time.Time, none string, now time.Time) string {
	if t.IsZero() {
		return none
	}
	return fmt.Sprintf("%s (%s)", t.UTC().Format(time.RFC3339), daemonAge(now.Sub(t)))
}

// daemonAge renders how long ago something was, to the second below a minute
// and to the minute above it.
func daemonAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	return daemonRelative(-d)
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
		return fmt.Sprintf("MISSING; repair with `litefactory repo webhook %s/%s`", r.Owner, r.Name)
	default:
		return "not needed (listener off)"
	}
}

// This is current configuration, independent of the daemon's recorded snapshot.
func printDoctorConfiguration(w io.Writer, repos []store.Repo) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, repo := range repos {
		fmt.Fprintf(tw, "project %s\tbackground Doctor discovery unsupported/skipped for Linear (current configuration)\n", repo.Slug())
	}
	return tw.Flush()
}
