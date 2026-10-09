package agent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
)

// ErrDeliveryRefused distinguishes instruction refusal from a provider resume
// failure. It must never trigger a cold retry or replacement of saved bytes.
var ErrDeliveryRefused = errors.New("instruction delivery refused")

// DeliveryRefusal supplies the workflow/node context and supported recovery path.
func DeliveryRefusal(req Request, cause error) error {
	workflow := string(req.Workflow)
	if workflow == "" {
		workflow = req.Mode
	}
	node := "agent"
	if req.Graph != nil {
		node = req.Graph.Node
	}
	return fmt.Errorf("%w: workflow %s node %s: %w; use the supported installed-workflow path: install and link the workflow team, then start a new supported workflow; preserve this saved bundle for diagnosis", ErrDeliveryRefused, workflow, node, cause)
}

// VerifyRequestDelivery is the shared launch boundary. Separate advisory calls
// may omit delivery; every graph launch must retain its captured instructions.
func VerifyRequestDelivery(ctx context.Context, req Request) error {
	if req.Invocation == InvocationGraphNode {
		if err := req.validateInstructionSelection(); err != nil {
			return DeliveryRefusal(req, err)
		}
	}
	if req.Delivery == nil {
		return nil
	}
	if err := VerifyDelivery(ctx, *req.Delivery); err != nil {
		return DeliveryRefusal(req, err)
	}
	return nil
}

func (req Request) validateInstructionSelection() error {
	d := req.Delivery
	if d == nil {
		return errors.New("missing captured instruction delivery")
	}
	if err := d.validate(); err != nil {
		return err
	}
	if d.MainRole == "" || strings.TrimSpace(d.MainModel) == "" {
		return errors.New("missing delivered main role or resolved model")
	}
	if len(d.Evidence.Specialists) != 1 || d.Evidence.Specialists[0].ID != d.MainRole || d.Evidence.Specialists[0].Model != d.MainModel {
		return errors.New("delivered main role/model disagrees with captured selection")
	}
	if !slices.Equal(d.Skills, d.RequiredSkills) {
		return errors.New("delivered skills disagree with required main-role skills")
	}
	if err := validateRenderedManifest(d.RenderedFiles); err != nil {
		return err
	}
	required := []string{"bootstrap.md", "plugin/.claude-plugin/plugin.json"}
	claudeRole := "plugin/agents/" + d.MainRole + ".md"
	codexRole := "roles/" + d.MainRole + ".toml"
	_, claude := d.RenderedFiles[claudeRole]
	_, codex := d.RenderedFiles[codexRole]
	if claude == codex {
		return errors.New("rendered manifest requires exactly the selected native main role")
	}
	if codex {
		required = append(required, "main-instructions.md")
	}
	selected := map[string]bool{}
	for _, skill := range d.RequiredSkills {
		if !strings.HasPrefix(skill, "plugin/skills/") || path.Base(skill) != "SKILL.md" || path.Dir(path.Dir(skill)) != "plugin/skills" || selected[path.Dir(skill)] {
			return fmt.Errorf("unsafe or duplicate required skill: %s", skill)
		}
		selected[path.Dir(skill)] = true
		required = append(required, skill)
	}
	for _, file := range required {
		if _, ok := d.RenderedFiles[file]; !ok {
			return fmt.Errorf("missing rendered selection resource: %s", file)
		}
	}
	for _, file := range sortedRenderedPaths(d.RenderedFiles) {
		if strings.HasPrefix(file, "plugin/agents/") && file != claudeRole || strings.HasPrefix(file, "roles/") && file != codexRole {
			return fmt.Errorf("unselected rendered role: %s", file)
		}
		if strings.HasPrefix(file, "plugin/skills/") {
			parts := strings.Split(file, "/")
			if len(parts) < 4 || !selected[strings.Join(parts[:3], "/")] {
				return fmt.Errorf("unselected rendered skill resource: %s", file)
			}
		}
	}
	return nil
}
