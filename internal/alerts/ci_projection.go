package alerts

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// CITransportReader is the local transport-only read boundary for status views.
type CITransportReader interface {
	CITransport(context.Context, string) (store.CITransportState, error)
}

// ProjectLocal adds durable transport limits to the saved incident projection.
// It never observes GitHub or mutates accepted CI evidence.
func ProjectLocal(ctx context.Context, l store.AlertLedger, now time.Time, allowTask func(string) bool, reader CITransportReader) Projection {
	p := Project(l, now, allowTask)
	if p.CI == nil || reader == nil {
		return p
	}
	wait, err := reader.CITransport(ctx, "ci:"+l.CI.Origin)
	applyCITransport(&p, wait, err, now)
	return p
}
func applyCITransport(p *Projection, wait store.CITransportState, err error, now time.Time) {
	if p.CI == nil {
		return
	}
	reason := ""
	switch {
	case err != nil:
		reason = "store_unavailable"
	case wait.Lease != "" && !wait.LeaseUntil.After(now):
		reason = "transport_uncertain"
	case wait.NotBefore.After(now):
		reason = "rate_limited"
		if p.CI.NextRetry == nil || wait.NotBefore.After(*p.CI.NextRetry) {
			p.CI.NextRetry = projectionTime(wait.NotBefore)
		}
	}
	if reason != "" {
		p.CI.Ready = false
		p.CI.Reason = reason
		for i := range p.Rules {
			if p.Rules[i].Rule == "main_red" || p.Rules[i].Rule == "ci_monitoring_unavailable" {
				p.Rules[i].Ready = false
				p.Rules[i].Reason = reason
			}
		}
	}
}
