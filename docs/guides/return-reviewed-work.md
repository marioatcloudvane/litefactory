# Return reviewed work to Implement

Use `/return -- <explanation>` on the displayed Validate report to ask for a
change, even when its checks pass. If reports have been replaced, copy the short
decision reference displayed with the command. An explicit reference or reply
selects its recorded report; an old report cannot silently select newer work.

The factory retains your explanation, the reviewed commit, report, complete
assessment and frozen stories. It schedules Implement on the same feature
branch. You do not need another command or a mode selection. One responsible
story receives a shared change; the other completed stories stay completed.

The existing coordinator resolver may suggest allocation only when it already
runs before dispatch. The factory validates the keys against the frozen set and
saves the selection before reopening stories. Without usable allocation it picks
a single valid story named in your explanation, otherwise the first story in
frozen plan order. Replays keep that selection and schedule identity.

The selected worker receives your original explanation and the full retained
reviewed context. After its normal completion and merge, the existing automatic
Validate setting controls the next Validate run. `/validate-implement` remains
available for the older finding-based repair command.
