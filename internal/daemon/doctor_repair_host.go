package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// DoctorReplacement starts a factory-owned same-binary recovery handoff. It is
// never exposed to the agent and cannot accept executable, PID or shell input.
type DoctorReplacement interface {
	StartDoctorReplacement(context.Context, store.DoctorRestart) error
}

// DoctorHostOptions are installed by the daemon process owner. Recovery is read
// from durable state after lock acquisition, never inferred from a live PID.
type DoctorHostOptions struct {
	Paths       config.Paths
	Replacement DoctorReplacement
	Stop        context.CancelFunc
	Recovery    *store.DoctorRestart
}

type doctorHostRegistry struct {
	Instance   string `json:"instance"`
	Descriptor string `json:"descriptor"`
}
type doctorHostRequest struct {
	Instance  string `json:"instance"`
	Target    string `json:"target,omitempty"`
	Operation string `json:"operation"`
	Step      int    `json:"step,omitempty"`
	Token     string `json:"token,omitempty"`
}
type doctorHostResponse struct {
	Instance  string `json:"instance"`
	Confirmed bool   `json:"confirmed"`
}

// DoctorRepairHost owns bounded authenticated control handlers for this daemon.
// Closing the broker joins every admitted handler before singleton release.
type DoctorRepairHost struct {
	daemon   *Daemon
	options  DoctorHostOptions
	broker   *submission.Broker
	registry doctorHostRegistry
	ready    atomic.Bool
}

func (d *Daemon) startDoctorRepairHost(ctx context.Context, options DoctorHostOptions) (*DoctorRepairHost, error) {
	if err := d.proveDoctorSingleton(); err != nil {
		return nil, err
	}
	if options.Paths.LockPath() != d.lock.Path() {
		return nil, errors.New("doctor host installation differs from held singleton")
	}
	if options.Stop == nil || options.Replacement == nil {
		return nil, errors.New("doctor host lifecycle unavailable")
	}
	if err := os.MkdirAll(options.Paths.DoctorControlDir(), 0700); err != nil {
		return nil, err
	}
	host := &DoctorRepairHost{daemon: d, options: options}
	broker, err := submission.Start(ctx, submission.Options{Directory: options.Paths.DoctorControlDir(), AllowTools: true, AttemptReference: d.instanceID, MaxWorkers: 2, RequestTimeout: time.Minute, Handler: host.handle})
	if err != nil {
		return nil, err
	}
	host.broker = broker
	host.registry = doctorHostRegistry{Instance: d.instanceID, Descriptor: broker.DescriptorPath()}
	if err = writeDoctorHostRegistry(options.Paths, host.registry); err != nil {
		return nil, errors.Join(err, broker.Close())
	}
	return host, nil
}

func (d *Daemon) proveDoctorSingleton() error {
	if d == nil || d.instanceID == "" || d.lock == nil || d.lock.file == nil {
		return errors.New("doctor host requires held daemon singleton")
	}
	descriptor, err := d.lock.file.Stat()
	if err != nil {
		return err
	}
	current, err := os.Lstat(d.lock.Path())
	if err != nil {
		return err
	}
	if !current.Mode().IsRegular() || !os.SameFile(descriptor, current) {
		return errors.New("doctor singleton identity changed")
	}
	return nil
}

func (h *DoctorRepairHost) prepare(ctx context.Context) error {
	if err := h.daemon.proveDoctorSingleton(); err != nil {
		return err
	}
	if h.options.Recovery != nil {
		if _, err := readDoctorHandoff(ctx, h.daemon.db, *h.options.Recovery); err != nil {
			return err
		}
	}
	h.ready.Store(true)
	return nil
}

func (h *DoctorRepairHost) handle(ctx context.Context, q submission.Request) (json.RawMessage, error) {
	if q.Method != "tools/call" {
		return nil, errors.New("doctor host exposes only local control calls")
	}
	call, err := submission.DecodeToolCall(q.Payload)
	if err != nil {
		return nil, err
	}
	var input doctorHostRequest
	if err := submission.DecodeJSON(call.Arguments, &input); err != nil {
		return nil, err
	}
	if input.Instance != h.daemon.instanceID || !h.ready.Load() {
		return nil, errors.New("doctor daemon instance is stale or not ready")
	}
	if err := h.daemon.proveDoctorSingleton(); err != nil {
		return nil, err
	}
	response := doctorHostResponse{Instance: h.daemon.instanceID}
	switch call.Name {
	case "doctor_host_drain":
		target, err := naming.ParseRef(input.Target)
		if err != nil {
			return nil, err
		}
		if input.Token != "" {
			return nil, errors.New("drain does not accept restart authority")
		}
		response.Instance, err = h.daemon.DrainDoctorTarget(ctx, target, input.Operation)
		if err != nil {
			return nil, err
		}
		response.Confirmed = true
	case "doctor_host_stop_run":
		target, err := naming.ParseRef(input.Target)
		if err != nil {
			return nil, err
		}
		if input.Token != "" {
			return nil, errors.New("stop does not accept restart authority")
		}
		if err = h.daemon.StopDoctorRun(ctx, target, input.Operation, input.Step); err != nil {
			return nil, err
		}
		response.Confirmed = true
	case "doctor_host_restart":
		intent, err := readDoctorHandoff(ctx, h.daemon.db, store.DoctorRestart{RepairID: input.Operation, Token: input.Token})
		if err != nil {
			return nil, err
		}
		if intent.OldInstance != h.daemon.instanceID {
			return nil, errors.New("restart targets another daemon instance")
		}
		if err = claimDoctorHostDispatch(h.options.Paths, intent); err != nil {
			return nil, err
		}
		if err = h.options.Replacement.StartDoctorReplacement(ctx, intent); err != nil {
			return nil, err
		}
		// Only this process's trusted cancellation handle is used, never a caller PID.
		h.options.Stop()
	case "doctor_host_observe":
		intent, err := readDoctorHandoff(ctx, h.daemon.db, store.DoctorRestart{RepairID: input.Operation, Token: input.Token})
		if err != nil {
			return nil, err
		}
		recovery := h.options.Recovery
		response.Confirmed = recovery != nil && recovery.RepairID == intent.RepairID && recovery.Token == intent.Token && intent.OldInstance != h.daemon.instanceID
	default:
		return nil, errors.New("doctor host operation not permitted")
	}
	return json.Marshal(response)
}

