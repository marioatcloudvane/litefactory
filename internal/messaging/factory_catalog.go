package messaging

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
)

// projectCatalog adapts the owned registered-project projection to legacy Ask.
type projectCatalog struct{ factory *factory.Messaging }

func (p projectCatalog) ListRepos(ctx context.Context) ([]contracts.Project, error) {
	return p.factory.Projects(ctx)
}
