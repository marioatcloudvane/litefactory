# Run 0017 — scripts for #230 and #259

Delivered session-local upgrade-230.sh and upgrade-259.sh, sharing upgrade_pending.py. See US-009-scripts-230-259.md for commands. Default preview uses supported factory CLI; explicit apply requires retained matching preview and unchanged fresh preview. Scripts target only the two requested tasks; all other open tasks ignored.

10 mock behavioral tests passed. Shell/Python syntax and help checks passed. gofmt listed no files. make check passed vet/pinned lint, then full race suite timed out after 360 seconds; aggregate gate incomplete. No live preview/apply or migration performed, no runtime state edits, no product feature, no legacy-reader removal. US-009 remains blocked; submitted checkpoint request cca32cf26fad478fe49dd2b9520bb64a, receipt 165.
