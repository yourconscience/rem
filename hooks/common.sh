#!/usr/bin/env bash

MEMORY_HOOK_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
MEMORY_DIR="$(dirname "$MEMORY_HOOK_DIR")"

load_memory_config() {
  CONF="${KNOWLEDGE_CONF:-${XDG_CONFIG_HOME:-$HOME/.config}/rem/memsearch.conf}"
  KNOWLEDGE_DIR_ENV="${KNOWLEDGE_DIR:-}"
  SESSIONS_DIR_ENV="${SESSIONS_DIR:-}"
  NOTES_DIR_ENV="${NOTES_DIR:-}"
  PROFILE_DIR_ENV="${PROFILE_DIR:-}"
  MEMSEARCH_STATE_DIR_ENV="${MEMSEARCH_STATE_DIR:-}"
  MEMSEARCH_COLLECTION_ENV="${MEMSEARCH_COLLECTION:-}"
  if [ -f "$CONF" ]; then
    # shellcheck source=/dev/null
    . "$CONF"
  fi

  export KNOWLEDGE_DIR="${KNOWLEDGE_DIR_ENV:-${KNOWLEDGE_DIR:-$HOME/agent-memory}}"
  export SESSIONS_DIR="${SESSIONS_DIR_ENV:-${SESSIONS_DIR:-$KNOWLEDGE_DIR/sessions}}"
  export NOTES_DIR="${NOTES_DIR_ENV:-${NOTES_DIR:-$KNOWLEDGE_DIR/notes}}"
  export PROFILE_DIR="${PROFILE_DIR_ENV:-${PROFILE_DIR:-$KNOWLEDGE_DIR/profile}}"
  export MEMSEARCH_STATE_DIR="${MEMSEARCH_STATE_DIR_ENV:-${MEMSEARCH_STATE_DIR:-$HOME/.memsearch/state}}"
  export MEMSEARCH_COLLECTION="${MEMSEARCH_COLLECTION_ENV:-${MEMSEARCH_COLLECTION:-ai}}"

}

prepare_memory_index_env() {
  export MEMSEARCH_MEMORY_DIR="${SESSIONS_DIR}"
  export MEMSEARCH_COLLECTION_NAME="${MEMSEARCH_COLLECTION}"
  # Only the index state dir is created; the vault sessions/ dir is legacy and
  # must not be recreated by hooks.
  mkdir -p "$MEMSEARCH_STATE_DIR"
}

# Best-effort vault reindex fired after a vault sync writes. It never
# blocks the hook (all work is backgrounded), is bounded by a watchdog so a
# hung memsearch can't run forever, and refuses to overlap a refresh that is
# already running via an atomic mkdir lock. No-op when memsearch is absent.
refresh_index_async() {
  command -v memsearch >/dev/null 2>&1 || return 0
  prepare_memory_index_env

  reindex_lock="${MEMSEARCH_STATE_DIR%/}/reindex.lock"
  # mkdir is atomic: it fails when a refresh already holds the lock, so we never
  # spawn overlapping reindexers.
  if ! mkdir "$reindex_lock" 2>/dev/null; then
    # The lock exists. Reclaim it only if its owner is gone (e.g. the refresher
    # was SIGKILLed mid-run), so a dead process can't suppress reindex forever.
    owner="$(cat "$reindex_lock/pid" 2>/dev/null || true)"
    if [ -n "$owner" ] && kill -0 "$owner" 2>/dev/null; then
      return 0
    fi
    rm -f "$reindex_lock/pid" 2>/dev/null || true
    rmdir "$reindex_lock" 2>/dev/null || true
    mkdir "$reindex_lock" 2>/dev/null || return 0
  fi

  (
    trap 'rm -f "$reindex_lock/pid" 2>/dev/null || true; rmdir "$reindex_lock" 2>/dev/null || true' EXIT
    set -- "$NOTES_DIR" "$PROFILE_DIR"
    [ -d "$KNOWLEDGE_DIR/memory" ] && set -- "$@" "$KNOWLEDGE_DIR/memory"
    for path in "$SESSIONS_DIR"/*.md "$SESSIONS_DIR"/*.markdown; do
      [ -f "$path" ] && set -- "$@" "$path"
    done
    memsearch index "$@" --collection "$MEMSEARCH_COLLECTION" >/dev/null 2>&1 &
    index_pid=$!
    ( sleep "${MEMSEARCH_REINDEX_TIMEOUT:-120}"; kill "$index_pid" 2>/dev/null || true ) &
    watchdog_pid=$!
    wait "$index_pid" 2>/dev/null || true
    kill "$watchdog_pid" 2>/dev/null || true
    wait "$watchdog_pid" 2>/dev/null || true
  ) >/dev/null 2>&1 &
  # Record the worker's PID (portable across bash 3.2, unlike $BASHPID) so a
  # later call can tell a live refresh from a lock stranded by a killed one.
  printf '%s\n' "$!" >"$reindex_lock/pid" 2>/dev/null || true

  return 0
}
