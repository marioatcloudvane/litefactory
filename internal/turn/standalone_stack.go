package turn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/harness"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/teamsource"
)

const standaloneStackLimit = 16 << 20

type standaloneStackCapture struct {
	Schema int                       `json:"schema"`
	Origin teamsource.WorkflowOrigin `json:"origin"`
	Files  map[string][]byte         `json:"files"`
	Digest string                    `json:"digest"`
}

func standaloneStackSpecs(workflow agent.Workflow) (graphexec.Registry, error) {
	switch workflow {
	case agent.WorkflowResearch:
		return ResearchSpecs()
	case agent.WorkflowPlan:
		return PlanSpecs()
	case agent.WorkflowImplement:
		return ImplementSpecs()
	case agent.WorkflowValidate:
		return ValidateFlowSpecs()
	case agent.WorkflowSpike:
		return SpikeSpecs()
	}
	return graphexec.Registry{}, errors.New("stack.workflow: unsupported standalone workflow")
}

// A resumed ordinary run retains the same private workflow capture alongside its
// existing run metadata. Historical runs without the marker keep their semantics.
func prepareStandaloneStack(ctx context.Context, opts Options, req Request, previous store.Run, resume bool) (*teamsource.WorkflowDefinition, error) {
	if !resume && opts.WorkflowSource == nil {
		return nil, nil
	}
	workflow := agent.WorkflowResearch
	var err error
	// Infer is the historical tool-free mode-selection path. Capture this
	// selected team's neutral Research scope without changing its run mode.
	if req.mode() != "infer" || req.Workflow != "" {
		workflow, err = agent.ResolveWorkflow(req.Workflow, agent.Mode(req.mode()))
	}
	if err != nil {
		return nil, err
	}
	specs, err := standaloneStackSpecs(workflow)
	if err != nil {
		return nil, err
	}
	if resume {
		raw, err := readStandaloneStackFile(previous.LogPath + ".stack-workflow.json")
		if errors.Is(err, os.ErrNotExist) {
			metaRaw, metaErr := readStandaloneStackFile(stringsMetaPath(previous.LogPath))
			if metaErr != nil && !errors.Is(metaErr, os.ErrNotExist) {
				return nil, errors.New("stack.capture: prior metadata unavailable")
			}
			var meta harness.Meta
			if len(metaRaw) > 0 && json.Unmarshal(metaRaw, &meta) != nil {
				return nil, errors.New("stack.capture: prior metadata invalid")
			}
			if meta.StackCaptureDigest != "" {
				return nil, errors.New("stack.capture: retained workflow missing")
			}
			return nil, nil
		}
		if err != nil {
			return nil, errors.New("stack.capture: retained workflow unavailable")
		}
		var capture standaloneStackCapture
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&capture) != nil || capture.Schema != 1 {
			return nil, errors.New("stack.capture: retained workflow invalid")
		}
		canonical, err := json.Marshal(capture)
		if err != nil || !bytes.Equal(canonical, raw) {
			return nil, errors.New("stack.capture: retained bytes changed")
		}
		metaRaw, metaErr := readStandaloneStackFile(stringsMetaPath(previous.LogPath))
		if metaErr != nil {
			return nil, errors.New("stack.capture: retained metadata unavailable")
		}
		var meta harness.Meta
		if json.Unmarshal(metaRaw, &meta) != nil || meta.StackCaptureDigest != standaloneStackDigest(raw) {
			return nil, errors.New("stack.capture: retained digest mismatch")
		}
		if err := store.ValidateWorkflowProvenance(store.WorkflowProvenance(capture.Origin)); err != nil {
			return nil, errors.New("stack.capture: retained provenance invalid")
		}
		d, err := teamsource.CaptureWorkflowIdentity(ctx, teamsource.RetainedFiles(capture.Files), capture.Origin.GraphFile, capture.Origin.Team, workflow, specs)
		if err != nil || d.Digest != capture.Digest || len(d.Files) != len(capture.Files) || d.StackProfile == nil {
			return nil, errors.New("stack.capture: retained resources or identity invalid")
		}
		if err := validateStackHost(d.StackProfile.Effective.Requirements); err != nil {
			return nil, err
		}
		d.Origin = capture.Origin
		return d, nil
	}
	if opts.WorkflowSource == nil {
		return nil, nil
	}
	d, err := capturedWorkflow(ctx, opts, req, workflow, specs)
	if err != nil {
		return nil, err
	}
	if d.StackProfile == nil {
		return nil, nil
	}
	return d, nil
}

func stringsMetaPath(logPath string) string {
	return logPath[:len(logPath)-len(filepath.Ext(logPath))] + ".meta.json"
}

func standaloneStackDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func readStandaloneStackFile(name string) ([]byte, error) {
	// #nosec G304 -- name is derived solely from a factory-recorded private run log path.
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() > standaloneStackLimit {
		return nil, errors.New("stack.capture: artifact bounds exceeded")
	}
	raw, err := io.ReadAll(io.LimitReader(f, standaloneStackLimit+1))
	if len(raw) > standaloneStackLimit {
		return nil, errors.New("stack.capture: artifact bounds exceeded")
	}
	return raw, err
}

func retainStandaloneStack(run store.Run, definition *teamsource.WorkflowDefinition) (string, error) {
	if definition == nil {
		return "", nil
	}
	raw, err := json.Marshal(standaloneStackCapture{Schema: 1, Origin: definition.Origin, Files: definition.Files, Digest: definition.Digest})
	if err != nil || len(raw) > standaloneStackLimit {
		return "", errors.New("stack.capture: capture exceeds serialization bounds")
	}
	name := run.LogPath + ".stack-workflow.json"
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return "", err
	}
	// #nosec G304 -- name is derived solely from the factory-allocated private run log path.
	f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(raw)
	var syncErr error
	if writeErr == nil {
		syncErr = f.Sync()
	}
	if err := errors.Join(writeErr, syncErr, f.Close()); err != nil {
		return "", err
	}
	return standaloneStackDigest(raw), nil
}

func standaloneStackGuidance(d *teamsource.WorkflowDefinition) (string, error) {
	resources := map[string]string{}
	for name, raw := range d.Files {
		if strings.HasPrefix(name, ".stack-capture/guidance/") {
			resources[strings.TrimPrefix(name, ".stack-capture/guidance/")] = string(raw)
		}
	}
	raw, err := json.Marshal(resources)
	if err != nil {
		return "", err
	}
	guidance := "\nSelected stack guidance and complete referenced resources retained at admission: " + string(raw) + "\n"
	if d.Profile.Mode == agent.ModeImplement {
		recipes, err := stackEngineeringGuidance(agent.Request{StackProfile: d.StackProfile})
		if err != nil {
			return "", err
		}
		guidance += recipes
	}
	return guidance, nil
}

func standaloneStackSetupAllowed(req Request) bool {
	if req.mode() != string(agent.ModeImplement) || req.Workflow == agent.WorkflowBug {
		return false
	}
	if req.Agent == agent.Kind(store.AgentCodex) {
		return req.CodexSandbox == "workspace-write"
	}
	if req.ToolProfile != "implement" {
		return false
	}
	for _, tool := range req.Tools {
		if tool == "Bash" && len(req.AllowedTools) == 0 {
			return true
		}
	}
	return false
}
