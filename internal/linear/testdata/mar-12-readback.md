```litefactory-metadata-v1
lf:plan-story parent=4503599627567323 key=story-001
```

```litefactory-metadata-v1
lf:plan-request digest=ed786d383bf1c84a31dcd0268555de20fb607414cc5d06da8cbaf0ff44b97885
```

# Userstory

As invoice reviewer, I want a simpler Invoices table and a row drawer that fits on screen, so that I can scan invoices and read one invoice's details without clutter or scrolling.

* Assumption: the actor is the person who reviews invoices in the table.
* Assumption: 'height capped' means the drawer never grows taller than the visible view; long content scrolls inside it.
* Assumption: the VAT lines toggle starts closed every time a drawer opens.
* Assumption: this is a layout-only change. Data, saving and behaviour stay as they are.

## What this story solves

The Invoices table is crowded with columns and a legend that the reviewer does not need to scan rows. The row drawer is long: sections stack and VAT lines are always open, so the reviewer has to scroll to see Details and Payment.

## Scope

In-Scope

* Remove the Confidence column from the Invoices table
* Remove the VAT class column from the Invoices table
* Remove the VAT class legend
* Show Details and Payment side by side in the row drawer
* Put VAT lines behind a toggle that is closed by default
* Cap the drawer height so it fits the view

Not in Scope

* Any change to stored data, including Confidence and VAT class values
* Any change to how VAT is classified or calculated
* Changing In/Out inline (separate story)
* New fields, filters or sorting
* Any change to saving, auditing or locking behaviour

# Task

| Task Id | Description | Owner | Dependencies |
| -- | -- | -- | -- |

# Acceptance Criterias

# Architecture

```litefactory-metadata-v1
lf:plan-content v=1 data={"key":"story-001","name":"Simpler Invoices table and drawer","user_story":{"actor":"invoice reviewer","want":"a simpler Invoices table and a row drawer that fits on screen","benefit":"I can scan invoices and read one invoice's details without clutter or scrolling","notes":["Assumption: the actor is the person who reviews invoices in the table.","Assumption: 'height capped' means the drawer never grows taller than the visible view; long content scrolls inside it.","Assumption: the VAT lines toggle starts closed every time a drawer opens.","Assumption: this is a layout-only change. Data, saving and behaviour stay as they are."]},"problem":"The Invoices table is crowded with columns and a legend that the reviewer does not need to scan rows. The row drawer is long: sections stack and VAT lines are always open, so the reviewer has to scroll to see Details and Payment.","in_scope":["Remove the Confidence column from the Invoices table","Remove the VAT class column from the Invoices table","Remove the VAT class legend","Show Details and Payment side by side in the row drawer","Put VAT lines behind a toggle that is closed by default","Cap the drawer height so it fits the view"],"not_in_scope":["Any change to stored data, including Confidence and VAT class values","Any change to how VAT is classified or calculated","Changing In/Out inline (separate story)","New fields, filters or sorting","Any change to saving, auditing or locking behaviour"],"dependencies":{"story_keys":[],"issue_refs":[]},"frontend":true}
```
