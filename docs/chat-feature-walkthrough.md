# Confirming a feature from Discord

In an allowed Discord channel, signed in as the configured owner:

1. Use `/ask` about a registered project and discuss a feature idea. Ask for
   `/draft` with that project and an optional note (up to 2000 bytes).
2. Check that one message shows the project, complete title and body, and an
   eight-character code. Check that the draft represents the idea. No issue
   should exist yet.
3. Use `/draft note:make the title shorter`. Check the revised preview and new
   code. Confirm the old code; the reply should refuse it and show the new code.
4. Send a separate `/confirm code:<new code>` within 24 hours. Open its issue
   link. Check the Summary, Problem, Wanted outcome and Origin sections and that
   the title and body match the preview. Check that the issue has no labels,
   assignees, milestone or issue type and no factory work has started.
5. Confirm the new code again. Check that the reply returns the same issue link
   and there is exactly one issue.

The owner judges whether the text captures the idea and the replies are clear.
This walkthrough has not been performed against a live Discord channel. Offline
checks cover expiry, replacement, duplicate delivery, concurrent confirmations,
known failures and lost-response recovery; they do not establish live acceptance.
