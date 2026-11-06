# Owner live run: Linear label setup

These steps check `litefactory repo labels setup <project-id>` against a real Linear
**test** team. Never point it at a production team.

The command already works for GitHub. This story adds Linear. It does this:

- Prints a dry run first. One line per wanted label shows its status (stored,
  adopted, would create, ambiguous, blocking, warning), its scope (team or
  workspace), its form (grouped or flat), its name and its ID.
- Wanted labels: the five Mode labels (Research, Plan, Implement, Validate,
  Spike), one `Coding Agent: <Name>` per registered runner (today Claude, and
  Codex if configured), and `bug`.
- Applies only after you answer `y` at the y/N prompt, or with `--yes`. Without
  a terminal and without `--yes` it writes nothing.
- If no label of a family exists, it creates a group (`Mode` or `Coding Agent`)
  in the team and the children under it. If flat labels such as `Mode: Plan`
  already exist, it creates the missing ones flat too.
- Creates labels only in the team, never at workspace level. Never renames,
  recolours or deletes a label.
- Saves the label IDs on the ticket connection.
- Refuses a second setup run on the same team at the same time.
- A rerun after success says there is nothing to create and saves no new
  connection revision.

## Before you start

1. A Linear test team and a test project in it.
2. A Linear token that can reach that test team.
3. A Linear connection for the project, set with `litefactory repo connection set`
   and pointing at the test team. Check it with `litefactory repo connection list`.
4. A factory build that you build and run yourself.

## Steps

**1. Dry run with no terminal.**
Run `litefactory repo labels setup <project-id> </dev/null` (no `--yes`).
Expect the per-label lines and a note that nothing was applied.
Check in Linear that no label was added, and run `litefactory repo connection list`
to see the connection revision did not change.

**2. Interactive run.**
Run `litefactory repo labels setup <project-id>` in a normal terminal.
Read every per-label line: status, scope, form, name, ID. On a fresh test team
expect "would create", scope team, form grouped for Mode and Coding Agent.
Answer `y`.

**3. Check the labels in Linear.**
Open the **team** label settings (not the workspace settings). Expect:
- a `Mode` group with Research, Plan, Implement, Validate, Spike under it;
- a `Coding Agent` group with one child per runner (Claude, and Codex if set);
- `bug` as a flat label, or Linear's default Bug label adopted instead.
Check nothing new appeared in the workspace label settings.

**4. Group exclusivity.**
On a test issue, apply Mode › Plan, then Mode › Implement. Only one Mode label
should remain. Do the same for the Coding Agent group. This needs two children:
either two runners are registered, or you add a second child to the group by
hand for this test. If you add one by hand, removing it afterwards is your own
action; the factory never deletes labels.

**5. Connection check.**
Run `litefactory repo connection check <project-id>`. Expect no blocking lines,
no missing labels and no runner without a label.

**6. Rerun.**
Run `litefactory repo connection list` and note the revision. Run setup again.
Expect "nothing to create". Run `litefactory repo connection list` again; the
revision must be the same.

**7. Optional: ambiguity.**
In a fresh test team (with a connection pointing at it), create a flat label
`Mode: Plan` in the team and a second `Mode: Plan` at workspace level, by hand.
Run setup. Expect an ambiguity report listing both labels with their scope,
form and ID, and that nothing is written (check Linear and the connection
revision). One hand-made label on its own is not ambiguous: setup adopts it.

## What to report

For each step: pass or fail, and paste the printed output.

Report these specially:
- **Group creation failed** (an error from Linear, or children not under the
  group): copy the exact failure line. The agreed fallback is a small follow-up
  that creates flat team labels instead.
- Any label created at workspace level, or any label renamed, recoloured or
  deleted by the factory.
- Anything written during step 1 or step 7.
- A changed connection revision in step 6.

## Known limit

Linear has no lock. If another person creates a matching label in the short gap
between setup's last read and its create call, setup cannot prevent it. The next
run adopts that label or reports an ambiguity. This is expected, not a failure.
