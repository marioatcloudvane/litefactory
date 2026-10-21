package doctor

import (
	"context"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/instructionpackage"
	"github.com/cloudvane/litefactory/internal/store"
)

func validateInstructions(capture *instructionpackage.RoleCapture, role string) error {
	if capture == nil {
		return errors.New("doctor instruction capture is missing")
	}
	if _, err := capture.Encode(); err != nil {
		return err
	}
	if capture.Snapshot().Role != role || len(capture.Instructions()) == 0 || len(capture.SkillInstructions("doctor-field-guide")) == 0 {
		return errors.New("doctor instruction capture has wrong role or missing guide")
	}
	if role == "doctor-repair" && len(capture.SkillInstructions("doctor-repair-authority")) == 0 {
		return errors.New("doctor repair authority instructions are missing")
	}
	return nil
}

func (s *Service) captureInstructions(ctx context.Context, role string) (*instructionpackage.RoleCapture, error) {
	captured, err := instructionpackage.CaptureRole(ctx, s.Instructions, s.Paths, s.Logger, role)
	if err != nil {
		return nil, err
	}
	if err := validateInstructions(captured, role); err != nil {
		return nil, err
	}
	// Reject an oversized static instruction set before any repair effects.
	if role == "doctor-repair" {
		if _, err := buildRepairPrompt(captured, nil); err != nil {
			return nil, err
		}
	}
	return captured, nil
}

func (s *Service) retainInstructions(ctx context.Context, attempt string, captured *instructionpackage.RoleCapture) error {
	raw, err := captured.Encode()
	if err != nil {
		return err
	}
	return s.DB.RetainDoctorInstructionCapture(ctx, attempt, raw)
}

func buildRepairPrompt(captured *instructionpackage.RoleCapture, manifest []byte) (string, error) {
	if err := validateInstructions(captured, "doctor-repair"); err != nil {
		return "", err
	}
	prompt := string(captured.Instructions()) + "\n" + string(captured.SkillInstructions("doctor-field-guide")) + "\n" + string(captured.SkillInstructions("doctor-repair-authority")) + "\n" + string(manifest)
	if len(prompt) > 131072 {
		return "", errors.New("doctor repair prompt exceeds redaction budget")
	}
	return prompt, nil
}

// Recovery validates retained provenance without consulting current package files.
// Historical attempts predate captures and retain their original version labels.
func (s *Service) checkRetainedInstructions(ctx context.Context, attempt string) error {
	_, err := s.DB.DoctorInstructionCapture(ctx, attempt)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("restore doctor instructions: %w", err)
	}
	return nil
}
