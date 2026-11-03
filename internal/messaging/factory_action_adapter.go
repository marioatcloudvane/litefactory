package messaging

import (
	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/owneraction"
)

// actionAvailability supplies registration facts to the shared factory validator.
func (s *Service) actionAvailability() owneractions.Availability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, draft := s.definitions["draft"]
	_, confirm := s.definitions["confirm"]
	_, answer := s.definitions["answer"]
	_, diagnose := s.definitions["diagnose"]
	_, standards := s.definitions["standards-author"]
	return owneractions.Availability{Draft: draft, Confirm: confirm, Answer: answer, Diagnose: diagnose, Standards: standards}
}

func (b selectionDomains) standardsDomain() owneractions.Standards {
	s := b.Service
	return owneractions.Standards{Targets: s.factory.Targets, Available: s.standards != nil, Protect: s.Protect}
}

func (b selectionDomains) actionDomain() owneractions.Actions {
	s := b.Service
	return owneractions.Actions{FallbackTransport: s.transport, Targets: s.factory.Targets, Protect: s.Protect, Research: s.research, Availability: s.actionAvailability()}
}

func actionSelection(p naturalPayload, fallback string) owneractions.Selection {
	return owneractions.Selection{Invocation: owneraction.Invocation{Command: p.Invocation.Command, Arguments: p.Invocation.Arguments}, PreviewID: p.PreviewID, PreviewHash: p.PreviewHash, Question: p.Question, Revision: p.Revision, RepoID: p.RepoID, Project: p.Project, Channel: p.Channel, Transport: naturalTransport(p, fallback), ProjectPending: p.ProjectPending, Focus: p.focus}
}
