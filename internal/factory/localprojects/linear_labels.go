package localprojects

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/linear"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
)

// LinearLabelSetup is the dry run for one Linear project. Nothing is written
// until Apply, and Apply reads Linear again before the first create.
type LinearLabelSetup struct {
	Plan  projectconnection.LinearLabelPlan
	Found int
	// Invalid is the store's refusal of the version setup would save.
	Invalid string
	version store.ConnectionVersion
	client  *linear.Client
	db      *store.DB
	wanted  []agent.WantedLabel
}

// Applicable says whether apply may write: no blocking finding, no ambiguity
// and a savable candidate version.
func (l *LinearLabelSetup) Applicable() bool { return l.Plan.Applicable && l.Invalid == "" }

// LinearCreated names one label created by Apply.
type LinearCreated struct{ Name, ID string }

func (s *Service) planLinearLabelSetup(ctx context.Context, v store.ConnectionVersion) (*LabelSetup, error) {
	clients, err := s.factory(0).Clients(ctx, v, 0)
	if err != nil {
		return nil, unavailable(err)
	}
	client := clients.Linear
	if _, err := client.ContainerIdentity(ctx, client.Container()); err != nil {
		return nil, unavailableText("the saved Linear workspace, team or project does not match what Linear reports; nothing was changed")
	}
	l := &LinearLabelSetup{version: v, client: client, db: s.db, wanted: agent.WantedLabels(s.options.LabelAgents)}
	if err := l.discover(ctx); err != nil {
		return nil, unavailable(err)
	}
	return &LabelSetup{Linear: l}, nil
}

// discover reads every label and plans again; it writes nothing.
func (l *LinearLabelSetup) discover(ctx context.Context) error {
	found, err := l.client.ListSetupLabels(ctx)
	if err != nil {
		return fmt.Errorf("Linear labels could not be read in full; nothing was changed: %w", err) //nolint:staticcheck // owner-facing sentence
	}
	modes, err := l.version.Linear.ModeLabels()
	if err != nil {
		return err
	}
	agents, err := l.version.Linear.CodingAgentLabels()
	if err != nil {
		return err
	}
	bug, err := l.version.Linear.BugLabel()
	if err != nil {
		return err
	}
	l.Found = len(found)
	l.Plan = projectconnection.PlanLinearLabels(l.wanted, found, modes, agents, bug)
	l.Invalid = ""
	if candidate, err := l.candidate(nil); err != nil {
		l.Invalid = err.Error()
	} else if err := store.ValidateConnectionVersion(candidate); err != nil {
		l.Invalid = err.Error()
	}
	return nil
}

func (l *LinearLabelSetup) candidate(created map[string]string) (store.ConnectionVersion, error) {
	cfg, err := l.Plan.CandidateWith(l.version.Linear, created)
	if err != nil {
		return store.ConnectionVersion{}, err
	}
	v := l.version
	v.Linear = cfg
	v.Provenance = "explicit"
	return v, nil
}

