# MAR-12 evidence

mar-12-readback.md is the complete owner-supplied Linear GraphQL description from
`issue(id:"MAR-12"){ description }`, read 2026-10-14 00:44 Europe/Vienna.
It is 3304 bytes including the final newline, SHA-256
98e141564f22ff4265dbb84bded3e2c495c457b803c9ad08c9755029e837a88b.
This is owner-provided live evidence, not an independent query or a capture of
the initial 2026-10-13 21:19 response.

mar-12-excerpt.md preserves the earlier reply:6153816990 excerpt, including its
truncation inside the metadata note. It is not a complete description.

Tests reconstruct the expected logical body with report.RenderPlanDraft from
the complete description's structured draft and the original retained request
identity plan-request-708-6832, story-001, parent 4503599627567323. The metadata
matches that request's SHA-256 digest. EncodeContent reconstructs the expected
wire body. Neither reconstructed body is an original captured sent body; the
failing authored write was not retained. The renderer and supplied incident
analysis establish the projection. No missing readback bytes are synthesized.

## Repair attempt: additional mismatch

The reconstructed renderer output still differs after the approved bullet,
scaffold-spacing and table-rule changes. `storyText` in
internal/report/typed_story.go emits numeric entities for punctuation. This
behavior is present both at the incident daemon commit d9c31c5 and the analysis
source bc016249a801073b8127822fb521bba7d5919aa8, as well as current source.
The complete Linear fixture contains literal punctuation instead:

- First differing comparison line: `- Assumption: the actor is the person who reviews invoices in the table&#46;`
  versus `- Assumption: the actor is the person who reviews invoices in the table.`
- Other demonstrated cases: `layout&#45;only` versus `layout-only`, and
  `&#40;separate story&#41;` versus `(separate story)`.

The focused regression and GraphQL update-readback MAR-12 case fail for these
remaining differences. Entity equivalence is not implemented: a broader decode
could turn escaped punctuation into Markdown structure. The repair was returned
for reshape before expanding the approved normalization. Partial comparison and
diagnostic code is retained for review, not declared validated or complete.
Conflict reconciliation is not yet changed.
