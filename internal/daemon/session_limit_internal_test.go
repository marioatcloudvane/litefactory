package daemon

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

func TestNewRetainsAutoResumeOnSessionLimit(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, yaml string
		want       bool
	}{
		{name: "absent/disabled", yaml: "{}"},
		{name: "false/disabled", yaml: "settings:\n  auto_resume_on_session_limit: false\n"},
		{name: "true/enabled", yaml: "settings:\n  auto_resume_on_session_limit: true\n", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := config.Decode(strings.NewReader(tt.yaml))
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			// Constructor-only dependencies deliberately panic if used: creating
			// a daemon must retain loaded options without touching infrastructure.
			opts := Options{
				AutoResumeOnSessionLimit: cfg.Settings.AutoResumeOnSessionLimit,
				DB:                       &store.DB{},
				Forge:                    struct{ Forge }{},
				Filter:                   struct{ Classifier }{},
				Turn:                     struct{ Executor }{},
				Reporter:                 struct{ Reporter }{},
				InstanceID:               "session-limit-constructor-test",
				Logger:                   slog.New(slog.NewTextHandler(io.Discard, nil)),
				DefaultAgent:             agent.Kind(store.AgentClaude),
				Agents:                   turn.RunnerSet{agent.Kind(store.AgentClaude): nil},
				Classifiers:              map[agent.Kind]agent.Classifier{agent.Kind(store.AgentClaude): nil},
			}

			d, err := New(opts)

			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if d.autoResumeOnSessionLimit != tt.want {
				t.Errorf("daemon auto resume = %t, want %t", d.autoResumeOnSessionLimit, tt.want)
			}
		})
	}
}