func (l *LinearLabelSetup) applyLinear(ctx context.Context) (out LabelSetupResult) {
	if !l.Applicable() {
		out.Refused = "the plan has blocking findings or ambiguities; nothing written"
		return out
	}
	if !l.Plan.Changed {
		out.NothingToSave = true
		return out
	}
	release, err := l.db.LockLabelSetup(ctx, l.version.Linear.WorkspaceID+"/"+l.version.Linear.TeamID)
	if err != nil {
		if errors.Is(err, store.ErrLabelSetupBusy) {
			out.Refused = "another label setup for this Linear team is running; nothing written, rerun when it has finished"
		} else {
			out.Refused = "label setup could not be serialised; nothing written"
		}
		return out
	}
	defer func() { _ = release() }()
	current, err := l.db.CurrentConnection(ctx, l.version.RepoID, store.TicketConnection)
	if err != nil || current.ID != l.version.ID {
		out.Refused = "the connection changed since the dry run; nothing written, rerun setup"
		return out
	}
	dry := l.Plan
	if err := l.discover(ctx); err != nil {
		out.Refused = "Linear labels could not be read again before writing; nothing written"
		return out
	}
	adopted, changed := comparePlans(dry, l.Plan)
	out.AppearedAdopted = adopted
	replanned := l.Plan
	out.Replanned = &replanned
	switch {
	case !l.Applicable():
		out.Refused = "labels changed since the dry run and the new plan cannot be applied; nothing written"
		return out
	case changed:
		out.Refused = "labels changed since the dry run in a way setup will not apply without showing you; nothing written, rerun setup"
		return out
	case !l.Plan.Changed:
		out.NothingToSave = true
		return out
	}
	created := map[string]string{}
	for i, c := range l.Plan.Creates {
		parent := c.ParentID
		if c.ParentPlaceholder != "" {
			parent = created[c.ParentPlaceholder]
		}
		id, err := l.client.CreateLabel(ctx, linear.NewLabel{Name: c.Name, Color: c.Color, Description: c.Description, IsGroup: c.IsGroup, ParentID: parent})
		if err != nil {
			out.Failed, out.FailedWhy = createName(c), linearFailureReason(err)
			for _, rest := range l.Plan.Creates[i+1:] {
				out.NotTried = append(out.NotTried, createName(rest))
			}
			return out
		}
		created[c.Placeholder] = id
		out.Created = append(out.Created, createName(c))
		out.LinearCreated = append(out.LinearCreated, LinearCreated{Name: createName(c), ID: id})
	}
	candidate, err := l.candidate(created)
	if err == nil {
		err = store.ValidateConnectionVersion(candidate)
	}
	if err != nil {
		out.SaveRefused = "the connection could not be built from the labels; nothing saved, rerun setup to adopt them"
		return out
	}
	var verify []string
	for _, id := range l.Plan.VerifyIDs {
		if createdID, ok := created[id]; ok {
			id = createdID
		}
		verify = append(verify, id)
	}
	if err := l.client.VerifyForSave(ctx, verify); err != nil {
		out.SaveRefused = "Linear did not confirm the labels, states or destination at save time; nothing saved, rerun setup once it is fixed"
		return out
	}
	saved, err := l.db.SetConnection(ctx, candidate, l.version.Revision)
	if err != nil {
		if errors.Is(err, store.ErrConnectionConflict) {
			out.SaveRefused = "the connection was changed by someone else while setup ran; nothing saved, rerun setup to adopt the labels"
		} else {
			out.SaveRefused = "the connection could not be saved; nothing saved, rerun setup to adopt the labels"
		}
		return out
	}
	out.SavedRevision = saved.Revision
	return out
}

// comparePlans allows only one kind of change between the dry run and apply:
// a label planned for creation now exists and is adopted. Any other change,
// including a different place for a create, counts as changed.
func comparePlans(dry, fresh projectconnection.LinearLabelPlan) (adopted []string, changed bool) {
	if len(dry.Entries) != len(fresh.Entries) {
		return nil, true
	}
	for i, d := range dry.Entries {
		f := fresh.Entries[i]
		switch {
		case d.Status == projectconnection.StatusCreate && f.Status == projectconnection.StatusAdopted:
			adopted = append(adopted, f.Wanted.Name)
		case d.Status != f.Status:
			return adopted, true
		case f.Status == projectconnection.StatusCreate:
			if d.Create.Name != f.Create.Name || d.Create.ParentID != f.Create.ParentID || d.Create.ParentPlaceholder != f.Create.ParentPlaceholder || d.Create.Form != f.Create.Form {
				return adopted, true
			}
		case d.Chosen != nil && (f.Chosen == nil || d.Chosen.ID != f.Chosen.ID):
			return adopted, true
		}
	}
	for _, c := range fresh.Creates {
		if !c.IsGroup {
			continue
		}
		known := false
		for _, g := range dry.Creates {
			known = known || (g.IsGroup && g.Placeholder == c.Placeholder)
		}
		if !known {
			return adopted, true
		}
	}
	return adopted, false
}

func createName(c projectconnection.LinearCreate) string {
	switch {
	case c.IsGroup:
		return c.Name + " (group)"
	case c.Form == projectconnection.FormGrouped:
		return c.Wanted + " (in group)"
	default:
		return c.Name
	}
}

func linearFailureReason(err error) string {
	var quota *ticket.RateLimitError
	if errors.As(err, &quota) {
		return "rate limited by Linear"
	}
	var failure *ticket.Failure
	if errors.As(err, &failure) {
		switch failure.Kind {
		case ticket.Refused:
			return "Linear refused the label; check that the token may create labels in this team"
		case ticket.Uncertain:
			return "the reply from Linear was lost, so the label may exist; rerun setup to adopt it"
		}
	}
	return "request to Linear failed"
}
