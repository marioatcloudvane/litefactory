## Goal and scope
{{range .Goal}}
- {{.ID}}: {{.Text}}
{{- else}}
- none recorded
{{- end}}

## Non-goals
{{range .NonGoals}}
- {{.ID}}: {{.Text}}
{{- else}}
- none recorded
{{- end}}

## Must not change
{{range .MustNotChange}}
- {{.ID}}: {{.Text}}
{{- else}}
- none recorded
{{- end}}

## Open questions
{{range .OpenQuestions}}
- {{.ID}}: {{.Text}}
{{- else}}
- none recorded
{{- end}}

## Size and impact

- Size: {{.Size}}
- Impact: {{.Impact}}

## Brainstorm summary

{{.Summary}}

## Related issues and possible duplicates
{{range .RelatedIssues}}
- Related: {{.}}
{{- end}}
{{- if .DuplicateWarning}}
- Duplicate warning: {{.DuplicateWarning}}
{{- end}}
{{- range .PossibleDuplicates}}
- Possible duplicate: {{.}}
{{- end}}
{{- if .DuplicateCheck}}
- {{.DuplicateCheck}}
{{- end}}

## Sources
{{range .Sources}}
- {{.}}
{{- else}}
- none
{{- end}}

{{.Marker}}
