package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/instructionpackage"
)

// validateJudgeInstructions checks only durable bytes. Report recovery deliberately
// does not call it because historical requests may have no complete capture.
func validateJudgeInstructions(cfg judgeConfiguration) error {
	if len(cfg.Instructions) == 0 {
		return errors.New("judge instruction capture is missing; historical role-only captures cannot launch")
	}
	capture, err := instructionpackage.Restore(cfg.Instructions)
	if err != nil {
		return fmt.Errorf("judge retained instruction capture: %w", err)
	}
	data := capture.Snapshot()
	if data.Role != instructionpackage.JudgeRole {
		return errors.New("judge retained instruction capture has the wrong role")
	}
	role := capture.Instructions()
	sum := sha256.Sum256(role)
	if cfg.Role != string(role) || cfg.RoleDigest != hex.EncodeToString(sum[:]) || cfg.InstructionDigest != data.EffectiveDigest {
		return errors.New("judge retained instruction identity mismatch")
	}
	return nil
}

func (s *JudgeService) captureJudgeConfiguration(ctx context.Context, in JudgeRequest, invocation config.JudgeInvocation) (judgeConfiguration, error) {
	cfg := judgeConfiguration{Requested: in.Options, Effective: invocation}
	capture := in.Instructions
	if capture == nil {
		if in.GraphInstanceID > 0 {
			if in.RecoverOnly {
				return cfg, nil
			}
			return cfg, errors.New("judge workflow instruction capture is missing; historical role-only captures cannot launch")
		}
		pkg, err := instructionpackage.New(ctx, s.Paths)
		if err != nil {
			return cfg, fmt.Errorf("capture manual judge instructions: %w", err)
		}
		var diagnostics []instructionpackage.Diagnostic
		capture, diagnostics, err = pkg.Capture(ctx, instructionpackage.JudgeRole)
		if err == nil && s.Logger != nil {
			for _, diagnostic := range diagnostics {
				s.Logger.WarnContext(ctx, "judge instruction fallback", "role", diagnostic.Role, "file", diagnostic.File, "category", diagnostic.Category, "source", diagnostic.SelectedSource)
			}
		}
		if err != nil {
			return cfg, fmt.Errorf("capture manual judge instructions: %w", err)
		}
	}
	raw, err := capture.Encode()
	if err != nil {
		return cfg, fmt.Errorf("retain judge instructions: %w", err)
	}
	capture, err = instructionpackage.Restore(raw)
	if err != nil {
		return cfg, fmt.Errorf("retain judge instructions: %w", err)
	}
	if capture.Role() != instructionpackage.JudgeRole {
		return cfg, errors.New("judge instruction capture has the wrong role")
	}
	role := capture.Instructions()
	sum := sha256.Sum256(role)
	cfg.Role = string(role)
	cfg.RoleDigest = hex.EncodeToString(sum[:])
	cfg.Instructions = json.RawMessage(raw)
	cfg.InstructionDigest = capture.Snapshot().EffectiveDigest
	return cfg, nil
}
