// One conversation, one bounded fixer invocation, automatic factory delivery.
// Questions and all stopping outcomes park at their origin. Owner guidance
// resumes that origin once; only exact approval releases an unapproved repair.
digraph bug {
 graph [team="builtin",workflow="bug",profile_version="2",start="conversation"];
 conversation [lf_action="lf_loop",lf_contract="owner_conversation",lf_outcomes="analyse,approved,blocked",lf_limit="64",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 analyst [lf_action="lf_agent",lf_agent_markdown="bug-analyst",lf_skills="bug-analysis-result",lf_model_tier="flagship",lf_access="read",lf_response="result",lf_outcomes="ask,done,blocked",lf_limit="8",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 fixer [lf_action="lf_agent",lf_agent_markdown="bug-fixer",lf_skills="bug-repair-result",lf_response="result",lf_outcomes="succeeded,reshape,blocked",lf_limit="3",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted"];
 finalization [lf_action="lf_bug_finalize",lf_owner_continuation="required",lf_outcomes="completed,revalidate,stale,blocked",lf_limit="16",lf_park="owner",lf_stop="blocked,failed,invalid,exhausted,stale"];
 completed [lf_terminal="true"];
 conversation -> analyst [lf_on="analyse"];
 conversation -> fixer [lf_on="approved"];
 analyst -> conversation [lf_on="ask"];
 analyst -> conversation [lf_on="done"];
 fixer -> conversation [lf_on="reshape"];
 fixer -> finalization [lf_on="succeeded"];
 finalization -> fixer [lf_on="revalidate"];
 finalization -> completed [lf_on="completed"];
}
