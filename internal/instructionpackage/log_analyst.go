package instructionpackage

import "context"

// LogAnalystRole names the standalone bounded execution-log analyst.
const LogAnalystRole = "log-analyst"

// LogAnalystInstructions captures the complete focused role without ambient owner
// instructions, which must not enlarge the isolated analyst's authority.
func LogAnalystInstructions(ctx context.Context) ([]byte, error) {
	p, err := LoadShipped(ctx, "")
	if err != nil {
		return nil, err
	}
	role, _, err := p.Capture(ctx, LogAnalystRole)
	if err != nil {
		return nil, err
	}
	instructions := role.Instructions()
	for _, name := range selectedSkills(LogAnalystRole) {
		instructions = append(instructions, '\n')
		instructions = append(instructions, role.SkillInstructions(name)...)
	}
	return instructions, nil
}
