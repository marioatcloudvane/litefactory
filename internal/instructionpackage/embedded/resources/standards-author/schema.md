# Response schema

{"schema_version":1,"category_id":"category ID","new_category":{"id":"category ID","name":"name","description":"what belongs in it"},"rule":"MUST ...","description":"rule, the mistake it prevents, how to follow it, compliant and non-compliant example","evidence":"what shows compliance","scope":"stacks, repositories or stage","check":"automated","applies_to":["story-worker"],"questions":[{"text":"question for the owner","options":["plain option"],"follow_up_to":"ID of an earlier question"}],"supersede_question_ids":["ID of your own pending question that no longer matters"],"notes":"what you did and why"}

check is automated, semi_automated or manual. new_category, options, follow_up_to, supersede_question_ids and notes are optional; leave out what you do not use, never send null. Every other field is required; an empty string or list is fine while the draft is incomplete.
