# rem

Review-first memory for coding agents, as a standalone package that works beside any config manager.

- **Session start context:** a compact list of recent sessions from [AgentsView](https://github.com/kenn-io/agentsview), plus pinned Claude Desktop sessions when `ccd-sessions` is installed. Anything unavailable degrades to empty context.
- **`rem`:** capture candidate facts, consolidate them into review reports, and search the vault. Candidates stay inert until you promote them.
- **`knowledge-sync`:** guarded commit, merge and push for a git-backed vault, with a best-effort search reindex.
- **Hermes bridge:** optional sync between Hermes memory and the vault.

It does not capture sessions itself. AgentsView indexes each harness's own transcripts.

## Requirements

Go 1.26+ and Python 3 to build and run. AgentsView, `ccd-sessions`, [memsearch](https://github.com/zilliztech/memsearch) and Hermes are optional.

## Install

```bash
make install   # BINDIR=~/.local/bin, REM_HOME=~/.local/share/rem
```

Installing only copies files. No hook runs until you register it.

## Register the hook

### With skillshare

```bash
mkdir -p ~/.config/skillshare/rem
cp integrations/skillshare/session-start.* ~/.config/skillshare/rem/
skillshare hooks add rem-session-start --file ~/.config/skillshare/rem/session-start.yaml
skillshare hooks sync --dry-run
skillshare hooks sync
```

This covers Claude Code, Codex, Factory Droid, Pi and OMP. Delete the bindings you don't use. skillshare has no Hermes hook target, so register Hermes by hand. See [the companion setup](integrations/skillshare/README.md) for Codex roles and the `universal` target.

### By hand

Any harness that accepts a command hook can run `~/.local/share/rem/hooks/session-start.sh` on session start. It reads the hook's JSON payload on stdin and prints a `hookSpecificOutput.additionalContext` reply. Harnesses that run hooks as code, such as Pi and OMP, can use the modules in `integrations/skillshare/`.

For Hermes, add these to your Hermes hooks:

| Hermes event | Command |
|---|---|
| `on_session_start` | `~/.local/share/rem/hooks/sync-vault-to-memory.sh`, `~/.local/share/rem/hooks/basic-session-start.py` |
| `on_session_finalize` | `~/.local/share/rem/hooks/sync-memory-to-vault.sh` |

## Use

```bash
rem add -src claude "prefers pnpm for Node work"
rem dream           # report-only consolidation
rem dream --apply   # guarded exact-duplicate cleanup
rem search "preferences"
rem sync
```

Run `knowledge-sync` on a schedule to keep the vault synced. See [cmd/rem](cmd/rem/README.md), [cmd/knowledge-sync](cmd/knowledge-sync/README.md) and [memsearch](memsearch/README.md).

## Configuration

| Variable | Default | Purpose |
|---|---|---|
| `KNOWLEDGE_DIR` | `~/Workspace/knowledge` | The vault |
| `KNOWLEDGE_REMOTE` | `origin` | Remote for `knowledge-sync` and `rem sync` |
| `KNOWLEDGE_BRANCH` | `main` | Branch for `knowledge-sync` |
| `KNOWLEDGE_CONF` | `~/.config/rem/memsearch.conf` | Optional shell file that sets the vault and memsearch paths |
| `REM_PRIVATE_PROJECTS` | `personal` | Comma-separated project or folder names whose session text never enters another session's context |
| `REM_HOME` | `~/.local/share/rem` | Where the Pi and OMP modules look for the hook |
| `AGENTSVIEW_BIN`, `CCD_SESSIONS_BIN` | found on `PATH` | Tool overrides |

## Development

```bash
make test
```

## License

[MIT](LICENSE)
