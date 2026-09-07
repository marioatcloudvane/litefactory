# Spike — disk-growth

2026-09-06. "Check why the disk space keeps on growing fast again." Measure what
is actually consuming and actively growing on this machine.

## What was done

Read-only measurement only, no deletions:

- `df -h`, `diskutil apfs list` for authoritative volume accounting.
- `du -sh` sweeps over `/`, `/Users/mariomh` (including dotfiles — the first pass
  missed them because `du -sh *` skips hidden entries), `Library`, and
  `/System/Volumes/Data`.
- `find -mtime -2` piped through `stat` to attribute *recently written* bytes to
  directories, which is what "growing fast" actually means.
- `docker system df`, `tmutil listlocalsnapshots`, `lsof | grep deleted`.

## What was found

**Volume accounting** (`/dev/disk3s5`, the Data volume):

| | |
|---|---|
| Container capacity | 245.1 GB |
| In use by all volumes | 190.5 GB (77.7%) |
| Data volume consumed | 139.6 GB |
| Free | ~51 GB |

**What `du` can see on the Data volume — ~79 GB:**

| Path | Size |
|---|---|
| `/Users/mariomh` | 41 GB |
| `/Applications` | 17 GB |
| `/System/Volumes/Data/System` | 9.9 GB |
| `/private` | 6.0 GB |
| `/Library` | 4.3 GB |
| `/opt` | 1.1 GB |

**Inside the home directory — the top offenders:**

| Path | Size | Note |
|---|---|---|
| `Library/Application Support/Claude/vm_bundles` | 7.2 GB | Claude Desktop VM bundles |
| `Library/Containers/com.docker.docker` | 7.3 GB | `Docker.raw`, sparse: 228 GB apparent / ~7.3 GB real |
| `~/.openclaw` | 6.5 GB | 3.8 GB `agents/`, 1.0 GB `browser/`, ~1.0 GB workspaces |
| `Library/Caches` | 3.7 GB | 2.1 GB of it is `go-build` |
| `Library/Application Support/Claude/local-agent-mode-sessions` | 1.4 GB | |
| `Library/Caches/Google` | 1.1 GB | |
| `~/go` | 583 MB | module cache |
| `~/.claude/projects` | 356 MB | session transcripts |

**The confirmed fast grower: the Go build cache.**

`~/Library/Caches/go-build` holds **19,931 files, of which 19,929 were written in
the last 24 hours** — i.e. essentially the entire 2.1 GB cache is one day old. Of
all bytes written across the home directory in the last 2 days, `Library/Caches`
accounts for 2.25 GB, more than every other non-Docker directory combined
(`go/pkg` 494 MB, `.nvm` 380 MB, `.npm/_cacache` 177 MB, `.claude/projects`
69 MB, `.lf-worktrees` 50 MB, `.litefactory/{claude-home,logs}` 68 MB).

This is consistent with the harness: 28 worktrees under `~/.lf-worktrees/litefactory`
(`task-1` … `task-101`), each a separate build root, so agent runs recompile the
world instead of hitting a warm cache.

**Docker is large but not the grower.** `docker system df` reports 4.2 GB images
(100% reclaimable), 1.6 GB volumes, 22 MB containers. `Docker.raw`'s 228 GB is
apparent size on a sparse file, not consumed space — but note it never shrinks on
its own once it has grown.

**Swap:** 12 files in `/System/Volumes/VM`, ~11.5 GB total, the newest written
today. Root-owned, so `du` did not count it.

**Not a leak:** no significant deleted-but-open files (`lsof` top entry is a
35 MB system ICU file). No Time Machine data snapshots — only three
`com.apple.os.update-*` snapshots.

## Decisions worth remembering

- Growth is **cache churn, not accumulation of one big artifact.** The go-build
  cache is fully rewritten daily. That is why it "grows fast again" after every
  cleanup: deleting it buys 2 GB back and it refills within a day.
- The per-task worktree layout is the amplifier. Each of the 28 worktrees is a
  distinct build root; Go's content-addressed cache helps, but changed source
  across worktrees still produces distinct entries.
- `du -sh *` in a home directory is misleading — it silently skips dotfiles, which
  here hid 9.5 GB (`.openclaw` alone was 6.5 GB).

## Not done

**~50 GB of the Data volume is unaccounted for and could not be measured.**
`du` finds 79 GB; the volume reports 139.6 GB consumed. ~11.5 GB of the gap is
swap. The rest sits in root-owned directories that `du` silently skipped, most
importantly:

```
/System/Volumes/Data/.DocumentRevisions-V100   d--x--x--x root:wheel
/System/Volumes/Data/.Spotlight-V100           drwx--x--- root:_mds_stores
/System/Volumes/Data/.fseventsd                drwx------ root:wheel  (808 entries, modified today)
```

`.DocumentRevisions-V100` (macOS document version history) is the prime suspect:
it grows with file-edit volume, which is exactly what an agent harness generates,
and it is unbounded in practice. This was **not** confirmed — measuring it needs
root, and `sudo` requires a password in this non-interactive session.

Nothing was deleted. No cleanup was performed.

## Recommendation

**First, close the measurement gap.** One command, needs the password:

```
sudo du -shx /System/Volumes/Data/.DocumentRevisions-V100 \
             /System/Volumes/Data/.Spotlight-V100 \
             /System/Volumes/Data/.fseventsd \
             /System/Volumes/Data/private/var 2>/dev/null
```

If `.DocumentRevisions-V100` comes back at tens of GB, that is the real answer to
"grows fast" and the go-build cache is a sideshow.

**Safe reclaims available now, ~13 GB, no root needed:**

| Command | Reclaims |
|---|---|
| `go clean -cache` | 2.1 GB (refills in a day — treat as maintenance, not a fix) |
| `docker image prune -a` | up to 4.2 GB (all images reported reclaimable) |
| prune stale `~/.lf-worktrees/litefactory/task-*` | some of 155 MB, and reduces future cache churn |
| review `~/.openclaw/agents` (3.8 GB) and `browser` (1.0 GB) | up to 4.8 GB |
| review `Claude/vm_bundles` (7.2 GB) — old VM bundles | unknown |

**Do not** shrink `Docker.raw` blindly; it is sparse and currently costs 7.3 GB
real, not 228 GB.

**Next mode:** none required if the sudo measurement explains the gap and the
reclaims above are enough. If the fix turns out to be structural — bounding the
go-build cache per worktree, or teaching the harness to garbage-collect
`~/.lf-worktrees` and `~/.litefactory/claude-home` — that is a change to the
harness and needs **Research**, not another spike.
