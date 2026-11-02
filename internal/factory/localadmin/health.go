package localadmin

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"strings"
	"time"
)

// HealthSnapshot is the local operator's display-only daemon observation.
// It deliberately contains no database rows or live persistence capability.
type HealthSnapshot struct {
	Found    bool
	Listener ListenerHealth
	Repos    []RepositoryHealth
}

// ListenerHealth contains only facts rendered by local daemon status.
type ListenerHealth struct {
	ListenAddress          string
	Listening              bool
	WrittenAt              time.Time
	LastHeartbeatStartedAt time.Time
	DiscoveryPaused        bool
	PauseReason            string
	PausedSince            time.Time
	ExpectedResumeAt       time.Time
	RunsInFlight           int
	ChatStatus             string
	RunsHeld               string
}

// RepositoryHealth contains the discovery and webhook facts for one project.
type RepositoryHealth struct {
	Owner, Name                                       string
	LastDeliveryAt, LastPollFinishedAt, NextPollDueAt time.Time
	PollOwed, SecretPresent                           bool
	WebhookMode                                       string
}

// HealthInspection is the saved health plus configuration coverage diagnostics.
type HealthInspection struct {
	HealthSnapshot
	UnsupportedDoctor   []string
	MissingModeMappings []string
}

// InspectHealth releases the database before its display-only result is rendered.
func InspectHealth(ctx context.Context, paths config.Paths) (_ HealthInspection, err error) {
	db, err := store.Open(ctx, paths.DBPath())
	if err != nil {
		return HealthInspection{}, err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	snap, err := db.ReadHealthSnapshot(ctx)
	if err != nil {
		return HealthInspection{}, err
	}
	repos, err := db.ListRepos(ctx)
	if err != nil {
		return HealthInspection{}, err
	}
	listener := snap.Listener
	health := HealthInspection{HealthSnapshot: HealthSnapshot{Found: snap.Found, Listener: ListenerHealth{
		ListenAddress: listener.ListenAddress, Listening: listener.Listening, WrittenAt: listener.WrittenAt,
		LastHeartbeatStartedAt: listener.LastHeartbeatStartedAt, DiscoveryPaused: listener.DiscoveryPaused,
		PauseReason: listener.PauseReason, PausedSince: listener.PausedSince, ExpectedResumeAt: listener.ExpectedResumeAt,
		RunsInFlight: listener.RunsInFlight, ChatStatus: listener.ChatStatus, RunsHeld: listener.RunsHeld,
	}}}
	for _, project := range snap.Repos {
		health.Repos = append(health.Repos, RepositoryHealth{Owner: project.Owner, Name: project.Name,
			LastDeliveryAt: project.LastDeliveryAt, LastPollFinishedAt: project.LastPollFinishedAt,
			NextPollDueAt: project.NextPollDueAt, PollOwed: project.PollOwed,
			SecretPresent: project.SecretPresent, WebhookMode: project.WebhookMode})
	}
	for _, repo := range repos {
		connection, err := db.CurrentConnection(ctx, repo.ID, store.TicketConnection)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return HealthInspection{}, err
		}
		if connection.Enabled && connection.Provider == ticket.Linear {
			health.UnsupportedDoctor = append(health.UnsupportedDoctor, repo.Slug())
			labels, err := connection.Linear.ModeLabels()
			if err != nil {
				return HealthInspection{}, err
			}
			if missing := linear.MissingModeLabels(labels); len(missing) > 0 {
				health.MissingModeMappings = append(health.MissingModeMappings, fmt.Sprintf("project %d (%s) connection %d (%s): missing mode mappings: %s; add label IDs to --mode-labels, or use a mapped mode", repo.ID, repo.Slug(), connection.ID, connection.Destination, strings.Join(missing, ", ")))
			}
		}
	}
	return health, nil
}
