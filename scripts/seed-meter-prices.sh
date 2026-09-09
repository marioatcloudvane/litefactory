#!/usr/bin/env bash
#
# Seed the litefactory meter price catalog.
#
# Rates are USD per million tokens, taken from the providers' own pricing pages
# on 2026-09-09:
#
#   Claude  https://platform.claude.com/docs/en/about-claude/pricing
#   OpenAI  https://developers.openai.com/api/docs/pricing
#
# Each `meter prices set` writes a complete immutable catalog revision, so this
# script creates one revision per model and the last one wins. Re-running it is
# safe — it just adds more revisions. Prices are pinned to an attempt before it
# launches, so seed BEFORE starting the daemon: work metered under an empty
# catalog stays unpriced forever.
#
# Usage: scripts/seed-meter-prices.sh [path-to-litefactory-binary]

set -euo pipefail

LF="${1:-./dist/litefactory}"

if ! command -v "$LF" >/dev/null 2>&1 && [ ! -x "$LF" ]; then
	echo "seed-meter-prices: no litefactory binary at '$LF'" >&2
	echo "usage: $0 [path-to-litefactory-binary]" >&2
	exit 1
fi

set_price() {
	echo "  $1"
	"$LF" meter prices set "$@" >/dev/null
}

# --- Codex / OpenAI -----------------------------------------------------------
#
# OpenAI has no cache-TTL split, so only the generic --cache_write is set; the
# meter falls back to it for writes it cannot decompose into 5m/1h buckets.
# Cache writes are 1.25x uncached input on GPT-5.6 and later.
#
# Long-context rates (roughly 2x, past the standard window) are a separate tier
# that the catalog cannot express — estimates for very large attempts will read
# low. Standard-window rates are seeded here.

echo "Codex (OpenAI):"

# GPT-6 Astra — flagship, released 2026-09-03.
set_price gpt-6-astra --coding_agent codex \
	--input 10 --output 50 --cache_read 1 --cache_write 12.50

# GPT-5.6 Sol — $4/$20 is promotional, held at least through 2026-11-21.
# Standard list is $5/$30; re-seed after the promo ends or new attempts will
# be under-estimated.
set_price gpt-5.6-sol --coding_agent codex \
	--input 4 --output 20 --cache_read 0.40 --cache_write 5

set_price gpt-5.6-terra --coding_agent codex \
	--input 2 --output 12 --cache_read 0.20 --cache_write 2.50

set_price gpt-5.6-luna --coding_agent codex \
	--input 0.20 --output 1.20 --cache_read 0.02 --cache_write 0.25

# `gpt-5.6` is documented as an alias for Sol; seed it in case that is what
# lands in the rollout records.
set_price gpt-5.6 --coding_agent codex \
	--input 4 --output 20 --cache_read 0.40 --cache_write 5

# --- Claude -------------------------------------------------------------------
#
# Anthropic bills cache writes by TTL: 1.25x input for 5m, 2x input for 1h.
# The generic --cache_write is set to the 5m rate, which is the default TTL and
# what undecomposed writes are most likely to be.
#
# Cache reads are 0.1x input on every model except Fable 5.1, which is 0.025x.

echo "Claude:"

# Current lineup.
set_price claude-opus-5 --coding_agent claude \
	--input 5 --output 25 --cache_read 0.50 \
	--cache_write 6.25 --cache_write_5m 6.25 --cache_write_1h 10

set_price claude-fable-5-1 --coding_agent claude \
	--input 10 --output 50 --cache_read 0.25 \
	--cache_write 12.50 --cache_write_5m 12.50 --cache_write_1h 20

set_price claude-sonnet-5 --coding_agent claude \
	--input 2 --output 10 --cache_read 0.20 \
	--cache_write 2.50 --cache_write_5m 2.50 --cache_write_1h 4

set_price claude-haiku-4-5 --coding_agent claude \
	--input 1 --output 5 --cache_read 0.10 \
	--cache_write 1.25 --cache_write_5m 1.25 --cache_write_1h 2

# Haiku 4.5 is the one current model whose Claude API ID carries a date; the
# collector records whatever the message reports, so seed both forms.
set_price claude-haiku-4-5-20251001 --coding_agent claude \
	--input 1 --output 5 --cache_read 0.10 \
	--cache_write 1.25 --cache_write_5m 1.25 --cache_write_1h 2

# Claude Code reports the 1M-context Opus 5 variant with a bracket suffix.
# Same rates — 4.6 and later bill the full window at standard prices.
set_price 'claude-opus-5[1m]' --coding_agent claude \
	--input 5 --output 25 --cache_read 0.50 \
	--cache_write 6.25 --cache_write_5m 6.25 --cache_write_1h 10

# Legacy models, still served and still reachable from an older session.
set_price claude-fable-5 --coding_agent claude \
	--input 10 --output 50 --cache_read 1 \
	--cache_write 12.50 --cache_write_5m 12.50 --cache_write_1h 20

set_price claude-opus-4-8 --coding_agent claude \
	--input 5 --output 25 --cache_read 0.50 \
	--cache_write 6.25 --cache_write_5m 6.25 --cache_write_1h 10

set_price claude-opus-4-7 --coding_agent claude \
	--input 5 --output 25 --cache_read 0.50 \
	--cache_write 6.25 --cache_write_5m 6.25 --cache_write_1h 10

set_price claude-opus-4-6 --coding_agent claude \
	--input 5 --output 25 --cache_read 0.50 \
	--cache_write 6.25 --cache_write_5m 6.25 --cache_write_1h 10

set_price claude-sonnet-4-6 --coding_agent claude \
	--input 3 --output 15 --cache_read 0.30 \
	--cache_write 3.75 --cache_write_5m 3.75 --cache_write_1h 6

echo
echo "Seeded. Current revision:"
"$LF" meter prices list
