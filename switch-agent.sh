#!/bin/bash
#
# switch-agent.sh — move a session's conversation to a different coding agent.
#
# litefactory refuses a mid-conversation agent switch on purpose: prepare()
# compares the resolved agent against the one the segment is already owned by
# and returns an agentSwitchRefusal rather than silently starting a second
# conversation with a different tool (US-3, internal/turn/prepare.go). Changing
# the "Coding Agent:" label on an issue that has already run therefore does
# nothing except produce a refusal comment.
#
# This is the deliberate override for that guard. It backs the database up
# first, then rewrites the segment's owning agent so the next run is accepted.
# The next run cold-starts: resumeDecision needs a resume_token the new tool
# has never issued, so there is no transcript to continue. Context comes from
# the issue body and thread comments litefactory assembles into the prompt,
# which is usually what you want when handing work to a different tool.
#
# Usage:  ./switch-agent.sh <session-id> <agent>
#         ./switch-agent.sh 34 codex
#         ./switch-agent.sh --list
#
set -euo pipefail

LF_HOME="${LF_HOME:-$HOME/.litefactory}"
DB="$LF_HOME/litefactory.db"

die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v sqlite3 >/dev/null || die "sqlite3 is not on PATH"
[ -f "$DB" ] || die "no database at $DB (set LF_HOME?)"

q() { sqlite3 "$DB" "$@"; }

if [ "${1:-}" = "--list" ]; then
	printf 'sessions and the agent each conversation is owned by:\n\n'
	sqlite3 -header -column "$DB" "
		select s.id            as session,
		       s.task_ref      as task,
		       g.agent         as owned_by,
		       coalesce(g.resume_token, '-')             as resume_token,
		       (select count(*) from runs r
		         where r.segment_id = g.id)              as runs
		  from sessions s
		  join segments g on g.session_id = s.id
		 order by s.id;"
	exit 0
fi

[ $# -eq 2 ] || die "usage: $0 <session-id> <agent>   (or --list)"
SESSION="$1"
AGENT="$2"

case "$SESSION" in
	''|*[!0-9]*) die "session id must be a number, got '$SESSION'" ;;
esac

# The registry is built at daemon startup from config, so this script cannot
# ask it what is registered. These are the two implementations that exist.
case "$AGENT" in
	claude|codex) ;;
	*) die "agent must be 'claude' or 'codex', got '$AGENT'" ;;
esac

# A run in flight holds the worktree and will write a runs row for the agent it
# was prepared with. Switching underneath it would leave the two disagreeing.
if pgrep -f 'litefactory daemon (run|start)' >/dev/null 2>&1; then
	die "the litefactory daemon is running; stop it before switching agents"
fi

CURRENT=$(q "select agent from segments where session_id = $SESSION;")
[ -n "$CURRENT" ] || die "no segment for session $SESSION (does the session exist?)"

TASK=$(q "select task_ref from sessions where id = $SESSION;")

printf 'session %s  %s\n' "$SESSION" "$TASK"
printf '  currently owned by : %s\n' "$CURRENT"
printf '  switching to       : %s\n' "$AGENT"

if [ "$CURRENT" = "$AGENT" ]; then
	printf '\nnothing to do — already owned by %s.\n' "$AGENT"
	exit 0
fi

STAMP=$(date +%Y%m%d-%H%M%S)
BACKUP="$DB.bak-switch-$SESSION-$STAMP"

# .backup rather than cp: it takes a read lock and produces a consistent file
# even if something else has the database open.
q ".backup '$BACKUP'"
[ -f "$BACKUP" ] || die "backup failed; nothing was changed"
printf '\nbacked up to %s (%s bytes)\n' "$BACKUP" "$(wc -c < "$BACKUP" | tr -d ' ')"

# resume_token is cleared with the same statement, not left behind: it names a
# conversation belonging to the old tool, and leaving it would let a later run
# hand the new tool a thread id it never issued.
q "begin immediate;
   update segments
      set agent = '$AGENT',
          resume_token = null
    where session_id = $SESSION;
   commit;"

AFTER=$(q "select agent from segments where session_id = $SESSION;")
[ "$AFTER" = "$AGENT" ] || die "update did not take (agent is still '$AFTER'); restore from $BACKUP"

printf 'done — session %s is now owned by %s, resume token cleared.\n\n' "$SESSION" "$AGENT"
sqlite3 -header -column "$DB" "
	select id, session_id, ordinal, agent, coalesce(resume_token,'-') as resume_token
	  from segments where session_id = $SESSION;"

printf '\nThe next run on this issue cold-starts. To undo:\n  cp %s %s\n' "$BACKUP" "$DB"
