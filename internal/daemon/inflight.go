package daemon

import (
	"context"
	factoryruntime "github.com/cloudvane/litefactory/internal/factory/runtime"
)

// inflight preserves the daemon host's private call sites. Atomic admission,
// cancellation and settlement belong to the factory runtime owner.
type inflight struct{ owner factoryruntime.InFlight }

func (f *inflight) enter(id int64) bool                            { return f.owner.Enter(id) }
func (f *inflight) leave(id int64)                                 { f.owner.Leave(id) }
func (f *inflight) size() int                                      { return f.owner.Size() }
func (f *inflight) list() []int64                                  { return f.owner.List() }
func (f *inflight) label(id int64, source string)                  { f.owner.Label(id, source) }
func (f *inflight) listLabels() map[int64]string                   { return f.owner.ListLabels() }
func (f *inflight) peak() int                                      { return f.owner.Peak() }
func (f *inflight) bindCancel(id int64, cancel context.CancelFunc) { f.owner.BindCancel(id, cancel) }
func (f *inflight) drain(ctx context.Context, id int64, interrupt bool) error {
	return f.owner.Drain(ctx, id, interrupt)
}
func (f *inflight) cancel(id int64, interrupt bool) bool { return f.owner.Cancel(id, interrupt) }
