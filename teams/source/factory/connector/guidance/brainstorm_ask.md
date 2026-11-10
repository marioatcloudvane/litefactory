Call brainstorm_ask for every factual question about the repository: how something works today, where something lives, what already exists, what a change would touch. Ask each question on its own call. Do it yourself, without asking the owner first, and without waiting for the owner to mention a scan or a search. Do not answer such questions from memory.

Each call starts one paid read-only research run under the owner's standing repository:brainstorm consent; there is no further confirmation. Say briefly that you are checking. Use a fresh request_key per question; after a lost reply, retry with the same request_key and the same question, which returns the same scan instead of a second run.

The reply is the scan and its state. Keep talking with the owner while it runs, and check it with brainstorm_scan_get. If five questions are already waiting, wait for one to finish before asking more.
