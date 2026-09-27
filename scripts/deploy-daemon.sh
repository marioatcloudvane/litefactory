#!/usr/bin/env bash
#
# Build litefactory from a clean checkout and redeploy the launchd daemon.
#
# The build never uses the working tree: that may carry uncommitted changes,
# migrations included, which would apply to the live database on restart. The
# ref is checked out into a temporary worktree and built there.
#
# The new binary replaces dist/litefactory by rename, never by copy. Copying
# over the binary the daemon runs from invalidates it on macOS: every later
# invocation dies with SIGKILL (exit 137) and prints nothing, while the old
# daemon keeps running.
#
# Steps: build, stop the daemon, back up the database, keep the old binary as
# dist/litefactory.prev-<timestamp>, rename the new one into place, start, and
# verify a new pid runs the new binary.
#
# Usage: scripts/deploy-daemon.sh [git-ref]    (default: origin/main)
#
# From a screen-login Terminal the launchd agent is managed with launchctl.
# From SSH the GUI domain is unreachable, even as root. There the daemon is
# stopped with SIGTERM (KeepAlive is off, so launchd does not restart it) and
# started detached with the plist's environment, after unlocking the login
# keychain that holds its token.

set -euo pipefail

REF="${1:-origin/main}"
LABEL="com.github.cloudvane.litefactory"
DOMAIN="gui/$(id -u)"
PLIST="$HOME/Library/LaunchAgents/$LABEL.plist"
LF_HOME="${LF_HOME:-$HOME/.litefactory}"
DB="$LF_HOME/litefactory.db"
REPO="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO/dist/litefactory"
STAMP="$(date +%Y%m%d-%H%M%S)"

if launchctl print "$DOMAIN" >/dev/null 2>&1; then ROUTE=launchctl; else ROUTE=ssh; fi

say() { echo "deploy: $*"; }
die() { echo "deploy: $*" >&2; exit 1; }

daemon_pid() { pgrep -f "^$BIN daemon run" || true; }

[ -f "$PLIST" ] || die "no launch agent at $PLIST"
[ -f "$DB" ] || die "no database at $DB"
command -v sqlite3 >/dev/null || die "sqlite3 is not installed"

stop_daemon() {
	if [ "$ROUTE" = launchctl ]; then
		launchctl bootout "$DOMAIN/$LABEL" || say "bootout failed; waiting in case the daemon exits anyway"
	else
		pkill -TERM -f "^$BIN daemon run" || true
	fi
}

start_daemon() {
	if [ "$ROUTE" = launchctl ]; then
		launchctl bootstrap "$DOMAIN" "$PLIST"
	else
		local agent_path
		agent_path="$(/usr/libexec/PlistBuddy -c "Print :EnvironmentVariables:PATH" "$PLIST")"
		(cd / && /usr/bin/env -i HOME="$HOME" USER="$USER" LOGNAME="$USER" SHELL="${SHELL:-/bin/zsh}" TMPDIR="${TMPDIR:-/tmp/}" LF_HOME="$LF_HOME" PATH="$agent_path" \
			nohup "$BIN" daemon run >>"$LF_HOME/logs/launchd.out.log" 2>>"$LF_HOME/logs/launchd.err.log" </dev/null &)
	fi
}

say "route: $ROUTE"
if [ "$ROUTE" = ssh ]; then
	say "unlock the login keychain (your login password); the daemon reads its token from it"
	security unlock-keychain "$HOME/Library/Keychains/login.keychain-db"
fi

# ---- Build ----------------------------------------------------------------
say "fetching origin"
git -C "$REPO" fetch -q origin
SHA="$(git -C "$REPO" rev-parse --verify --short "$REF^{commit}")" || die "unknown ref $REF"
if ! git -C "$REPO" merge-base --is-ancestor "$SHA" origin/main; then
	say "warning: $REF ($SHA) is not on origin/main"
fi

WORK="$(mktemp -d "${TMPDIR:-/tmp}/lf-deploy.XXXXXX")"
cleanup() {
	git -C "$REPO" worktree remove --force "$WORK/src" >/dev/null 2>&1 || true
	rm -rf "$WORK"
}
trap cleanup EXIT

say "building $REF ($SHA) in a clean worktree"
git -C "$REPO" worktree add -q --detach "$WORK/src" "$SHA"
make -C "$WORK/src" build >/dev/null
BUILT="$WORK/src/dist/litefactory"
[ -x "$BUILT" ] || die "build produced no binary"

if [ -x "$BIN" ] && cmp -s "$BUILT" "$BIN"; then
	say "dist/litefactory already is this build; nothing to deploy"
	exit 0
fi

# Stage next to the target so the final mv is a same-filesystem rename.
cp "$BUILT" "$BIN.new"

# ---- Stop -----------------------------------------------------------------
OLD_PID="$(daemon_pid)"
say "stopping the daemon${OLD_PID:+ (pid $OLD_PID)}"
stop_daemon
for _ in $(seq 1 30); do
	[ -z "$(daemon_pid)" ] && break
	sleep 1
done
[ -z "$(daemon_pid)" ] || die "daemon still running after 30s; $BIN.new is staged, nothing replaced"

# ---- Back up and swap -----------------------------------------------------
BACKUP="$DB.bak-pre-deploy-$STAMP"
say "backing up the database to $BACKUP"
sqlite3 "$DB" ".backup '$BACKUP'"

if [ -e "$BIN" ]; then
	cp -p "$BIN" "$BIN.prev-$STAMP"
	say "kept the old binary as dist/litefactory.prev-$STAMP"
fi
mv -f "$BIN.new" "$BIN"

# ---- Start and verify -----------------------------------------------------
say "starting the daemon"
start_daemon
NEW_PID=""
for _ in $(seq 1 15); do
	NEW_PID="$(daemon_pid)"
	[ -n "$NEW_PID" ] && break
	sleep 1
done

rollback="to roll back: stop the daemon, mv -f $BIN.prev-$STAMP $BIN, and rerun its start ($ROUTE route)"
[ -n "$NEW_PID" ] || die "daemon did not start; see $LF_HOME/logs/launchd.err.log; $rollback"
[ "$NEW_PID" != "$OLD_PID" ] || die "pid did not change ($NEW_PID); $rollback"
cmp -s "$BUILT" "$BIN" || die "dist/litefactory does not match the build; $rollback"

sleep 3
[ -n "$(daemon_pid)" ] || die "daemon exited right after start; see $LF_HOME/logs/launchd.err.log; $rollback"

say "deployed $SHA; daemon running as pid $NEW_PID"
say "$rollback"
