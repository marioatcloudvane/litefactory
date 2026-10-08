package doctor

import (
	"context"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// RepairEffects performs factory-owned external effects for immutable approved steps.
type RepairEffects interface {
	Execute(context.Context, naming.Ref, store.DoctorRepair, store.DoctorRepairStep, string) (store.DoctorRepairStep, error)
	Verify(context.Context, naming.Ref, store.DoctorRepairStep) error
}
