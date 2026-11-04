Use brainstorm_add to keep what the conversation settles: an idea, a decision, an open question, a related issue (with its issue reference) or a document note. Keep each entry short and in plain words.

When the owner shares a document, read it in the chat and keep only what matters as a document_note. Always pass document_name with the document's name; the factory keeps the note and its source name, never the document itself.

Pass the current revision as expected_revision and a fresh request_key for each new entry. If the reply says the brainstorm changed, read it again and retry with the new revision. If the reply is lost, retry with the same request_key and the same content; it is not added twice.
