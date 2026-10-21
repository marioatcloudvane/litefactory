package standards

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

func apply(before Snapshot, ops []Operation) (Snapshot, error) {
	categories := map[string]Category{}
	standards := map[string]Standard{}
	for _, c := range before.Categories {
		categories[c.ID] = c
	}
	for _, v := range before.Standards {
		standards[v.ID] = v
	}
	problems := []Problem{}
	add := func(field, message string) { problems = append(problems, Problem{field, message}) }
	seen := map[string]bool{}
	for i, op := range ops {
		field := fmt.Sprintf("operations[%d]", i)
		parts := strings.Split(op.Kind, ".")
		if len(parts) != 2 || (parts[0] != "category" && parts[0] != "standard") || (parts[1] != "add" && parts[1] != "replace" && parts[1] != "remove") {
			add(field+".kind", "use category.add/replace/remove or standard.add/replace/remove")
			continue
		}
		entity, kind := parts[0], parts[1]
		id := op.ID
		switch {
		case kind == "remove":
			if op.Category != nil || op.Standard != nil {
				add(field, "remove requires only id")
			}
		case entity == "category":
			if op.Category == nil || op.Standard != nil || op.ID != "" {
				add(field, "category add/replace requires only category")
				continue
			}
			id = op.Category.ID
		default:
			if op.Standard == nil || op.Category != nil || op.ID != "" {
				add(field, "standard add/replace requires only standard")
				continue
			}
			id = op.Standard.ID
		}
		if strings.TrimSpace(id) == "" {
			add(field+".id", "must not be blank")
			continue
		}
		key := entity + ":" + id
		if seen[key] {
			add(field+".id", "duplicate operation for "+key)
			continue
		}
		seen[key] = true
		if entity == "category" {
			_, exists := categories[id]
			if kind == "add" && exists {
				add(field+".category.id", "already exists")
				continue
			}
			if kind != "add" && !exists {
				add(field+".id", "category does not exist in base")
				continue
			}
			if kind == "remove" {
				delete(categories, id)
			} else {
				categories[id] = *op.Category
			}
		} else {
			_, exists := standards[id]
			if kind == "add" && exists {
				add(field+".standard.id", "already exists")
				continue
			}
			if kind != "add" && !exists {
				add(field+".id", "standard does not exist in base")
				continue
			}
			if kind == "remove" {
				delete(standards, id)
			} else {
				standards[id] = *op.Standard
			}
		}
	}
	after := Snapshot{}
	for _, c := range categories {
		after.Categories = append(after.Categories, c)
	}
	for _, v := range standards {
		after.Standards = append(after.Standards, v)
	}
	after = canonical(after)
	if err := validateSnapshot(after); err != nil {
		var invalid *ValidationError
		if errors.As(err, &invalid) {
			problems = append(problems, invalid.Problems...)
		} else {
			return Snapshot{}, err
		}
	}
	if reflect.DeepEqual(canonical(before), after) {
		add("operations", "no changes to the resulting book")
	}
	if len(problems) > 0 {
		return Snapshot{}, &ValidationError{problems}
	}
	return after, nil
}
func validateSnapshot(s Snapshot) error {
	problems := []Problem{}
	add := func(field, message string) { problems = append(problems, Problem{field, message}) }
	ids := map[string]bool{}
	names := map[string]bool{}
	for _, c := range s.Categories {
		f := "categories[" + c.ID + "]"
		if strings.TrimSpace(c.ID) == "" {
			add(f+".id", "must not be blank")
		}
		if ids[c.ID] {
			add(f+".id", "duplicate stable ID")
		}
		ids[c.ID] = true
		n := strings.TrimSpace(c.Name)
		if n == "" {
			add(f+".name", "must not be blank")
		}
		if names[n] {
			add(f+".name", "duplicate category name")
		}
		names[n] = true
	}
	standardIDs := map[string]bool{}
	for _, v := range s.Standards {
		f := "standards[" + v.ID + "]"
		if strings.TrimSpace(v.ID) == "" {
			add(f+".id", "must not be blank")
		}
		if standardIDs[v.ID] {
			add(f+".id", "duplicate stable ID")
		}
		standardIDs[v.ID] = true
		validatePolicy(v.ExceptionPolicy, f+".exception_policy", add)
		if strings.TrimSpace(v.Title) == "" {
			add(f+".title", "must not be blank")
		}
		if strings.TrimSpace(v.Body) == "" {
			add(f+".body", "must not be blank")
		}
		if len(v.CategoryIDs) == 0 {
			add(f+".category_ids", "requires at least one category")
		}
		members := map[string]bool{}
		for _, id := range v.CategoryIDs {
			if !ids[id] {
				add(f+".category_ids", "category "+id+" does not exist; reassign or remove its standards before removing a category")
			}
			if members[id] {
				add(f+".category_ids", "duplicate membership "+id)
			}
			members[id] = true
		}
	}
	if len(problems) > 0 {
		return &ValidationError{problems}
	}
	return nil
}

func validatePolicy(p *ExceptionPolicy, field string, add func(string, string)) {
	if p == nil {
		return
	}
	if p.Version != 1 {
		add(field+".version", "unsupported policy version; use 1")
	}
	switch p.Mode {
	case "deny":
		if p.AllRegisteredRepositories || len(p.RepositoryIDs) > 0 || len(p.TaskRefs) > 0 || p.MaxLifetimeSeconds != 0 {
			add(field, "deny must not declare eligibility or lifetime")
		}
	case "allow-task":
		if p.AllRegisteredRepositories == (len(p.RepositoryIDs) > 0) {
			add(field, "choose all registered repositories or an exact repository allowlist")
		}
		if p.MaxLifetimeSeconds <= 0 || p.MaxLifetimeSeconds > 9223372036 {
			add(field+".max_lifetime_seconds", "must be positive and fit a duration")
		}
		seen := map[int64]bool{}
		for _, id := range p.RepositoryIDs {
			if id <= 0 || seen[id] {
				add(field+".repository_ids", "IDs must be positive and unique")
			}
			seen[id] = true
		}
		tasks := map[string]bool{}
		for _, ref := range p.TaskRefs {
			parsed, err := naming.ParseRef(ref)
			if err != nil || parsed.String() != ref || tasks[ref] {
				add(field+".task_refs", "requires unique canonical exact task references")
			}
			tasks[ref] = true
		}
	default:
		add(field+".mode", "use deny or allow-task")
	}
}
