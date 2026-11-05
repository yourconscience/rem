# rem

The capture and consolidation CLI for the knowledge vault. `make install` at the
repo root builds it to `~/.local/bin`.

```text
rem add [-src harness] "<fact>"   capture an inert candidate in ai/YYYY-MM-DD.md
rem search "<query>"              semantic search via memsearch (collection ai)
rem dream [--apply]               report candidates; optionally collapse exact
                                  duplicate sync sections with backup + commit
rem sync                          run the guarded knowledge-sync binary
```

`rem dream` is the only consolidation command. Its normal mode writes a review
report and does not promote facts. `--apply` performs only its documented,
unattended-safe exact-duplicate cleanup; it does not infer or promote facts.
Session digests written by memory hooks remain context records, not durable
profile claims.

Environment:

- `KNOWLEDGE_DIR` — vault root (default `~/Workspace/knowledge`)
- `REM_SYNC_BIN` — alternate `knowledge-sync` binary
- `REM_COLLECTION` — collection for `rem search` (default `ai`)

Tests are included in the root module and run with `go test ./...`.
