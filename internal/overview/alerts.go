package overview

import (
	"context"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/alerts"
	"github.com/cloudvane/litefactory/internal/store"
)

type alertReader interface {
	ReadAlerts(context.Context) (store.AlertLedger, error)
}

func (s *Service) projectAlerts(ctx context.Context, r *Result, repos []store.OverviewRepository, now time.Time) {
	reader, ok := s.reader.(alertReader)
	if !ok {
		r.Alerts = alerts.Project(store.AlertLedger{}, now, nil)
		r.Coverage.Sources["alerts"] = Source{Status: "unavailable", ReadAt: now, Reasons: []string{"alert_reader_unavailable"}}
		return
	}
	ledger, err := reader.ReadAlerts(ctx)
	if err != nil {
		r.Alerts = alerts.Project(store.AlertLedger{}, now, nil)
		r.Coverage.Sources["alerts"] = Source{Status: "unavailable", ReadAt: now, Reasons: []string{"source_read_failed"}}
		return
	}
	// Repository membership comes from the same authorized local reader. Never
	// broaden a filtered registry using task text or private subject identifiers.
	allowed := map[string]bool{}
	for _, repo := range repos {
		allowed[repo.Name] = true
	}
	transport, _ := s.reader.(alerts.CITransportReader)
	r.Alerts = alerts.ProjectLocal(ctx, ledger, now, func(task string) bool {
		colon := strings.IndexByte(task, ':')
		hash := strings.LastIndexByte(task, '#')
		return colon >= 0 && hash > colon && allowed[task[colon+1:hash]]
	}, transport)
	status := "complete"
	reasons := []string{}
	for _, rule := range r.Alerts.Rules {
		if !rule.Ready {
			status = "unavailable"
			reasons = appendUnique(reasons, rule.Reason)
		}
	}
	r.Coverage.Sources["alerts"] = Source{Status: status, ReadAt: now, Reasons: reasons}
}