// Close joins all local control handlers before the owner releases its lock.
func (h *DoctorRepairHost) Close() error {
	if h == nil {
		return nil
	}
	h.ready.Store(false)
	err := h.broker.Close()
	registry, readErr := readDoctorHostRegistry(h.options.Paths)
	if readErr == nil && registry == h.registry {
		err = errors.Join(err, os.Remove(h.options.Paths.DoctorHostRegistryPath()))
	}
	return err
}

// DoctorLocalControl discovers the current private descriptor and binds every
// call to its exact instance. Socket refusal never proves the daemon is down.
type DoctorLocalControl struct{ Paths config.Paths }

func (c *DoctorLocalControl) call(ctx context.Context, name string, input doctorHostRequest) (doctorHostResponse, error) {
	var out doctorHostResponse
	registry, err := readDoctorHostRegistry(c.Paths)
	if err != nil {
		return out, err
	}
	if input.Instance != "" && input.Instance != registry.Instance {
		return out, errors.New("doctor expected daemon instance changed")
	}
	input.Instance = registry.Instance
	args, err := json.Marshal(input)
	if err != nil {
		return out, err
	}
	payload, err := json.Marshal(submission.ToolCall{Name: name, Arguments: args})
	if err != nil {
		return out, err
	}
	reply, err := submission.Call(ctx, registry.Descriptor, "tools/call", payload)
	if err != nil {
		return out, err
	}
	if reply.Error != "" {
		return out, fmt.Errorf("doctor local control refused: %s", reply.Error)
	}
	if err := submission.DecodeJSON(reply.Result, &out); err != nil {
		return out, err
	}
	if out.Instance != registry.Instance {
		return out, errors.New("doctor local response instance mismatch")
	}
	return out, nil
}

// DrainDoctorTarget invokes only the exact target and held operation.
func (c *DoctorLocalControl) DrainDoctorTarget(ctx context.Context, ref naming.Ref, operation string) (string, error) {
	out, err := c.call(ctx, "doctor_host_drain", doctorHostRequest{Target: ref.String(), Operation: operation})
	if err != nil {
		return "", err
	}
	if !out.Confirmed {
		return "", errors.New("doctor drain unconfirmed")
	}
	return out.Instance, nil
}

func readDoctorHandoff(ctx context.Context, db *store.DB, want store.DoctorRestart) (store.DoctorRestart, error) {
	var empty store.DoctorRestart
	if db == nil || want.RepairID == "" || want.Token == "" {
		return empty, errors.New("doctor restart identity missing")
	}
	got, err := db.ReadDoctorRestart(ctx, want.RepairID)
	if err != nil {
		return empty, err
	}
	if got.Token != want.Token || (got.State != "dispatched" && got.State != "uncertain") {
		return empty, errors.New("doctor restart identity is stale or unclaimed")
	}
	if want.OldInstance != "" && got.OldInstance != want.OldInstance {
		return empty, errors.New("doctor restart old-instance mismatch")
	}
	return got, nil
}

func validDoctorDescriptor(paths config.Paths, name string) bool {
	relative, err := filepath.Rel(paths.DoctorControlDir(), name)
	return err == nil && filepath.Base(name) == "descriptor.json" && filepath.IsLocal(relative) && filepath.Dir(filepath.Dir(name)) == paths.DoctorControlDir()
}

// StopDoctorRun invokes the exact held stop step over authenticated local IPC.
func (c *DoctorLocalControl) StopDoctorRun(ctx context.Context, ref naming.Ref, id string, number int) error {
	out, err := c.call(ctx, "doctor_host_stop_run", doctorHostRequest{Target: ref.String(), Operation: id, Step: number})
	if err != nil {
		return err
	}
	if !out.Confirmed {
		return errors.New("doctor stop unconfirmed")
	}
	return nil
}
