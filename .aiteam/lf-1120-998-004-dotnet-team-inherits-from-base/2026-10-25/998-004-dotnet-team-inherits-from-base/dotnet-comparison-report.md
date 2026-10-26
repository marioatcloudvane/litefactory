# Dotnet vs base-team comparison and build proof

Counts found: inherited 25, replaced 24, added 13, excluded 7 (expected 26/23/13/7).
Difference: bug/bug-analyst.md differs from base-team (dotnet has an extra paragraph), so it is a replace entry. base-team was not edited.

## Replaced (24)
- bug/bug-analyst.md
- bug/bug-fixer.md
- bug/bug.dot
- bug/skills/bug-analysis-result/result.md
- bug/skills/bug-repair-result/result.md
- implement/implement.dot
- implement/story-worker.md
- implement/sync-resolver.md
- plan/plan-set.md
- plan/plan-story.md
- plan/plan-technical.md
- plan/skills/plan-evidence/node-result.md
- plan/skills/plan-story-set/SKILL.md
- research/research.md
- research/shaping.md
- spike/spike-fixer.md
- spike/spike-investigator.md
- stack-profile.json
- validate/skills/validate-repair-report/node-result.md
- validate/validate-acceptance.md
- validate/validate-architecture.md
- validate/validate-code.md
- validate/validate-repair.md
- validate/validate.dot

## Excluded (7)
- bug/skills/litefactory-rules/SKILL.md
- bug/skills/litefactory-rules/litefactory.md
- implement/skills/litefactory-rules/SKILL.md
- implement/skills/litefactory-rules/litefactory.md
- plan/skills/plan-go-guidance/SKILL.md
- validate/skills/litefactory-rules/SKILL.md
- validate/skills/litefactory-rules/litefactory.md

## Added (13)
- validate/skills/dotnet-web-engineering/engineering.md
- validate/skills/dotnet-web-engineering/workflow.md
- validate/skills/dotnet-web-engineering/SKILL.md
- plan/skills/blazor-guidance/recipes.md
- plan/skills/blazor-guidance/SKILL.md
- plan/skills/blazor-guidance/compatibility.md
- plan/skills/blazor-guidance/practices.md
- bug/skills/dotnet-web-engineering/engineering.md
- bug/skills/dotnet-web-engineering/workflow.md
- bug/skills/dotnet-web-engineering/SKILL.md
- implement/skills/dotnet-web-engineering/engineering.md
- implement/skills/dotnet-web-engineering/workflow.md
- implement/skills/dotnet-web-engineering/SKILL.md

## Build proof
Temporary repo copy, test-only version 9999.0.0, existing release build: flat source vs slimmed source. All 1085 files under teams/ (excluding source/) were byte-identical, including GENERATED.md, team.config and the catalogue (teams.json). Published releases untouched.
