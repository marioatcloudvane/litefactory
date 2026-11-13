package turn

import (
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/denypolicy"
	"github.com/cloudvane/litefactory/internal/graphexec"
)

func TestDenyAdmissionResolvesEverySelectedNode(t *testing.T) {
	loads := 0
	opts := Options{LoadDenyConfig: func(config.Paths) (*config.Config, error) {
		loads++
		return &config.Config{Settings: config.Settings{DenyRules: denypolicy.Catalogue{"known": {Deny: []string{"Agent"}}}}}, nil
	}}
	profile := &graphexec.Profile{Attrs: map[string]string{"lf_deny": "known"}, Nodes: []graphexec.Node{{Name: "first", Attrs: map[string]string{"lf_deny": "known"}}, {Name: "later", Attrs: map[string]string{"lf_deny": "missing"}}}}
	err := admitDenySelections(opts, profile)
	if err == nil || !strings.Contains(err.Error(), "later") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("unknown future selection admitted: %v", err)
	}
	if loads != 1 {
		t.Fatalf("loads=%d", loads)
	}
	profile.Nodes[1].Attrs["lf_deny"] = ""
	if err := admitDenySelections(opts, profile); err != nil {
		t.Fatal(err)
	}
}

func TestDenyAdmissionSkipsUnselectedConfiguration(t *testing.T) {
	opts := Options{LoadDenyConfig: func(config.Paths) (*config.Config, error) {
		t.Fatal("unselected admission loaded settings")
		return nil, errors.New("unavailable")
	}}
	profile := &graphexec.Profile{Nodes: []graphexec.Node{{Name: "worker", Attrs: map[string]string{"lf_deny": " "}}}}
	if err := admitDenySelections(opts, profile); err != nil {
		t.Fatal(err)
	}
}

func TestDenyAdmissionRefusesReloadFailure(t *testing.T) {
	for _, load := range []func(config.Paths) (*config.Config, error){func(config.Paths) (*config.Config, error) { return nil, errors.New("unavailable") }, func(config.Paths) (*config.Config, error) { return nil, nil }} {
		opts := Options{LoadDenyConfig: load}
		profile := &graphexec.Profile{Attrs: map[string]string{"lf_deny": "selected"}}
		if err := admitDenySelections(opts, profile); err == nil {
			t.Fatal("selected admission accepted unavailable configuration")
		}
	}
}
