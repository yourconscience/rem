# rem

The capture and consolidation CLI for the knowledge vault. `make install` at the
repo root builds it to `~/.local/bin`.

```text
rem add [-src harness] "<fact>"   write a note to memory/_inbox
rem search "<query>"              inbox matches plus memsearch results, with paths
rem dream [--apply]               v1 report over ai/; --apply collapses exact
                                  duplicate sync sections with backup + commit
rem dream --v2 [flags]            LLM dream over AgentsView transcripts
rem sync                          run the guarded knowledge-sync binary
```

The store is `$KNOWLEDGE_DIR/memory`: small pages with frontmatter, `[[slug]]`
links and an `_index.md` catalog whose first 4 KB every session loads. Notes in
`memory/_inbox` are labeled unreviewed in search and are never loaded at start.

`rem dream --v2` selects sessions finished since the watermark in
`memory/.dream-state`, skipping active sessions and anything matching
`REM_PRIVATE_PROJECTS`. It renders and redacts each transcript, runs one analyzer
per session and one organizer on a `rem-dream/<date>` branch in `.worktrees/`,
then commits with `reviews/rem-dream-v2-<date>.md`. It never touches `main`; the
branch, the watermark and the inbox cleanup take effect only when you merge.

```text
--limit N       read only the oldest N selected sessions
--since WHEN    use this date or duration instead of the watermark
--session ID    read this session only, without moving the watermark (repeatable)
--parallel N    analyzers running at once (default 4)
--dry-run       print the selection and sizes, run no model
```

The commit is refused when an input has no analyzer result, when anything outside
the dream's own output changed, or when a credential appears in the added lines.
The worktree stays in place for inspection. The runner is `claude`, with file
tools only and writes confined to the memory directory. Any other runner is
refused until its restrictions have been tested.

Proposed changes to `profile/USER.md` land as a separate commit. Changes to
`AGENTS.md`, which lives in another repo, land as a patch beside the report.

Environment:

- `KNOWLEDGE_DIR` — vault root (default `~/Workspace/knowledge`)
- `REM_SYNC_BIN` — alternate `knowledge-sync` binary
- `REM_COLLECTION` — collection for `rem search` (default `ai`)
- `REM_DREAM_RUNNER`, `REM_DREAM_MODEL`, `REM_AGENTS_MD` — see the root README

Tests are included in the root module and run with `go test ./...`.
