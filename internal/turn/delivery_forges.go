package turn

import (
	"context"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
)

// DeliveryPRForge supplies shared PR observations and factory-owned creation.
type DeliveryPRForge interface {
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	ListDeliveryPRs(context.Context, github.Repo, string) ([]github.DeliveryPR, error)
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	CreateDeliveryPR(context.Context, github.Repo, string, string, redact.Body, redact.Body) (github.DeliveryPR, error)
}

// DeliveryMergeForge supplies guarded merge and independent integration observations.
type DeliveryMergeForge interface {
	GetDeliveryPR(context.Context, github.Repo, int) (github.DeliveryPR, error)
	GetDeliveryHead(context.Context, github.Repo, string) (string, error)
	GetDeliveryCommit(context.Context, github.Repo, string) (github.DeliveryCommit, error)
	MergeDeliveryPR(context.Context, github.Repo, int, string) error
}

// DeliveryCloseForge supplies numeric issue observation and confirmed closure.
type DeliveryCloseForge interface {
	BaseURL() string
	GetIssue(context.Context, github.Repo, int) (github.Issue, error)
	CloseDeliveredParent(context.Context, github.Repo, int, int64) error
}

// DeliveryCodeForge keeps repository and integration operations separate from tickets.
type DeliveryCodeForge interface {
	DeliveryPRForge
	DeliveryMergeForge
}

// RolePermissions carries a graph role's tool surface and sandbox.
type RolePermissions struct {
	Tools   []string
	Sandbox string
}
