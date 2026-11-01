package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/taskdiscussion"
	"github.com/cloudvane/litefactory/internal/turn"
)

func (d *Daemon) prepareTaskDiscussion(ctx context.Context, judge *turn.JudgeService, reporter *turn.Reporter, sanitizer *redact.Redactor, now func() time.Time) error {
	if d.remoteConnector == nil || d.remoteConnector.actions == nil || d.remoteConnector.actions.workflows == nil {
		return nil
	}
	invocation, err := judge.Config.ResolveJudge(config.JudgeOptions{}, "", "")
	if err != nil {
		return err
	}
	runner := &turn.TaskAdvisoryRunner{DB: d.db, Paths: judge.Paths, Executable: judge.Executable, Providers: judge.Providers, Git: judge.Git, Provider: agent.Kind(invocation.Provider), Model: invocation.Model, Thinking: invocation.Thinking, Deadline: 30 * time.Minute}
	if !runner.Ready() {
		return errors.New("isolated advisory provider unavailable")
	}
	if _, err = runner.CaptureGuidance(ctx, "", ""); err != nil {
		return err
	}
	capture := taskdiscussion.StoreCapture{DB: d.db, Git: judge.Git, Guidance: func(ctx context.Context, role, repo string) (json.RawMessage, error) {
		return runner.CaptureGuidance(ctx, role, repo)
	}}
	clock := now
	if clock == nil {
		clock = time.Now
	}
	service, err := taskdiscussion.New(taskdiscussion.Options{DB: d.db, Now: clock, Capture: capture, Runner: runner, Publisher: turn.DiscussionPublisher{Reporter: reporter}, Deadline: runner.Deadline, Protect: func(ctx context.Context, text string) (string, error) {
		b, e := sanitizer.Apply(ctx, text, max(4096, len(text)+redact.Margin))
		if e == nil && b.Truncated() {
			e = errors.New("advisory redaction exceeded bound")
		}
		return b.String(), e
	}})
	if err != nil {
		return err
	}
	d.remoteConnector.actions.workflows.BindAdvisory(service)
	d.remoteConnector.actions.discussions = service
	d.remoteConnector.server.SetTaskDiscussion(service)
	return nil
}
