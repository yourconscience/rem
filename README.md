# rem

Review-first memory for coding agents, as a standalone package that works beside any config manager.

- **Session start context:** the head of the memory index (at most 4 KB), a compact list of recent sessions from [AgentsView](https://github.com/kenn-io/agentsview), and pinned Claude Desktop sessions when a sessions tool is installed. Anything unavailable degrades to empty context.
- **`rem`:** capture notes that are searchable at once, dream over past sessions onto a reviewable branch, and search the vault. Only a branch you merge reaches the memory sessions load.
- **`knowledge-sync`:** guarded commit, merge and push for a git-backed vault, with a best-effort search reindex.

It does not capture sessions itself. AgentsView indexes each harness's own transcripts.

## Requirements

Go 1.26+ and Python 3 to build and run. AgentsView and [memsearch](https://github.com/zilliztech/memsearch) are optional.

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

This covers Claude Code, Codex, Factory Droid, Pi and OMP. Delete the bindings you don't use. See [the companion setup](integrations/skillshare/README.md) for Codex roles and the `universal` target.

### By hand

Any harness that accepts a command hook can run `~/.local/share/rem/hooks/session-start.sh` on session start. It reads the hook's JSON payload on stdin and prints a `hookSpecificOutput.additionalContext` reply. Harnesses that run hooks as code, such as Pi and OMP, can use the modules in `integrations/skillshare/`.

## Use

```bash
rem add -src claude "prefers pnpm for Node work"
rem search "preferences"
rem dream --v2 --limit 10 --dry-run   # show what a dream would read
rem dream --v2 --limit 10             # write a dream onto a vault branch
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
| `REM_PRIVATE_PROJECTS` | `personal` | Comma-separated markers; a session whose project name or folder contains one keeps its metadata but loses its text in other sessions' context |
| `REM_DREAM_RUNNER` | `claude` | Headless runner for `rem dream --v2`; only `claude` is supported so far |
| `REM_DREAM_MODEL` | `haiku` | Model for the dream |
| `REM_AGENTS_MD` | `~/.agents/AGENTS.md` | The file a dream reads and writes patches for |
| `REM_HOME` | `~/.local/share/rem` | Where the Pi and OMP modules look for the hook |
| `AGENTSVIEW_BIN` | found on `PATH` | Tool override |

## Development

```bash
make test
```

## License

[MIT](LICENSE)
