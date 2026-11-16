# rem

The capture and consolidation CLI for the knowledge vault. `make install` at the
repo root builds it to `~/.local/bin`.

```text
rem add [-src harness] "<fact>"   write a note to memory/_inbox, as an entry line
rem search "<query>"              inbox matches plus memsearch results, with paths
rem dream [--apply]               v1 report over ai/; --apply collapses exact
                                  duplicate sync sections with backup + commit
rem dream --v2 [flags]            LLM dream over AgentsView transcripts
rem migrate                       move an older vault to MEMORY.md on a branch
rem sync                          run the guarded knowledge-sync binary
```

The vault follows the [Agent Memory Repo](https://github.com/AgentMemoryRepo/agentmemoryrepo)
format. `$KNOWLEDGE_DIR/MEMORY.md` is the entry point and its first 4 KB is loaded
at every session start. Under `## Index` it lists one `- [[memory/slug]]: one
sentence` line per page, grouped under `### <domain>` and `### project: <name>`.
Pages live in `memory/`. Each fact is one line, `- <fact> [source: <session id>;
added: YYYY-MM-DD]`, and links start at the vault root without `.md`. Pages carry
no frontmatter; older pages keep theirs. Notes in `memory/_inbox` are written as
the same entry line, labeled unreviewed in search and never loaded at start.

`rem add` creates a missing or empty vault first: `git init -b main`, a
`MEMORY.md` skeleton, and a commit of it when git has `user.name` and
`user.email`. A vault that already has files is used as it is.

`rem migrate` moves a vault with `memory/_index.md` and `[[slug]]` links to this
shape. It makes no model call. It refuses on a vault that is not a git repository
or has uncommitted changes, commits the change on a `rem-migrate/<date>` branch,
and prints how to review and merge it. It never writes to `main` and never
pushes. On a migrated vault it says so and changes nothing. Until you migrate,
the hook reads `memory/_index.md` when there is no `MEMORY.md`. A dream on an
older vault starts its branch with the same migration as its first commit.

`rem dream --v2` selects sessions finished since the watermark in
`memory/.dream-state`, skipping active sessions and anything matching
`REM_PRIVATE_PROJECTS`. It renders and redacts each transcript, runs one analyzer
per session and one organizer on a `rem-dream/<date>` branch in `.worktrees/`,
then commits with `reviews/rem-dream-v2-<date>.md`. The organizer edits a copy of the
index, `memory/.dream/MEMORY.md`, because its writes stay inside `memory/`; rem
installs the copy as `MEMORY.md`. It never touches `main`; the
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
The report warns about a missing or misshapen `MEMORY.md`, links that resolve to
nothing (both `[[memory/slug]]` and the older `[[slug]]` count) and pages the
index omits.
The worktree stays in place for inspection. The runner is `claude`, with file
tools only and writes confined to the memory directory. Any other runner is
refused until its restrictions have been tested.

Proposed changes to `profile/USER.md` land as a separate commit. Changes to
`AGENTS.md`, which lives in another repo, land as a patch beside the report.

Environment:

- `KNOWLEDGE_DIR`: vault root (default `~/agent-memory`, created on the first write)
- `REM_SYNC_BIN`: alternate `knowledge-sync` binary
- `REM_COLLECTION`: collection for `rem search` (default `ai`)
- `REM_DREAM_RUNNER`, `REM_DREAM_MODEL`, `REM_AGENTS_MD`: see the root README

Tests are included in the root module and run with `go test ./...`.
