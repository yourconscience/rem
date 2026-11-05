#!/usr/bin/env bash
set -eu

. "$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/common.sh"

if [ "${MEMSEARCH_SKIP_CLAUDE_HOOKS:-}" = "1" ]; then
  echo '{}'
  exit 0
fi

load_memory_config
# Recent-session context comes from AgentsView for every harness; the vault
# sessions/ directory is no longer read or written by memory hooks.
exec python3 "$MEMORY_DIR/hooks/basic-session-start.py"
